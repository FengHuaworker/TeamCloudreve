package team

import (
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
// 文档（Wiki）
// ============================================================

type (
	ListDocService struct{}
	ListDocParamCtx struct{}

	CreateDocService struct {
		ProjectID int    `json:"project_id" binding:"required"`
		ParentID  *int   `json:"parent_id"`
		Title     string `json:"title"`
		Content   string `json:"content"`
		IsFolder  bool   `json:"is_folder"`
		Icon      string `json:"icon"`
	}
	CreateDocParamCtx struct{}

	DocIDService struct{}
	DocIDParamCtx struct{}

	UpdateDocService struct {
		Title    *string  `json:"title"`
		Content  *string  `json:"content"`
		ParentID *int     `json:"parent_id"`
		Icon     *string  `json:"icon"`
		SortOrder *float64 `json:"sort_order"`
	}
	UpdateDocParamCtx struct{}
)

// DocNode 文档树节点
type DocNode struct {
	ID        int        `json:"id"`
	ProjectID int        `json:"project_id"`
	ParentID  *int       `json:"parent_id,omitempty"`
	Title     string     `json:"title"`
	Icon      string     `json:"icon"`
	IsFolder  bool       `json:"is_folder"`
	SortOrder float64    `json:"sort_order"`
	Creator   *UserBrief `json:"creator,omitempty"`
	Editor    *UserBrief `json:"editor,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
	Children  []*DocNode `json:"children,omitempty"`
}

// DocDetail 文档详情（含正文）
type DocDetail struct {
	DocNode
	Content string `json:"content"`
}

func (s *ListDocService) Tree(c *gin.Context) ([]*DocNode, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	pid := pathID(c)
	if _, err := checkAccess(c, dep, pid, uid); err != nil {
		return nil, err
	}

	docs, err := inventory.NewTeamDocClient(dep.DBClient()).ListDocs(c, pid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list documents", err)
	}

	builder := newUserBriefBuilder(c, dep)
	nodes := make(map[int]*DocNode, len(docs))
	for _, d := range docs {
		nodes[d.ID] = &DocNode{
			ID: d.ID, ProjectID: d.ProjectID, ParentID: d.ParentID,
			Title: d.Title, Icon: d.Icon, IsFolder: d.IsFolder, SortOrder: d.SortOrder,
			Creator: builder.build(d.CreatorID), Editor: builder.build(d.LastEditorID),
			UpdatedAt: d.UpdatedAt,
		}
	}

	roots := make([]*DocNode, 0)
	for _, d := range docs {
		n := nodes[d.ID]
		if d.ParentID != nil {
			if parent, ok := nodes[*d.ParentID]; ok {
				parent.Children = append(parent.Children, n)
				continue
			}
		}
		roots = append(roots, n)
	}
	return roots, nil
}

func (s *DocIDService) Get(c *gin.Context) (*DocDetail, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	did := pathID(c)

	dc := inventory.NewTeamDocClient(dep.DBClient())
	doc, err := dc.GetDocByID(c, did)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Document not found", err)
	}
	if _, err := checkAccess(c, dep, doc.ProjectID, uid); err != nil {
		return nil, err
	}

	builder := newUserBriefBuilder(c, dep)
	return &DocDetail{
		DocNode: DocNode{
			ID: doc.ID, ProjectID: doc.ProjectID, ParentID: doc.ParentID,
			Title: doc.Title, Icon: doc.Icon, IsFolder: doc.IsFolder, SortOrder: doc.SortOrder,
			Creator: builder.build(doc.CreatorID), Editor: builder.build(doc.LastEditorID),
			UpdatedAt: doc.UpdatedAt,
		},
		Content: doc.Content,
	}, nil
}

func (s *CreateDocService) Create(c *gin.Context) (*DocDetail, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	access, err := checkAccess(c, dep, s.ProjectID, uid)
	if err != nil {
		return nil, err
	}
	if !CanEdit(access.Role) {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to create documents", nil)
	}

	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = "未命名文档"
		if s.IsFolder {
			title = "未命名分组"
		}
	}

	dc := inventory.NewTeamDocClient(dep.DBClient())
	doc, err := dc.CreateDoc(c, &inventory.NewTeamDocArgs{
		ProjectID: s.ProjectID,
		ParentID:  s.ParentID,
		Title:     title,
		Content:   s.Content,
		CreatorID: uid,
		IsFolder:  s.IsFolder,
		Icon:      s.Icon,
	})
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create document", err)
	}

	pid := s.ProjectID
	_ = pid
	notifyMentions(c, dep, s.Content, MentionTarget{
		ProjectID: s.ProjectID,
		Type:      inventory.NotificationMention,
		Title:     "文档中提到了你",
		Body:      title,
		DocID:     &doc.ID,
	}, uid)

	return (&DocIDService{}).GetByID(c, dep, doc.ID)
}

func (s *DocIDService) GetByID(c *gin.Context, dep dependency.Dep, id int) (*DocDetail, error) {
	dc := inventory.NewTeamDocClient(dep.DBClient())
	doc, err := dc.GetDocByID(c, id)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Document not found", err)
	}
	builder := newUserBriefBuilder(c, dep)
	return &DocDetail{
		DocNode: DocNode{
			ID: doc.ID, ProjectID: doc.ProjectID, ParentID: doc.ParentID,
			Title: doc.Title, Icon: doc.Icon, IsFolder: doc.IsFolder, SortOrder: doc.SortOrder,
			Creator: builder.build(doc.CreatorID), Editor: builder.build(doc.LastEditorID),
			UpdatedAt: doc.UpdatedAt,
		},
		Content: doc.Content,
	}, nil
}

func (s *UpdateDocService) Update(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	did := pathID(c)

	dc := inventory.NewTeamDocClient(dep.DBClient())
	doc, err := dc.GetDocByID(c, did)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Document not found", err)
	}
	access, err := checkAccess(c, dep, doc.ProjectID, uid)
	if err != nil {
		return err
	}
	if !CanEdit(access.Role) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to edit this document", nil)
	}

	if err := dc.UpdateDoc(c, did, func(up *ent.TeamDocUpdateOne) {
		if s.Title != nil {
			up.SetTitle(strings.TrimSpace(*s.Title))
		}
		if s.Content != nil {
			up.SetContent(*s.Content)
		}
		if s.Icon != nil {
			up.SetIcon(*s.Icon)
		}
		if s.SortOrder != nil {
			up.SetSortOrder(*s.SortOrder)
		}
		if s.ParentID != nil {
			if *s.ParentID > 0 && *s.ParentID != did {
				up.SetParentID(*s.ParentID)
			} else {
				up.ClearParentID()
			}
		}
		up.SetLastEditorID(uid)
	}); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to update document", err)
	}

	if s.Content != nil {
		notifyMentions(c, dep, *s.Content, MentionTarget{
			ProjectID: doc.ProjectID,
			Type:      inventory.NotificationMention,
			Title:     "文档中提到了你",
			Body:      doc.Title,
			DocID:     &did,
		}, uid)
	}
	return nil
}

func (s *DocIDService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	did := pathID(c)

	dc := inventory.NewTeamDocClient(dep.DBClient())
	doc, err := dc.GetDocByID(c, did)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Document not found", err)
	}
	access, err := checkAccess(c, dep, doc.ProjectID, uid)
	if err != nil {
		return err
	}
	if !CanManage(access.Role) && doc.CreatorID != uid {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to delete this document", nil)
	}

	if err := dc.DeleteDocCascade(c, doc.ProjectID, did); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to delete document", err)
	}
	return nil
}

// ============================================================
// 通知
// ============================================================

type (
	ListNotificationService struct {
		UnreadOnly bool `form:"unread_only"`
		Limit      int  `form:"limit"`
	}
	ListNotificationParamCtx struct{}

	ReadNotificationService struct {
		IDs []int `json:"ids"`
		All bool  `json:"all"`
	}
	ReadNotificationParamCtx struct{}
)

// NotificationResponse 通知响应
type NotificationResponse struct {
	ID        int        `json:"id"`
	Type      string     `json:"type"`
	Actor     *UserBrief `json:"actor,omitempty"`
	ProjectID *int       `json:"project_id,omitempty"`
	TaskID    *int       `json:"task_id,omitempty"`
	DocID     *int       `json:"doc_id,omitempty"`
	// TopicID 讨论区话题（新增字段，既有前端忽略即可）
	TopicID   *int       `json:"topic_id,omitempty"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	IsRead    bool       `json:"is_read"`
	CreatedAt time.Time  `json:"created_at"`
}

func (s *ListNotificationService) List(c *gin.Context) ([]*NotificationResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}

	nc := inventory.NewTeamNotificationClient(dep.DBClient())
	items, err := nc.ListNotifications(c, uid, s.UnreadOnly, s.Limit)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list notifications", err)
	}

	builder := newUserBriefBuilder(c, dep)
	res := make([]*NotificationResponse, 0, len(items))
	for _, n := range items {
		res = append(res, &NotificationResponse{
			ID: n.ID, Type: n.Type, Actor: builder.build(n.ActorID),
			ProjectID: n.ProjectID, TaskID: n.TaskID, DocID: n.DocID, TopicID: n.TopicID,
			Title: n.Title, Body: n.Body, IsRead: n.IsRead, CreatedAt: n.CreatedAt,
		})
	}
	return res, nil
}

