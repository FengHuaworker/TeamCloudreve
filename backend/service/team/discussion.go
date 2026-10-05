package team

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 讨论区（话题 / 楼层）
//
// 设计要点（契约 §2）：
//   - 话题正文即首帖（1 楼），因此楼层从 2 开始编号；
//   - 项目成员才能看 / 发，全部写操作走权限矩阵能力项；
//   - is_locked 的话题禁止回复；
//   - 列表排序：置顶优先，其余按「最后回复时间，无回复则创建时间」倒序。
// ============================================================

// 分页默认值与上限
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

type (
	// ListTopicService 话题列表（query：keyword / category / page / page_size）
	ListTopicService struct {
		Keyword  string `form:"keyword"`
		Category string `form:"category"`
		Page     int    `form:"page"`
		PageSize int    `form:"page_size"`
	}
	ListTopicParamCtx struct{}

	// CreateTopicService 创建话题
	CreateTopicService struct {
		ProjectID int    `json:"project_id" binding:"required"`
		Title     string `json:"title" binding:"required,max=512"`
		Content   string `json:"content"`
		Category  string `json:"category"`
	}
	CreateTopicParamCtx struct{}

	// TopicIDService 话题详情 / 删除
	TopicIDService struct{}
	TopicIDParamCtx struct{}

	// UpdateTopicService 更新话题（作者或 moderate_topic）
	UpdateTopicService struct {
		Title    *string `json:"title"`
		Content  *string `json:"content"`
		Category *string `json:"category"`
	}
	UpdateTopicParamCtx struct{}

	// ReplyTopicService 回复话题（分页参数从 query 取，缺省为第 1 页）
	ReplyTopicService struct {
		Content  string `json:"content" binding:"required"`
		ParentID *int   `json:"parent_id"`
	}
	ReplyTopicParamCtx struct{}

	// TopicStateService 置顶 / 锁定 / 标记已解决（moderate_topic）
	TopicStateService struct {
		IsPinned   *bool `json:"is_pinned"`
		IsLocked   *bool `json:"is_locked"`
		IsResolved *bool `json:"is_resolved"`
	}
	TopicStateParamCtx struct{}

	// PostIDService 删除楼层（作者或 moderate_topic）
	PostIDService struct{}
	PostIDParamCtx struct{}
)

// ============================ 辅助 ============================

// topicCategory 归一化话题分类：非法或空值一律回落到 discuss（契约 §2.2）
func topicCategory(v string) string {
	switch strings.TrimSpace(v) {
	case inventory.TeamTopicCategoryAnnounce:
		return inventory.TeamTopicCategoryAnnounce
	case inventory.TeamTopicCategoryQuestion:
		return inventory.TeamTopicCategoryQuestion
	case inventory.TeamTopicCategoryShare:
		return inventory.TeamTopicCategoryShare
	default:
		return inventory.TeamTopicCategoryDiscuss
	}
}

// pagination 归一化分页参数
func pagination(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

// queryPagination 从 query 里取分页参数。
//
// 回复接口是 FromJSON 端点（只解析 body），因此当前页需要单独从 query 读取；
// 契约里「返回刷新后的当前页」在缺省时即第 1 页。
func queryPagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	return pagination(page, pageSize)
}

// canModerateTopic 话题作者本人，或具备 moderate_topic 能力的人，可以管理该话题
func canModerateTopic(role string, topic *ent.TeamTopic, uid int) bool {
	return topic.UserID == uid || Capability(role, CapModerateTopic)
}

// TopicService 提供话题响应构建
type TopicService struct{}

