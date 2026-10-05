package explorer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/auth"
	"github.com/cloudreve/Cloudreve/v4/pkg/boolset"
	"github.com/cloudreve/Cloudreve/v4/pkg/cluster/routes"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/manager"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/queue"
	"github.com/cloudreve/Cloudreve/v4/pkg/util"
	"github.com/cloudreve/Cloudreve/v4/service/user"
	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
	"github.com/samber/lo"
)

type FullTextSearchResults struct {
	Hits  []FullTextSearchResult `json:"hits"`
	Total int64                  `json:"total"`
}

func BuildFullTextSearchResults(ctx context.Context, user *ent.User, hasher hashid.Encoder, results *manager.FullTextSearchResults) *FullTextSearchResults {
	return &FullTextSearchResults{
		Hits: lo.Map(results.Hits, func(result manager.FullTextSearchResult, index int) FullTextSearchResult {
			return FullTextSearchResult{
				File:    *BuildFileResponse(ctx, user, result.File, hasher, nil),
				Content: result.Content,
			}
		}),
		Total: results.Total,
	}
}

type FullTextSearchResult struct {
	File    FileResponse `json:"file"`
	Content string       `json:"content"`
}

type ArchiveListFilesResponse struct {
	Files []manager.ArchivedFile `json:"files"`
}

func BuildArchiveListFilesResponse(files []manager.ArchivedFile) *ArchiveListFilesResponse {
	return &ArchiveListFilesResponse{
		Files: files,
	}
}

type PutRelativeResponse struct {
	Name string `json:"Name"`
	Url  string `json:"Url"`
}

type DirectLinkResponse struct {
	Link    string `json:"link"`
	FileUrl string `json:"file_url"`
}

func BuildDirectLinkResponse(links []manager.DirectLink) []DirectLinkResponse {
	if len(links) == 0 {
		return nil
	}

	var res []DirectLinkResponse
	for _, link := range links {
		res = append(res, DirectLinkResponse{
			Link:    link.Url,
			FileUrl: link.File.Uri(false).String(),
		})
	}
	return res
}

const PathMyRedacted = "redacted"

type TaskResponse struct {
	CreatedAt    time.Time      `json:"created_at,"`
	UpdatedAt    time.Time      `json:"updated_at"`
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	Type         string         `json:"type"`
	Node         *user.Node     `json:"node,omitempty"`
	Summary      *queue.Summary `json:"summary,omitempty"`
	Error        string         `json:"error,omitempty"`
	ErrorHistory []string       `json:"error_history,omitempty"`
	Duration     int64          `json:"duration,omitempty"`
	ResumeTime   int64          `json:"resume_time,omitempty"`
	RetryCount   int            `json:"retry_count,omitempty"`
}

type TaskListResponse struct {
	Tasks      []TaskResponse               `json:"tasks"`
	Pagination *inventory.PaginationResults `json:"pagination"`
}

func BuildTaskListResponse(tasks []queue.Task, res *inventory.ListTaskResult, nodeMap map[int]*ent.Node, hasher hashid.Encoder) *TaskListResponse {
	return &TaskListResponse{
		Pagination: res.PaginationResults,
		Tasks: lo.Map(tasks, func(t queue.Task, index int) TaskResponse {
			var (
				node *ent.Node
				s    = t.Summarize(hasher)
			)

			if s.NodeID > 0 {
				node = nodeMap[s.NodeID]
			}
			return *BuildTaskResponse(t, node, hasher)
		}),
	}
}

func BuildTaskResponse(task queue.Task, node *ent.Node, hasher hashid.Encoder) *TaskResponse {
	model := task.Model()
	t := &TaskResponse{
		Status:    string(task.Status()),
		CreatedAt: model.CreatedAt,
		UpdatedAt: model.UpdatedAt,
		ID:        hashid.EncodeTaskID(hasher, task.ID()),
		Type:      task.Type(),
		Summary:   task.Summarize(hasher),
		Error:     auth.RedactSensitiveValues(model.PublicState.Error),
		ErrorHistory: lo.Map(model.PublicState.ErrorHistory, func(s string, index int) string {
			return auth.RedactSensitiveValues(s)
		}),
		Duration:   model.PublicState.ExecutedDuration.Milliseconds(),
		ResumeTime: model.PublicState.ResumeTime,
		RetryCount: model.PublicState.RetryCount,
	}

	if node != nil {
		t.Node = user.BuildNode(node, hasher)
	}

	return t
}

