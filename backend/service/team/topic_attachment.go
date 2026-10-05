package team

import (
	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 讨论区附件（契约补遗 §C）
//
// 设计：与任务附件完全同构 —— 只存 Cloudreve 文件引用，不复制文件，
// 因此天然复用 Cloudreve 的存储、权限与去重（Entity 引用计数）。
//
// 权限：
//   - 列表：项目成员即可读
//   - 上传：需要 post_topic 或 reply_topic 能力
//   - 删除：上传者本人，或拥有 moderate_topic 能力者
// ============================================================

type (
	// TopicAttachmentParamCtx 话题附件列表的参数上下文
	TopicAttachmentParamCtx struct{}

	// CreateTopicAttachmentService 上传附件（body: {file_id}）
	CreateTopicAttachmentService struct {
		FileID string `json:"file_id" binding:"required"`
	}
	CreateTopicAttachmentParamCtx struct{}

	// TopicAttachmentIDService 删除附件
	TopicAttachmentIDService  struct{}
	TopicAttachmentIDParamCtx struct{}
)

// ListTopicAttachments 列出话题的全部附件（项目成员可读）。
func (s *TopicAttachmentParamCtx) List(c *gin.Context) ([]*TopicAttachmentResponse, error) {
	return listTopicAttachments(c)
}

// listTopicAttachments 是列表逻辑的共用实现，供 GET 与 PUT 复用。
//
// PUT 返回「刷新后的全量列表」（契约补遗 §C），因此两个入口必须走同一份
// 组装逻辑，否则两边的字段或权限判断会慢慢分叉。
func listTopicAttachments(c *gin.Context) ([]*TopicAttachmentResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	tid := pathID(c)

	topic, err := dep.TeamClient().GetTopicByID(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Topic not found", err)
	}
	if _, err := checkAccess(c, dep, topic.ProjectID, uid); err != nil {
		return nil, err
	}

	attas, err := dep.TeamClient().ListTopicAttachments(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list topic attachments", err)
	}

	builder := newUserBriefBuilder(c, dep)
	res := make([]*TopicAttachmentResponse, 0, len(attas))
	for _, a := range attas {
		res = append(res, &TopicAttachmentResponse{
			ID:        a.ID,
			TopicID:   a.TopicID,
			FileID:    hashid.EncodeFileID(dep.HashIDEncoder(), a.FileID),
			Name:      a.Name,
			Size:      a.Size,
			User:      builder.build(a.UserID),
			CreatedAt: a.CreatedAt,
		})
	}
	return res, nil
}

// Create 上传附件到话题，返回刷新后的全量列表。
func (s *CreateTopicAttachmentService) Create(c *gin.Context) ([]*TopicAttachmentResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	tid := pathID(c)

	topic, err := dep.TeamClient().GetTopicByID(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Topic not found", err)
	}
	access, err := checkAccess(c, dep, topic.ProjectID, uid)
	if err != nil {
		return nil, err
	}
	// 能发话题或能回复的人，就能给话题加附件
	if !Capability(access.Role, CapPostTopic) && !Capability(access.Role, CapReplyTopic) {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr,
			"You have no permission to add attachments", nil)
	}

	// 解析 file_id（前端传 hashid）并校验文件存在
	fileID, err := dep.HashIDEncoder().Decode(s.FileID, hashid.FileID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid file id", err)
	}
	file, err := dep.FileClient().GetByID(c, int(fileID))
	if err != nil || file == nil {
		return nil, serializer.NewError(serializer.CodeFileNotFound, "File not found", err)
	}
	// 只允许引用自己的文件，避免越权把别人的文件挂到话题上
	if file.OwnerID != uid {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr,
			"You can only attach your own files", nil)
	}

	if _, err := dep.TeamClient().CreateTopicAttachment(c, &inventory.NewTeamTopicAttachmentArgs{
		TopicID: tid,
		FileID:  int(fileID),
		UserID:  uid,
		Name:    file.Name,
		Size:    file.Size,
	}); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to attach file", err)
	}

	// 契约：返回刷新后的全量列表，前端无需再发一次 GET
	return listTopicAttachments(c)
}

// Delete 删除附件（上传者本人或 moderate_topic）。
//
// 路径是 /team/topic-attachment/:id —— 不能用 /team/attachment/:id，
// 那个已被任务附件占用（契约补遗 §C）。
func (s *TopicAttachmentIDService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	aid := pathID(c)

	atta, err := dep.TeamClient().GetTopicAttachmentByID(c, aid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Attachment not found", err)
	}
	// 通过 topic 反查归属，才能做项目级权限判断
	topic, err := dep.TeamClient().GetTopicByID(c, atta.TopicID)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Topic not found", err)
	}
	access, err := checkAccess(c, dep, topic.ProjectID, uid)
	if err != nil {
		return err
	}
	// 上传者本人，或拥有 moderate_topic 能力者可移除
	if atta.UserID != uid && !Capability(access.Role, CapModerateTopic) {
		return serializer.NewError(serializer.CodeNoPermissionErr,
			"You have no permission to remove this attachment", nil)
	}

	if err := dep.TeamClient().DeleteTopicAttachment(c, aid); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to remove attachment", err)
	}
	return nil
}
