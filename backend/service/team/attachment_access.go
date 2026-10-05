package team

import (
	"fmt"
	"time"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs/dbfs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/manager"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 团队附件访问通道（B+）
//
// 背景：团队附件表（team_task_attachments / team_topic_attachments）**只存
// Cloudreve 文件的引用（file_id），不复制文件**。在补这条通道之前，项目成员
// 能在列表里看到附件记录，却打不开文件本体：
//
//	GET /file/info?id=<file_id>   -> 403 Permission denied
//	GET /file/info?uri=cloudreve://my/<name> -> 40016 Path not exist
//
// 因为 Cloudreve 的文件系统按所有者隔离（`cloudreve://my/...` 只解析自己的
// 文件），成员既不是所有者、又不在分享通道上，所以没有任何访问路径。
//
// 本通道的授权凭证 = **项目成员身份**：
//   attachment -> task/topic -> project_id -> checkAccess(ctx, dep, projectID, uid)
//
// 授权通过后，用 `dbfs.WithBypassOwnerCheck` 旁路所有者的所有权检查，
// 生成 Cloudreve **原生**的文件 URL。之所以返回原生 URL 而不是自行代理下载：
//  1. 前端可直接复用既有的预览器/下载器，无需另写流式转发；
//  2. 用户拿到的是真实的文件访问路径，**文件 ACL 得以在正确的层面上生效**
//     （ACL 判定的是「对可解析路径的读写等级」，代理下载会绕过它）。
//
// 注意：旁路所有者的**所有权**检查不等于绕过 ACL —— aclGuard 仍照常生效，
// 因此管理员对附件文件设的「只读」依然能拦住成员写入。
// ============================================================

type (
	// AttachmentAccessService 任务附件访问（GET /team/attachment/:id/url）
	AttachmentAccessService struct{}

	// TopicAttachmentAccessService 话题附件访问（GET /team/topic-attachment/:id/url）
	TopicAttachmentAccessService struct{}

	// AttachmentURLResponse 附件访问 URL
	AttachmentURLResponse struct {
		// URL 为 Cloudreve 原生文件访问地址（带签名，有有效期）
		URL string `json:"url"`
		// Expires URL 过期时间
		Expires time.Time `json:"expires"`
		// Name 文件名（便于前端展示/下载时定名）
		Name string `json:"name"`
		// Size 文件大小（快照值）
		Size int64 `json:"size"`
	}
)

// 说明：任务的 :id 参数上下文复用 members.go 已声明的 AttachmentIDParamCtx，
// 话题的复用 topic_attachment.go 的 TopicAttachmentIDParamCtx —— 二者已在
// 本包内定义，此处不再重复声明（重复声明会导致编译失败）。

// GetTaskAttachmentURL 返回任务附件的访问 URL。
//
// 授权：请求者必须是该附件所属项目的成员（或项目 owner / 全局管理员，
// 均经由 checkAccess 统一判定）。
func (s *AttachmentIDParamCtx) GetTaskAttachmentURL(c *gin.Context) (*AttachmentURLResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}

	aid := pathID(c)

	att, err := dep.TeamClient().GetAttachmentByID(c, aid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Attachment not found", err)
	}

	// 附件 -> 任务 -> 项目，用项目成员身份做授权凭证
	task, err := dep.TeamClient().GetTaskByID(c, att.TaskID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	if _, err := checkAccess(c, dep, task.ProjectID, uid); err != nil {
		return nil, err
	}

	return buildAttachmentURL(c, dep, att.FileID, att.Name, att.Size)
}

// GetTopicAttachmentURL 返回讨论区话题附件的访问 URL。
func (s *TopicAttachmentIDParamCtx) GetTopicAttachmentURL(c *gin.Context) (*AttachmentURLResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}

	aid := pathID(c)

	att, err := dep.TeamClient().GetTopicAttachmentByID(c, aid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Attachment not found", err)
	}

	topic, err := dep.TeamClient().GetTopicByID(c, att.TopicID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Topic not found", err)
	}
	if _, err := checkAccess(c, dep, topic.ProjectID, uid); err != nil {
		return nil, err
	}

	return buildAttachmentURL(c, dep, att.FileID, att.Name, att.Size)
}

// buildAttachmentURL 由 Cloudreve 文件 ID 生成原生访问 URL。
//
// 关键点：
//   - 文件可能属于**别的用户**（附件引用的是上传者自己的文件），因此必须
//     走 `dbfs.WithBypassOwnerCheck` 旁路所有权检查，否则会得到 ErrOwnerOnly；
//   - 但请求者（项目成员）在 Cloudreve 里并没有该文件的读取权，所以这里
//     需要以「文件所有者」的身份来取 URL —— 用 fileCLient 反查 owner，
//     再把该 owner 作为 file manager 的用户上下文。
func buildAttachmentURL(c *gin.Context, dep dependency.Dep,
	fileID int, name string, size int64) (*AttachmentURLResponse, error) {

	file, err := dep.FileClient().GetByID(c, fileID)
	if err != nil || file == nil {
		return nil, serializer.NewError(serializer.CodeFileNotFound, "File not found", err)
	}

	owner, err := dep.UserClient().GetByID(c, file.OwnerID)
	if err != nil || owner == nil {
		return nil, serializer.NewError(serializer.CodeUserNotFound, "File owner not found", err)
	}

	m := manager.NewFileManager(dep, owner)
	defer m.Recycle()

	// TraverseFile 由文件 ID 反解出该文件在其**所有者**文件系统中的真实 uri。
	// 这里之所以能成：m 的用户就是文件所有者。
	attFile, err := m.TraverseFile(c, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve attachment file uri: %w", err)
	}
	uri := attFile.Uri(true)

	expire := time.Now().Add(dep.SettingProvider().EntityUrlValidDuration(c))

	// WithBypassOwnerCheck：以所有者身份解析文件，故不需要额外的所有权检查。
	//
	// 【2026-10-04 第 3 步】本注释此前与实现不符，特此记录原因：
	//   当时 aclGuard 会被 ByPassOwnerCheckCtxKey 提前放行（旧 acl.go:67 的
	//   `if ok { return nil }`），所以「ACL 仍会照常执行」是【错的】。
	//   第 3 步拆除了那段旁路后，此处才真正成立。
	//
	// 本路径只做读判定（GetEntityUrls -> DBFS.Get -> aclGuard(AclRequireRead)），
	// 而 ACL 只在写操作命中 read 记录时拒绝（见 acl.go），故读不受影响。
	//
	// ⚠️ 修改 acl.go 中旁路/豁免相关逻辑时，请一并复核本注释。
	ctx := dbfs.WithBypassOwnerCheck(c)

	res, _, err := m.GetEntityUrls(ctx, []manager.GetEntityUrlArgs{{URI: uri}},
		fs.WithIsDownload(true),
		fs.WithUrlExpire(&expire),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate attachment url: %w", err)
	}
	if len(res) == 0 {
		return nil, serializer.NewError(serializer.CodeFileNotFound, "Attachment file is not available", nil)
	}

	return &AttachmentURLResponse{
		URL:     res[0].Url,
		Expires: expire,
		Name:    name,
		Size:    size,
	}, nil
}