type UploadSessionResponse struct {
	SessionID       string                 `json:"session_id"`
	UploadID        string                 `json:"upload_id"`
	ChunkSize       int64                  `json:"chunk_size"` // 分块大小，0 为部分快
	Expires         int64                  `json:"expires"`    // 上传凭证过期时间， Unix 时间戳
	UploadURLs      []string               `json:"upload_urls,omitempty"`
	Credential      string                 `json:"credential,omitempty"`
	AccessKey       string                 `json:"ak,omitempty"`
	KeyTime         string                 `json:"keyTime,omitempty"` // COS用有效期
	CompleteURL     string                 `json:"completeURL,omitempty"`
	StoragePolicy   *StoragePolicy         `json:"storage_policy,omitempty"`
	Uri             string                 `json:"uri"`
	CallbackSecret  string                 `json:"callback_secret"`
	MimeType        string                 `json:"mime_type,omitempty"`
	UploadPolicy    string                 `json:"upload_policy,omitempty"`
	EncryptMetadata *types.EncryptMetadata `json:"encrypt_metadata,omitempty"`
}

func BuildUploadSessionResponse(session *fs.UploadCredential, hasher hashid.Encoder) *UploadSessionResponse {
	res := &UploadSessionResponse{
		SessionID:       session.SessionID,
		ChunkSize:       session.ChunkSize,
		Expires:         session.Expires,
		UploadURLs:      session.UploadURLs,
		Credential:      session.Credential,
		CompleteURL:     session.CompleteURL,
		Uri:             session.Uri,
		UploadID:        session.UploadID,
		StoragePolicy:   BuildStoragePolicy(session.StoragePolicy, hasher),
		CallbackSecret:  session.CallbackSecret,
		MimeType:        session.MimeType,
		UploadPolicy:    session.UploadPolicy,
		EncryptMetadata: session.EncryptMetadata,
	}

	if session.EncryptMetadata != nil {
		res.EncryptMetadata = &types.EncryptMetadata{
			Algorithm:    session.EncryptMetadata.Algorithm,
			KeyPlainText: session.EncryptMetadata.KeyPlainText,
			IV:           session.EncryptMetadata.IV,
		}
	}

	return res
}

// WopiFileInfo Response for `CheckFileInfo`
type WopiFileInfo struct {
	// Required
	BaseFileName string `json:"BaseFileName"`
	Version      string `json:"Version"`
	Size         int64  `json:"Size"`

	// Breadcrumb
	BreadcrumbBrandName  string `json:"BreadcrumbBrandName"`
	BreadcrumbBrandUrl   string `json:"BreadcrumbBrandUrl"`
	BreadcrumbFolderName string `json:"BreadcrumbFolderName"`
	BreadcrumbFolderUrl  string `json:"BreadcrumbFolderUrl"`

	// Post Message
	FileSharingPostMessage bool   `json:"FileSharingPostMessage"`
	FileVersionPostMessage bool   `json:"FileVersionPostMessage"`
	ClosePostMessage       bool   `json:"ClosePostMessage"`
	PostMessageOrigin      string `json:"PostMessageOrigin"`

	// Other miscellaneous properties
	FileNameMaxLength int    `json:"FileNameMaxLength"`
	LastModifiedTime  string `json:"LastModifiedTime"`

	// User metadata
	IsAnonymousUser  bool   `json:"IsAnonymousUser"`
	UserFriendlyName string `json:"UserFriendlyName"`
	UserId           string `json:"UserId"`
	OwnerId          string `json:"OwnerId"`

	// Permission
	ReadOnly                bool `json:"ReadOnly"`
	UserCanRename           bool `json:"UserCanRename"`
	UserCanReview           bool `json:"UserCanReview"`
	UserCanWrite            bool `json:"UserCanWrite"`
	UserCanNotWriteRelative bool `json:"UserCanNotWriteRelative"`

	SupportsRename    bool `json:"SupportsRename"`
	SupportsReviewing bool `json:"SupportsReviewing"`
	SupportsUpdate    bool `json:"SupportsUpdate"`
	SupportsLocks     bool `json:"SupportsLocks"`

	EnableShare bool `json:"EnableShare"`
}