func (s *TopicService) build(t *ent.TeamTopic, builder *userBriefBuilder) *TopicResponse {
	resp := &TopicResponse{
		ID:          t.ID,
		ProjectID:   t.ProjectID,
		Title:       t.Title,
		Content:     t.Content,
		Category:    t.Category,
		User:        builder.build(t.UserID),
		IsPinned:    t.IsPinned,
		IsLocked:    t.IsLocked,
		IsResolved:  t.IsResolved,
		ReplyTotal:  t.ReplyTotal,
		ViewTotal:   t.ViewTotal,
		LastReplyAt: t.LastReplyAt,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
	if t.LastReplyUserID > 0 {
		resp.LastReplyUser = builder.build(t.LastReplyUserID)
	}
	return resp
}

// detail 组装话题详情（含当前页楼层）
func (s *TopicService) detail(c *gin.Context, dep dependency.Dep, topic *ent.TeamTopic,
	page, pageSize int) (*TopicDetailResponse, error) {

	builder := newUserBriefBuilder(c, dep)

	total, err := dep.TeamClient().CountPosts(c, topic.ID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count replies", err)
	}

	posts, err := dep.TeamClient().ListPosts(c, topic.ID, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list replies", err)
	}

	// 注意：Go 的 nil slice 会被序列化成 null，前端按数组处理会崩，
	// 这里统一归一化为空切片（与看板 columns 的处理一致）。
	postList := make([]*PostResponse, 0, len(posts))
	for _, p := range posts {
		postList = append(postList, &PostResponse{
			ID:        p.ID,
			TopicID:   p.TopicID,
			User:      builder.build(p.UserID),
			Content:   p.Content,
			Floor:     p.Floor,
			ParentID:  p.ParentID,
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
		})
	}

	return &TopicDetailResponse{
		Topic:    s.build(topic, builder),
		Posts:    postList,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// ============================ 列表 / 创建 ============================

// List 话题列表（项目成员可见）
func (s *ListTopicService) List(c *gin.Context) (*TopicListResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	pid := pathID(c)

	// 项目成员才能看
	if _, err := checkAccess(c, dep, pid, uid); err != nil {
		return nil, err
	}

	page, pageSize := pagination(s.Page, s.PageSize)
	filter := &inventory.TeamTopicFilter{
		Keyword: strings.TrimSpace(s.Keyword),
		Offset:  (page - 1) * pageSize,
		Limit:   pageSize,
	}
	// 只有显式传了 category 才过滤；非法值按契约回落到 discuss
	if strings.TrimSpace(s.Category) != "" {
		filter.Category = topicCategory(s.Category)
	}

	total, err := dep.TeamClient().CountTopics(c, pid, filter)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count topics", err)
	}

	topics, err := dep.TeamClient().ListTopics(c, pid, filter)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list topics", err)
	}

	builder := newUserBriefBuilder(c, dep)
	svc := &TopicService{}
	res := make([]*TopicResponse, 0, len(topics))
	for _, t := range topics {
		res = append(res, svc.build(t, builder))
	}

	return &TopicListResponse{Topics: res, Total: total, Page: page, PageSize: pageSize}, nil
}

// Create 创建话题（需 post_topic 能力）
func (s *CreateTopicService) Create(c *gin.Context) (*TopicResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}

	access, err := checkAccess(c, dep, s.ProjectID, uid)
	if err != nil {
		return nil, err
	}
	if !Capability(access.Role, CapPostTopic) {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to post topics", nil)
	}

	title := strings.TrimSpace(s.Title)
	if title == "" {
		return nil, serializer.NewError(serializer.CodeParamErr, "Topic title is required", nil)
	}

	topic, err := dep.TeamClient().CreateTopic(c, &inventory.NewTeamTopicArgs{
		ProjectID: s.ProjectID,
		Title:     title,
		Content:   s.Content,
		Category:  topicCategory(s.Category),
		UserID:    uid,
	})
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create topic", err)
	}

	// @提及 通知（契约 §B：话题正文里被 @ 的人收到 topic_mention）
	notifyMentions(c, dep, s.Content, MentionTarget{
		ProjectID: s.ProjectID,
		Type:      inventory.NotificationTopicMention,
		Title:     title,
		Body:      fmt.Sprintf("在话题《%s》中提到了你", title),
		TopicID:   &topic.ID,
	}, uid)

	builder := newUserBriefBuilder(c, dep)
	return (&TopicService{}).build(topic, builder), nil
}

// ============================ 详情 / 更新 / 删除 ============================

// Get 话题详情（项目成员可见），浏览量 +1
func (s *TopicIDService) Get(c *gin.Context) (*TopicDetailResponse, error) {
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

	// 契约 §2.2：view_total 在读取详情时 +1（自增失败不影响详情返回）
	if err := dep.TeamClient().IncreaseTopicView(c, tid); err == nil {
		topic.ViewTotal++
	}

	page, pageSize := queryPagination(c)
	return (&TopicService{}).detail(c, dep, topic, page, pageSize)
}

// Update 更新话题（作者或 moderate_topic）
func (s *UpdateTopicService) Update(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	tid := pathID(c)

	topic, err := dep.TeamClient().GetTopicByID(c, tid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Topic not found", err)
	}
	access, err := checkAccess(c, dep, topic.ProjectID, uid)
	if err != nil {
		return err
	}
	if !canModerateTopic(access.Role, topic, uid) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to update this topic", nil)
	}

	if err := dep.TeamClient().UpdateTopic(c, tid, func(up *ent.TeamTopicUpdateOne) {
		if s.Title != nil {
			if title := strings.TrimSpace(*s.Title); title != "" {
				up.SetTitle(title)
			}
		}
		if s.Content != nil {
			up.SetContent(*s.Content)
		}
		if s.Category != nil {
			up.SetCategory(topicCategory(*s.Category))
		}
	}); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to update topic", err)
	}

	// @提及 通知（契约 §B：话题编辑后同样要扫一遍正文）
	if s.Content != nil {
		notifyMentions(c, dep, *s.Content, MentionTarget{
			ProjectID: topic.ProjectID,
			Type:      inventory.NotificationTopicMention,
			Title:     topic.Title,
			Body:      fmt.Sprintf("在话题《%s》中提到了你", topic.Title),
			TopicID:   &tid,
		}, uid)
	}
	return nil
}