type UnreadCountService struct{}
type UnreadCountParamCtx struct{}

func (s *UnreadCountService) Get(c *gin.Context) (map[string]int, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	n, err := inventory.NewTeamNotificationClient(dep.DBClient()).CountUnread(c, uid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count notifications", err)
	}
	return map[string]int{"unread": n}, nil
}

func (s *ReadNotificationService) Read(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}

	nc := inventory.NewTeamNotificationClient(dep.DBClient())
	if s.All || len(s.IDs) == 0 {
		return nc.MarkAllRead(c, uid)
	}
	if err := nc.MarkRead(c, uid, s.IDs); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to mark as read", err)
	}
	return nil
}

// ============================================================
// 到期提醒（可由定时任务调用，也可在读取通知时惰性触发）
// ============================================================

// ScanDueSoon 扫描指定用户可见项目中 24 小时内到期的任务并生成提醒。
// 通过 ExistsNotification 去重，保证同一任务只提醒一次。
func ScanDueSoon(c *gin.Context, dep dependency.Dep, uid int) {
	tc := dep.TeamClient()
	nc := inventory.NewTeamNotificationClient(dep.DBClient())

	projects, err := tc.ListProjects(c, uid, false)
	if err != nil {
		return
	}

	now := time.Now()
	deadline := now.Add(24 * time.Hour)

	for _, p := range projects {
		tasks, err := tc.ListTasks(c, p.ID, &inventory.TeamTaskFilter{AssigneeID: &uid})
		if err != nil {
			continue
		}
		for _, t := range tasks {
			if t.DueAt == nil || t.Status == inventory.TeamTaskStatusDone {
				continue
			}
			if t.DueAt.After(deadline) {
				continue
			}
			exists, err := nc.ExistsNotification(c, uid, inventory.NotificationDueSoon, t.ID)
			if err != nil || exists {
				continue
			}
			pid, tid := p.ID, t.ID
			notify(c, dep, &inventory.NewTeamNotificationArgs{
				UserID:    uid,
				Type:      inventory.NotificationDueSoon,
				ProjectID: &pid,
				TaskID:    &tid,
				Title:     "任务即将到期",
				Body:      t.Title,
			})
		}
	}
}

// 保证 strconv 被使用（供后续扩展解析用）
var _ = strconv.Itoa