type ViewerSessionResponse struct {
	Session *manager.ViewerSession `json:"session"`
	WopiSrc string                 `json:"wopi_src,omitempty"`
}

type ListResponse struct {
	Files      []FileResponse               `json:"files"`
	Parent     FileResponse                 `json:"parent,omitempty"`
	Pagination *inventory.PaginationResults `json:"pagination"`
	Props      *fs.NavigatorProps           `json:"props"`
	// ContextHint is used to speed up following operations under this listed directory.
	// It persists some intermedia state so that the following request don't need to query database again.
	// All the operations under this directory that supports context hint should carry this value in header
	// as X-Cr-Context-Hint.
	ContextHint           *uuid.UUID          `json:"context_hint"`
	RecursionLimitReached bool                `json:"recursion_limit_reached,omitempty"`
	MixedType             bool                `json:"mixed_type"`
	SingleFileView        bool                `json:"single_file_view,omitempty"`
	StoragePolicy         *StoragePolicy      `json:"storage_policy,omitempty"`
	View                  *types.ExplorerView `json:"view,omitempty"`
}

type FileResponse struct {
	Type          int                 `json:"type"`
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Size          int64               `json:"size"`
	Metadata      map[string]string   `json:"metadata"`
	Path          string              `json:"path,omitempty"`
	Shared        bool                `json:"shared,omitempty"`
	Capability    *boolset.BooleanSet `json:"capability,omitempty"`
	Owned         bool                `json:"owned,omitempty"`
	PrimaryEntity string              `json:"primary_entity,omitempty"`

	FolderSummary *fs.FolderSummary `json:"folder_summary,omitempty"`
	ExtendedInfo  *ExtendedInfo     `json:"extended_info,omitempty"`
}

type ExtendedInfo struct {
	StoragePolicy *StoragePolicy      `json:"storage_policy,omitempty"`
	StorageUsed   int64               `json:"storage_used"`
	Shares        []Share             `json:"shares,omitempty"`
	Entities      []Entity            `json:"entities,omitempty"`
	View          *types.ExplorerView `json:"view,omitempty"`
	DirectLinks   []DirectLink        `json:"direct_links,omitempty"`
}

type DirectLink struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	Downloaded int       `json:"downloaded"`
	CreatedAt  time.Time `json:"created_at"`
}

type StoragePolicy struct {
	ID                string           `json:"id"`
	Name              string           `json:"name"`
	AllowedSuffix     []string         `json:"allowed_suffix,omitempty"`
	DeniedSuffix      []string         `json:"denied_suffix,omitempty"`
	AllowedNameRegexp string           `json:"allowed_name_regexp,omitempty"`
	DeniedNameRegexp  string           `json:"denied_name_regexp,omitempty"`
	Type              types.PolicyType `json:"type"`
	MaxSize           int64            `json:"max_size"`
	Relay             bool             `json:"relay,omitempty"`
	ChunkConcurrency  int              `json:"chunk_concurrency,omitempty"`
	Encryption        bool             `json:"encryption,omitempty"`
}

type Entity struct {
	ID            string           `json:"id"`
	Size          int64            `json:"size"`
	Type          types.EntityType `json:"type"`
	CreatedAt     time.Time        `json:"created_at"`
	StoragePolicy *StoragePolicy   `json:"storage_policy,omitempty"`
	CreatedBy     *user.User       `json:"created_by,omitempty"`
	EncryptedWith types.Cipher     `json:"encrypted_with,omitempty"`
}