// Delete 删除话题（作者或 moderate_topic），级联删除其全部楼层
func (s *TopicIDService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	tid := pathID(c)

	topic, err := dep.TeamClient().GetTopicByID(c, tid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Topic not found", err)
	}
	access, err := checkAccess(c, dep, topic.ProjectID, uid)
	if err != nil {
		return err
	}
	if !canModerateTopic(access.Role, topic, uid) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to delete this topic", nil)
	}

	if err := dep.TeamClient().DeleteTopicCascade(c, tid); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to delete topic", err)
	}
	return nil
}

// State 置顶 / 锁定 / 标记已解决（需 moderate_topic 能力）
func (s *TopicStateService) State(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	tid := pathID(c)

	topic, err := dep.TeamClient().GetTopicByID(c, tid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Topic not found", err)
	}
	access, err := checkAccess(c, dep, topic.ProjectID, uid)
	if err != nil {
		return err
	}
	if !Capability(access.Role, CapModerateTopic) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to moderate topics", nil)
	}

	if s.IsPinned == nil && s.IsLocked == nil && s.IsResolved == nil {
		return serializer.NewError(serializer.CodeParamErr, "Nothing to update", nil)
	}

	if err := dep.TeamClient().UpdateTopic(c, tid, func(up *ent.TeamTopicUpdateOne) {
		if s.IsPinned != nil {
			up.SetIsPinned(*s.IsPinned)
		}
		if s.IsLocked != nil {
			up.SetIsLocked(*s.IsLocked)
		}
		if s.IsResolved != nil {
			up.SetIsResolved(*s.IsResolved)
		}
	}); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to update topic state", err)
	}
	return nil
}

// ============================ 回复 ============================

// Reply 回复话题（需 reply_topic 能力；锁定的话题一律拒绝）
func (s *ReplyTopicService) Reply(c *gin.Context) (*TopicDetailResponse, error) {
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
	if !Capability(access.Role, CapReplyTopic) {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to reply to topics", nil)
	}
	if topic.IsLocked {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "This topic is locked", nil)
	}

	content := strings.TrimSpace(s.Content)
	if content == "" {
		return nil, serializer.NewError(serializer.CodeParamErr, "Reply content is required", nil)
	}

	// 楼中楼：父楼层必须属于同一话题，否则忽略（当作普通回复）
	var parentID *int
	if s.ParentID != nil && *s.ParentID > 0 {
		if parent, perr := dep.TeamClient().GetPostByID(c, *s.ParentID); perr == nil && parent.TopicID == tid {
			parentID = &parent.ID
		}
	}

	// 楼层号 = 当前最大楼层 + 1（首帖为 1 楼）
	floor, err := dep.TeamClient().NextPostFloor(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to allocate floor number", err)
	}

	if _, err := dep.TeamClient().CreatePost(c, &inventory.NewTeamPostArgs{
		TopicID:  tid,
		UserID:   uid,
		Content:  content,
		Floor:    floor,
		ParentID: parentID,
	}); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create reply", err)
	}

	// @提及 通知（契约 §B：回复正文里被 @ 的人收到 post_mention）
	notifyMentions(c, dep, content, MentionTarget{
		ProjectID: topic.ProjectID,
		Type:      inventory.NotificationPostMention,
		Title:     topic.Title,
		Body:      fmt.Sprintf("在话题《%s》的回复中提到了你", topic.Title),
		TopicID:   &tid,
	}, uid)

	// 更新话题的回复计数与最后回复信息
	now := time.Now()
	if err := dep.TeamClient().IncreaseTopicReply(c, tid, uid, now); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to update topic reply info", err)
	}
	topic.ReplyTotal++
	topic.LastReplyAt = &now
	topic.LastReplyUserID = uid

	page, pageSize := queryPagination(c)
	return (&TopicService{}).detail(c, dep, topic, page, pageSize)
}

// Delete 删除楼层（作者本人或 moderate_topic），并回退话题的回复计数
func (s *PostIDService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	pid := pathID(c)

	post, err := dep.TeamClient().GetPostByID(c, pid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Reply not found", err)
	}
	topic, err := dep.TeamClient().GetTopicByID(c, post.TopicID)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Topic not found", err)
	}
	access, err := checkAccess(c, dep, topic.ProjectID, uid)
	if err != nil {
		return err
	}
	if post.UserID != uid && !Capability(access.Role, CapModerateTopic) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to delete this reply", nil)
	}

	if err := dep.TeamClient().DeletePost(c, pid); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to delete reply", err)
	}
	if err := dep.TeamClient().DecreaseTopicReply(c, topic.ID); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to update topic reply total", err)
	}

	// 回填「最后回复」：被删掉的正好是最后一条时，改指向剩下的最后一条（没有则清空）
	if latest, lerr := dep.TeamClient().LatestPost(c, topic.ID); lerr == nil {
		if latest == nil {
			_ = dep.TeamClient().ClearTopicLastReply(c, topic.ID)
		} else {
			_ = dep.TeamClient().UpdateTopic(c, topic.ID, func(up *ent.TeamTopicUpdateOne) {
				up.SetLastReplyAt(latest.CreatedAt)
				up.SetLastReplyUserID(latest.UserID)
			})
		}
	}
	return nil
}