type Share struct {
	ID                string          `json:"id"`
	Name              string          `json:"name,omitempty"`
	RemainDownloads   *int            `json:"remain_downloads,omitempty"`
	Visited           int             `json:"visited"`
	Downloaded        int             `json:"downloaded,omitempty"`
	Expires           *time.Time      `json:"expires,omitempty"`
	Unlocked          bool            `json:"unlocked"`
	PasswordProtected bool            `json:"password_protected,omitempty"`
	SourceType        *types.FileType `json:"source_type,omitempty"`
	Owner             user.User       `json:"owner"`
	CreatedAt         time.Time       `json:"created_at,omitempty"`
	Expired           bool            `json:"expired"`
	Url               string          `json:"url"`
	ShowReadMe        bool            `json:"show_readme,omitempty"`
	Size              int64           `json:"size"`

	// Only viewable by owner
	IsPrivate bool   `json:"is_private,omitempty"`
	Password  string `json:"password,omitempty"`
	ShareView bool   `json:"share_view,omitempty"`

	// Only viewable if explicitly unlocked by owner
	SourceUri string `json:"source_uri,omitempty"`
}

func BuildShare(ctx context.Context, s *ent.Share, base *url.URL, hasher hashid.Encoder, requester *ent.User, owner *ent.User,
	name string, t types.FileType, unlocked bool, expired bool) *Share {
	redactLevel := user.RedactLevelAnonymous
	if !inventory.IsAnonymousUser(requester) {
		redactLevel = user.RedactLevelUser
	}
	res := Share{
		Name:              name,
		ID:                hashid.EncodeShareID(hasher, s.ID),
		Unlocked:          unlocked,
		Owner:             user.BuildUserRedacted(ctx, owner, redactLevel, hasher),
		Expired:           inventory.IsShareExpired(s) != nil || expired,
		Url:               BuildShareLink(s, hasher, base, unlocked),
		CreatedAt:         s.CreatedAt,
		Visited:           s.Views,
		SourceType:        util.ToPtr(t),
		PasswordProtected: s.Password != "",
	}

	if unlocked {
		res.RemainDownloads = s.RemainDownloads
		res.Downloaded = s.Downloads
		res.Expires = s.Expires
		res.Password = s.Password
		res.ShowReadMe = s.Props != nil && s.Props.ShowReadMe

		if t == types.FileTypeFile && s.Edges.File != nil {
			res.Size = s.Edges.File.Size
		}
	}

	if requester.ID == owner.ID {
		res.IsPrivate = s.Password != ""
		res.ShareView = s.Props != nil && s.Props.ShareView
	}

	return &res
}

func BuildListResponse(ctx context.Context, u *ent.User, parent fs.File, res *fs.ListFileResult, hasher hashid.Encoder) *ListResponse {
	r := &ListResponse{
		Files: lo.Map(res.Files, func(f fs.File, index int) FileResponse {
			return *BuildFileResponse(ctx, u, f, hasher, res.Props.Capability)
		}),
		Pagination:            res.Pagination,
		Props:                 res.Props,
		ContextHint:           res.ContextHint,
		RecursionLimitReached: res.RecursionLimitReached,
		MixedType:             res.MixedType,
		SingleFileView:        res.SingleFileView,
		StoragePolicy:         BuildStoragePolicy(res.StoragePolicy, hasher),
		View:                  res.View,
	}

	if !res.Parent.IsNil() {
		r.Parent = *BuildFileResponse(ctx, u, res.Parent, hasher, res.Props.Capability)
	}

	return r
}

// ============================================================
// ★ 一个响应里有【两个语义不同的 capability】—— 别把它们"修"成一致
//
//	props.capability     = 导航器级   "这个文件系统支持什么"
//	                       （来自 NavigatorProps.Capability，是包级常量）
//	files[i].capability  = 节点级     "这个对象你能做什么"
//	parent.capability    = 节点级     "当前这个目录你能做什么"
//
// team 空间里这三者【会故意不一致】：
//   · 用户对某个子目录被设了只读 ACL
//   · 于是那个节点是只读集，而同项目其它节点仍是可写集
//   · 前端据此隐藏该节点上的写入口（否则会"显示按钮、点了 403"）
//
// 所以：看到 props 与 files 对不上【不是 bug】，不要把它"修"回去。
// 反过来，如果发现某处代码【无条件】把 props 的值赋给每个节点，
// 那就是把节点级判定整个抹掉了 —— 见下面 BuildFileResponse 的说明。
// ============================================================

func BuildFileResponse(ctx context.Context, u *ent.User, f fs.File, hasher hashid.Encoder, cap *boolset.BooleanSet) *FileResponse {
	var owner *ent.User
	if f != nil {
		owner = f.Owner()
	}

	// ============================================================
	// ★ 节点自己的能力位优先于调用方传入的
	//
	// 【原行为】if cap == nil { cap = f.Capabilities() }
	//   即：只要调用方传了非 nil，节点自己的值【永远没有机会生效】。
	//   而 BuildListResponse:388/401 传的正是 res.Props.Capability ——
	//   导航器级常量。后果：
	//     在 team_navigator 里逐节点算出的 f.CapabilitiesBs 被覆盖，
	//     算得再对也到不了调用方。
	//   这解释了为什么 S0a 一直"能工作"：S0a 的判据本身就在
	//   Capabilities() 里，传的值恰好就是要的值，覆盖不覆盖都一样。
	//   而把判定下移到节点后，这处覆盖就必然冲突。
	//
	// 【为什么这是【送达端】而不是【产生端】的问题】
	//   产生端（team_navigator 逐节点赋值）改对了，
	//   但值在到达消费点之前被覆盖 —— 端到端完全看不出来。
	//   教训：追值要追到【消费点】，不能停在【产生点】。
	//
	// 【对其它导航器的影响：逐位等价，不是语义变更】
	//   5 个导航器（my/share/sharewithme/trash/team）的 root 都赋
	//   包级常量，而该常量就是 NavigatorProps.Capability 本身。
	//   于是只有两种情形：
	//     甲 own != nil -> cap = own，而 own 一路继承自 root，root 取自
	//                     同一个 Capabilities().Capability => own == passed
	//     乙 own == nil -> 保持传入值不变，与改动前逐字相同
	//   => 已用"四个导航器响应 JSON 改动前后逐字段 diff 为空"验证（非推理）
	// ============================================================
	if f != nil {
		if own := f.Capabilities(); own != nil {
			cap = own
		}
	}

	if cap == nil {
		cap = f.Capabilities() // 兜底：两侧都没有时保持上游原行为
	}

	res := &FileResponse{
		Type:          int(f.Type()),
		ID:            hashid.EncodeFileID(hasher, f.ID()),
		Name:          f.DisplayName(),
		CreatedAt:     f.CreatedAt(),
		UpdatedAt:     f.UpdatedAt(),
		Size:          f.Size(),
		Metadata:      f.Metadata(),
		Path:          f.Uri(false).String(),
		Shared:        f.Shared(),
		Capability:    cap,
		Owned:         owner == nil || owner.ID == u.ID,
		FolderSummary: f.FolderSummary(),
		ExtendedInfo:  BuildExtendedInfo(ctx, u, f, hasher),
		PrimaryEntity: hashid.EncodeEntityID(hasher, f.PrimaryEntityID()),
	}
	return res
}

func BuildExtendedInfo(ctx context.Context, u *ent.User, f fs.File, hasher hashid.Encoder) *ExtendedInfo {
	extendedInfo := f.ExtendedInfo()
	if extendedInfo == nil {
		return nil
	}

	dep := dependency.FromContext(ctx)
	base := dep.SettingProvider().SiteURL(ctx)

	ext := &ExtendedInfo{
		StoragePolicy: BuildStoragePolicy(extendedInfo.StoragePolicy, hasher),
		StorageUsed:   extendedInfo.StorageUsed,
		Entities: lo.Map(f.Entities(), func(e fs.Entity, index int) Entity {
			return BuildEntity(ctx, extendedInfo, e, hasher)
		}),
		DirectLinks: lo.Map(extendedInfo.DirectLinks, func(d *ent.DirectLink, index int) DirectLink {
			return BuildDirectLink(d, hasher, base)
		}),
	}

	if u.ID == f.OwnerID() {
		// Only owner can see the shares settings.
		ext.Shares = lo.Map(extendedInfo.Shares, func(s *ent.Share, index int) Share {
			return *BuildShare(ctx, s, base, hasher, u, u, f.DisplayName(), f.Type(), true, false)
		})
		ext.View = extendedInfo.View
	}

	return ext
}

func BuildDirectLink(d *ent.DirectLink, hasher hashid.Encoder, base *url.URL) DirectLink {
	return DirectLink{
		ID:         hashid.EncodeSourceLinkID(hasher, d.ID),
		URL:        routes.MasterDirectLink(base, hashid.EncodeSourceLinkID(hasher, d.ID), d.Name).String(),
		Downloaded: d.Downloads,
		CreatedAt:  d.CreatedAt,
	}
}

func BuildEntity(ctx context.Context, extendedInfo *fs.FileExtendedInfo, e fs.Entity, hasher hashid.Encoder) Entity {
	var u *user.User
	createdBy := e.CreatedBy()
	if createdBy != nil {
		userRedacted := user.BuildUserRedacted(ctx, e.CreatedBy(), user.RedactLevelAnonymous, hasher)
		u = &userRedacted
	}

	encryptedWith := types.Cipher("")
	if e.Encrypted() {
		encryptedWith = e.Props().EncryptMetadata.Algorithm
	}

	return Entity{
		ID:            hashid.EncodeEntityID(hasher, e.ID()),
		Type:          e.Type(),
		CreatedAt:     e.CreatedAt(),
		StoragePolicy: BuildStoragePolicy(extendedInfo.EntityStoragePolicies[e.PolicyID()], hasher),
		Size:          e.Size(),
		CreatedBy:     u,
		EncryptedWith: encryptedWith,
	}
}

func BuildShareLink(s *ent.Share, hasher hashid.Encoder, base *url.URL, unlocked bool) string {
	shareId := hashid.EncodeShareID(hasher, s.ID)
	if unlocked {
		return routes.MasterShareUrl(base, shareId, s.Password).String()
	}
	return routes.MasterShareUrl(base, shareId, "").String()
}

func BuildStoragePolicy(sp *ent.StoragePolicy, hasher hashid.Encoder) *StoragePolicy {
	if sp == nil {
		return nil
	}

	res := &StoragePolicy{
		ID:               hashid.EncodePolicyID(hasher, sp.ID),
		Name:             sp.Name,
		Type:             types.PolicyType(sp.Type),
		MaxSize:          sp.MaxSize,
		Relay:            sp.Settings.Relay,
		ChunkConcurrency: sp.Settings.ChunkConcurrency,
		Encryption:       sp.Settings.Encryption,
	}

	if sp.Settings.IsFileTypeDenyList {
		res.DeniedSuffix = sp.Settings.FileType
	} else {
		res.AllowedSuffix = sp.Settings.FileType
	}

	if sp.Settings.NameRegexp != "" {
		if sp.Settings.IsNameRegexpDenyList {
			res.DeniedNameRegexp = sp.Settings.NameRegexp
		} else {
			res.AllowedNameRegexp = sp.Settings.NameRegexp
		}
	}

	return res
}

func WriteEventSourceHeader(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
}

// WriteEventSource writes a Server-Sent Event to the client.
func WriteEventSource(c *gin.Context, event string, data any) {
	c.Writer.Write([]byte(fmt.Sprintf("event: %s\n", event)))
	c.Writer.Write([]byte("data:"))
	json.NewEncoder(c.Writer).Encode(data)
	c.Writer.Write([]byte("\n"))
	c.Writer.Flush()
}

var ErrSSETakeOver = errors.New("SSE take over")
