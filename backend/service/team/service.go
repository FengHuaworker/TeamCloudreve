package team

import (
	"context"
	"strings"
	"time"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 项目
// ============================================================

type (
	ListProjectService  struct{}
	ListProjectParamCtx struct{}

	CreateProjectService struct {
		Name        string `json:"name" binding:"required,max=255"`
		Description string `json:"description"`
	}
	CreateProjectParamCtx struct{}

	ProjectIDService  struct{}
	ProjectIDParamCtx struct{}

	UpdateProjectService struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
	}
	UpdateProjectParamCtx struct{}
)

func (s *ListProjectService) List(c *gin.Context) ([]*ProjectResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}

	// 全局管理员可见全部项目；普通用户只看自己拥有或已加入的。
	// 传 0 表示不加可见性过滤（见 inventory.ListProjects）。
	listUID := uid
	if isGlobalAdmin(c) {
		listUID = 0
	}

	projects, err := dep.TeamClient().ListProjects(c, listUID, false)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list projects", err)
	}

	builder := newUserBriefBuilder(c, dep)
	res := make([]*ProjectResponse, 0, len(projects))
	for _, p := range projects {
		res = append(res, s.build(c, dep, p, builder, uid))
	}
	return res, nil
}

func (s *ListProjectService) build(c context.Context, dep dependency.Dep, p *ent.TeamProject,
	builder *userBriefBuilder, uid int) *ProjectResponse {

	resp := &ProjectResponse{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Status:      p.Status,
		Owner:       builder.build(p.OwnerID),
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}

	if p.OwnerID == uid {
		resp.MyRole = inventory.TeamRoleOwner
	} else if m, err := dep.TeamClient().GetMember(c, p.ID, uid); err == nil && m != nil {
		resp.MyRole = m.Role
	}

	if byStatus, err := dep.TeamClient().CountTaskByStatus(c, p.ID); err == nil {
		for _, n := range byStatus {
			resp.TaskTotal += n
		}
		resp.DoneTotal = byStatus[inventory.TeamTaskStatusDone]
	}
	// 注意：创建项目时已将 owner 写入成员表，因此成员数就是成员表行数，
	// 不能再额外 +1，否则会重复计数。
	if members, err := dep.TeamClient().ListMembers(c, p.ID); err == nil {
		resp.MemberTotal = len(members)
	}
	return resp
}

func (s *CreateProjectService) Create(c *gin.Context) (*ProjectResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(s.Name)
	if name == "" {
		return nil, serializer.NewError(serializer.CodeParamErr, "Project name is required", nil)
	}

	project, err := dep.TeamClient().CreateProject(c, &inventory.NewTeamProjectArgs{
		Name:        name,
		Description: s.Description,
		OwnerID:     uid,
	})
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create project", err)
	}

	// owner 同时写入成员表，便于统一查询
	if _, err := dep.TeamClient().UpsertMember(c, project.ID, uid, inventory.TeamRoleOwner); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to add owner as member", err)
	}

	_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
		ProjectID: project.ID,
		UserID:    uid,
		Action:    "project.created",
		Payload:   map[string]any{"name": project.Name},
	})

	b := newUserBriefBuilder(c, dep)
	return (&ListProjectService{}).build(c, dep, project, b, uid), nil
}

func (s *ProjectIDService) Get(c *gin.Context) (*BoardResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	pid := pathID(c)

	access, err := checkAccess(c, dep, pid, uid)
	if err != nil {
		return nil, err
	}

	builder := newUserBriefBuilder(c, dep)
	project := (&ListProjectService{}).build(c, dep, access.Project, builder, uid)

	tasks, err := dep.TeamClient().ListTasks(c, pid, nil)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list tasks", err)
	}

	taskSvc := &TaskService{}
	byStatus := make(map[string][]*TaskResponse)
	for _, t := range tasks {
		byStatus[t.Status] = append(byStatus[t.Status], taskSvc.build(c, dep, t, builder))
	}

	columns := make([]*BoardColumn, 0, len(BoardStatusOrder))
	for _, st := range BoardStatusOrder {
		// 注意：Go 的 nil slice 会被 JSON 序列化成 null 而不是 []，
		// 前端对 column.tasks.length 的访问会因此崩溃，这里必须归一化为空切片。
		tasks := byStatus[st]
		if tasks == nil {
			tasks = []*TaskResponse{}
		}
		columns = append(columns, &BoardColumn{Status: st, Tasks: tasks})
	}
	// archived 不在看板展示，但保留数据

	members, _ := dep.TeamClient().ListMembers(c, pid)
	memberBriefs := make([]*UserBrief, 0, len(members)+1)
	memberBriefs = append(memberBriefs, builder.build(access.Project.OwnerID))
	for _, m := range members {
		if m.UserID == access.Project.OwnerID {
			continue
		}
		memberBriefs = append(memberBriefs, builder.build(m.UserID))
	}

	return &BoardResponse{Project: project, Columns: columns, Members: memberBriefs}, nil
}

func (s *UpdateProjectService) Update(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	pid := pathID(c)

	access, err := checkAccess(c, dep, pid, uid)
	if err != nil {
		return err
	}
	if !CanManage(access.Role) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "Only owner or admin can update the project", nil)
	}

	if err := dep.TeamClient().UpdateProject(c, pid, func(up *ent.TeamProjectUpdateOne) {
		if s.Name != nil {
			up.SetName(strings.TrimSpace(*s.Name))
		}
		if s.Description != nil {
			up.SetDescription(*s.Description)
		}
		if s.Status != nil && (*s.Status == "active" || *s.Status == "archived") {
			up.SetStatus(*s.Status)
		}
	}); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to update project", err)
	}
	return nil
}

func (s *ProjectIDService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	pid := pathID(c)

	project, err := dep.TeamClient().GetProjectByID(c, pid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Project not found", err)
	}
	if project.OwnerID != uid {
		return serializer.NewError(serializer.CodeOwnerOnly, "Only the project owner can delete it", nil)
	}

	// 级联删除项目下的所有关联数据
	if err := dep.TeamClient().DeleteProjectCascade(c, pid); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to delete project", err)
	}
	return nil
}

// ============================================================
// 任务
// ============================================================

type (
	CreateTaskService struct {
		ProjectID   int      `json:"project_id" binding:"required"`
		ParentID    *int     `json:"parent_id"`
		Title       string   `json:"title" binding:"required,max=512"`
		Description string   `json:"description"`
		Status      string   `json:"status"`
		Priority    string   `json:"priority"`
		AssigneeID  *string  `json:"assignee_id"`
		DueAt       *string  `json:"due_at"`
		Tags        []string `json:"tags"`
	}
	CreateTaskParamCtx struct{}

	TaskIDService  struct{}
	TaskIDParamCtx struct{}

	UpdateTaskService struct {
		Title       *string   `json:"title"`
		Description *string   `json:"description"`
		Status      *string   `json:"status"`
		Priority    *string   `json:"priority"`
		Progress    *int      `json:"progress"`
		AssigneeID  *string   `json:"assignee_id"`
		DueAt       *string   `json:"due_at"`
		Tags        *[]string `json:"tags"`
	}
	UpdateTaskParamCtx struct{}

	MoveTaskService struct {
		Status    string  `json:"status" binding:"required"`
		SortOrder float64 `json:"sort_order"`
	}
	MoveTaskParamCtx struct{}
)

// decodeAssignee 把请求里的 assignee_id（Cloudreve hashid 字符串）解码成整型用户 ID。
//
// 契约见《讨论区与权限-接口契约.md》§6.1：请求侧与响应侧口径必须一致。
// 响应侧 assignee.id 一直是 hashid（如 "pBiM"），而请求侧曾经是普通整型，
// 于是前端把 hashid 做 Number("pBiM") → NaN → JSON.stringify 后成为 null，
// 后端收到 null 后视作「不指派」——**任务指派因此从未生效过**。
//
// 返回值语义：
//   - raw == nil 或 *raw == ""  → (nil, nil)，表示「不指派 / 清除指派」
//   - 解码成功且 > 0            → (*int, nil)
//   - 解码失败或 <= 0           → (nil, error)，**必须报错，不得静默忽略**
//     （静默忽略会重新制造「看起来成功、实际没生效」的那类缺陷）
func decodeAssignee(dep dependency.Dep, raw *string) (*int, error) {
	if raw == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil, nil
	}

	uid, err := dep.HashIDEncoder().Decode(s, hashid.UserID)
	if err != nil || uid <= 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid assignee_id", err)
	}

	id := int(uid)
	return &id, nil
}

func (s *CreateTaskService) Create(c *gin.Context) (*TaskResponse, error) {
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
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to create tasks", nil)
	}

	title := strings.TrimSpace(s.Title)
	if title == "" {
		return nil, serializer.NewError(serializer.CodeParamErr, "Task title is required", nil)
	}
	if s.Status != "" && !ValidTaskStatus(s.Status) {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid task status", nil)
	}
	if s.Priority != "" && !ValidPriority(s.Priority) {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid priority", nil)
	}

	// assignee_id 是 Cloudreve hashid（契约 §6.1）；省略或空串 = 不指派。
	assigneeID, err := decodeAssignee(dep, s.AssigneeID)
	if err != nil {
		return nil, err
	}
	args := &inventory.NewTeamTaskArgs{
		ProjectID:   s.ProjectID,
		ParentID:    s.ParentID,
		Title:       title,
		Description: s.Description,
		Status:      s.Status,
		Priority:    s.Priority,
		CreatorID:   uid,
		Tags:        s.Tags,
		AssigneeID:  assigneeID,
	}
	if s.DueAt != nil && *s.DueAt != "" {
		if t, perr := time.Parse(time.RFC3339, *s.DueAt); perr == nil {
			args.DueAt = &t
		}
	}

	task, err := dep.TeamClient().CreateTask(c, args)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create task", err)
	}

	_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
		ProjectID: s.ProjectID,
		TaskID:    &task.ID,
		UserID:    uid,
		Action:    "task.created",
		Payload:   map[string]any{"title": task.Title},
	})

	builder := newUserBriefBuilder(c, dep)
	return (&TaskService{}).build(c, dep, task, builder), nil
}

func (s *TaskIDService) Get(c *gin.Context) (*TaskResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	tid := pathID(c)

	task, err := dep.TeamClient().GetTaskByID(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	if _, err := checkAccess(c, dep, task.ProjectID, uid); err != nil {
		return nil, err
	}

	builder := newUserBriefBuilder(c, dep)
	return (&TaskService{}).build(c, dep, task, builder), nil
}

func (s *UpdateTaskService) Update(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	tid := pathID(c)

	task, err := dep.TeamClient().GetTaskByID(c, tid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	access, err := checkAccess(c, dep, task.ProjectID, uid)
	if err != nil {
		return err
	}
	if !CanEdit(access.Role) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to update this task", nil)
	}

	if s.Status != nil {
		if !ValidTaskStatus(*s.Status) {
			return serializer.NewError(serializer.CodeParamErr, "Invalid task status", nil)
		}
	}
	if s.Priority != nil && !ValidPriority(*s.Priority) {
		return serializer.NewError(serializer.CodeParamErr, "Invalid priority", nil)
	}

	statusChanged := s.Status != nil && *s.Status != task.Status

	// assignee_id 是 Cloudreve hashid（契约 §6.1）。
	// 解码放在事务外，这样非法 hashid 能直接以参数错误返回，
	// 而不是被塞进 UpdateTask 的闭包里（闭包无法返回 error，会退化成静默忽略）。
	// s.AssigneeID == nil 表示「本次不修改指派」，因此不赋任何值。
	newAssignee, err := decodeAssignee(dep, s.AssigneeID)
	if err != nil {
		return err
	}
	assigneeProvided := s.AssigneeID != nil

	if err := dep.TeamClient().UpdateTask(c, tid, func(up *ent.TeamTaskUpdateOne) {
		if s.Title != nil {
			up.SetTitle(strings.TrimSpace(*s.Title))
		}
		if s.Description != nil {
			up.SetDescription(*s.Description)
		}
		if s.Priority != nil {
			up.SetPriority(*s.Priority)
		}
		if s.Progress != nil {
			p := *s.Progress
			if p < 0 {
				p = 0
			}
			if p > 100 {
				p = 100
			}
			up.SetProgress(p)
		}
		if s.Tags != nil {
			up.SetTags(*s.Tags)
		}
		// 契约 §6.1：非空 hashid → 指派；空串 → 清除指派；
		// 字段缺省（nil）→ 本次不修改指派。
		if assigneeProvided {
			if newAssignee != nil {
				up.SetAssigneeID(*newAssignee)
			} else {
				up.ClearAssigneeID()
			}
		}
		if s.DueAt != nil {
			if *s.DueAt == "" {
				up.ClearDueAt()
			} else if t, perr := time.Parse(time.RFC3339, *s.DueAt); perr == nil {
				up.SetDueAt(t)
			}
		}
		if s.Status != nil {
			up.SetStatus(*s.Status)
			if *s.Status == inventory.TeamTaskStatusDone {
				up.SetCompletedAt(time.Now())
				up.SetProgress(100)
			} else {
				up.ClearCompletedAt()
			}
		}
	}); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to update task", err)
	}

	if statusChanged {
		_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
			ProjectID: task.ProjectID,
			TaskID:    &tid,
			UserID:    uid,
			Action:    "task.moved",
			Payload:   map[string]any{"from": task.Status, "to": *s.Status, "title": task.Title},
		})
	}
	return nil
}

func (s *TaskIDService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	tid := pathID(c)

	task, err := dep.TeamClient().GetTaskByID(c, tid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	access, err := checkAccess(c, dep, task.ProjectID, uid)
	if err != nil {
		return err
	}
	if !CanEdit(access.Role) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to delete this task", nil)
	}

	if err := dep.TeamClient().DeleteTask(c, tid); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to delete task", err)
	}

	_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
		ProjectID: task.ProjectID,
		UserID:    uid,
		Action:    "task.deleted",
		Payload:   map[string]any{"title": task.Title},
	})
	return nil
}

// Move 看板拖拽：仅改状态与排序，是最轻量的写操作
func (s *MoveTaskService) Move(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	tid := pathID(c)

	if !ValidTaskStatus(s.Status) {
		return serializer.NewError(serializer.CodeParamErr, "Invalid task status", nil)
	}

	task, err := dep.TeamClient().GetTaskByID(c, tid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	access, err := checkAccess(c, dep, task.ProjectID, uid)
	if err != nil {
		return err
	}
	if !CanEdit(access.Role) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to move this task", nil)
	}

	oldStatus := task.Status
	if err := dep.TeamClient().UpdateTask(c, tid, func(up *ent.TeamTaskUpdateOne) {
		up.SetStatus(s.Status).SetSortOrder(s.SortOrder)
		if s.Status == inventory.TeamTaskStatusDone {
			up.SetCompletedAt(time.Now()).SetProgress(100)
		} else {
			up.ClearCompletedAt()
		}
	}); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to move task", err)
	}

	if oldStatus != s.Status {
		_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
			ProjectID: task.ProjectID,
			TaskID:    &tid,
			UserID:    uid,
			Action:    "task.moved",
			Payload:   map[string]any{"from": oldStatus, "to": s.Status, "title": task.Title},
		})
	}
	return nil
}

// TaskService 提供任务响应构建
type TaskService struct{}

func (s *TaskService) build(c context.Context, dep dependency.Dep, t *ent.TeamTask,
	builder *userBriefBuilder) *TaskResponse {

	resp := &TaskResponse{
		ID:          t.ID,
		ProjectID:   t.ProjectID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Progress:    t.Progress,
		SortOrder:   t.SortOrder,
		Tags:        t.Tags,
		Creator:     builder.build(t.CreatorID),
		StartAt:     t.StartAt,
		DueAt:       t.DueAt,
		CompletedAt: t.CompletedAt,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
	if t.ParentID != nil {
		resp.ParentID = t.ParentID
	}
	if t.AssigneeID != nil {
		resp.Assignee = builder.build(*t.AssigneeID)
	}
	if len(resp.Tags) == 0 {
		resp.Tags = []string{}
	}

	if t.DueAt != nil && t.Status != inventory.TeamTaskStatusDone {
		resp.Overdue = t.DueAt.Before(time.Now())
	}

	if collabs, err := dep.TeamClient().ListCollaborators(c, t.ID); err == nil {
		resp.Collaborators = make([]*UserBrief, 0, len(collabs))
		for _, col := range collabs {
			resp.Collaborators = append(resp.Collaborators, builder.build(col.UserID))
		}
	} else {
		resp.Collaborators = []*UserBrief{}
	}
	if comments, err := dep.TeamClient().ListComments(c, t.ID); err == nil {
		resp.CommentTotal = len(comments)
	}
	if attas, err := dep.TeamClient().ListAttachments(c, t.ID); err == nil {
		resp.AttachTotal = len(attas)
	}
	return resp
}

// ============================================================
// 评论
// ============================================================

type (
	CreateCommentService struct {
		Content string `json:"content" binding:"required"`
	}
	CreateCommentParamCtx struct{}

	// CommentParamCtx 评论列表的参数上下文
	CommentParamCtx struct{}
)

func (s *CreateCommentService) Create(c *gin.Context) ([]*CommentResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	tid := pathID(c)

	task, err := dep.TeamClient().GetTaskByID(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	if _, err := checkAccess(c, dep, task.ProjectID, uid); err != nil {
		return nil, err
	}

	content := strings.TrimSpace(s.Content)
	if content == "" {
		return nil, serializer.NewError(serializer.CodeParamErr, "Comment content is required", nil)
	}

	if _, err := dep.TeamClient().CreateComment(c, &inventory.NewTeamCommentArgs{
		TaskID: tid, UserID: uid, Content: content,
	}); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create comment", err)
	}

	_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
		ProjectID: task.ProjectID,
		TaskID:    &tid,
		UserID:    uid,
		Action:    "comment.added",
		Payload:   map[string]any{"title": task.Title},
	})

	// @提及 通知
	notifyMentions(c, dep, content, MentionTarget{
		ProjectID: task.ProjectID,
		Type:      inventory.NotificationMention,
		Title:     "评论中提到了你",
		Body:      task.Title,
		TaskID:    &tid,
	}, uid)

	// 通知任务负责人（若评论者不是本人）
	if task.AssigneeID != nil && *task.AssigneeID != uid {
		pid := task.ProjectID
		notify(c, dep, &inventory.NewTeamNotificationArgs{
			UserID:    *task.AssigneeID,
			ActorID:   uid,
			Type:      inventory.NotificationCommented,
			ProjectID: &pid,
			TaskID:    &tid,
			Title:     "你负责的任务有新评论",
			Body:      task.Title,
		})
	}

	return (&CommentService{}).List(c)
}

// CommentService 评论列表
type CommentService struct{}

func (s *CommentService) List(c *gin.Context) ([]*CommentResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	tid := pathID(c)

	task, err := dep.TeamClient().GetTaskByID(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	if _, err := checkAccess(c, dep, task.ProjectID, uid); err != nil {
		return nil, err
	}

	comments, err := dep.TeamClient().ListComments(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list comments", err)
	}

	builder := newUserBriefBuilder(c, dep)
	res := make([]*CommentResponse, 0, len(comments))
	for _, cm := range comments {
		res = append(res, &CommentResponse{
			ID:        cm.ID,
			TaskID:    cm.TaskID,
			User:      builder.build(cm.UserID),
			Content:   cm.Content,
			CreatedAt: cm.CreatedAt,
			UpdatedAt: cm.UpdatedAt,
		})
	}
	return res, nil
}

// ============================================================
// 任务协作者
//
// 与「负责人（assignee）」的区别：负责人是唯一责任人，协作者是共同参与人，
// 因此协作者不能与负责人重复（重复时按幂等处理，直接忽略）。
// 两条接口都返回刷新后的完整任务，前端无需再拉一次详情。
// ============================================================

type (
	// AddCollaboratorService 添加任务协作者。user_id 为 Cloudreve 用户的 hashid。
	AddCollaboratorService struct {
		UserID string `json:"user_id" binding:"required"`
	}
	AddCollaboratorParamCtx struct{}

	// RemoveCollaboratorService 移除任务协作者。user_id 为 Cloudreve 用户的 hashid。
	RemoveCollaboratorService struct {
		UserID string `form:"user_id" binding:"required"`
	}
	RemoveCollaboratorParamCtx struct{}
)

// Add 添加协作者（需 edit_task 能力；幂等）
func (s *AddCollaboratorService) Add(c *gin.Context) (*TaskResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	tid := pathID(c)

	task, err := dep.TeamClient().GetTaskByID(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	access, err := checkAccess(c, dep, task.ProjectID, uid)
	if err != nil {
		return nil, err
	}
	if !Capability(access.Role, CapEditTask) {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to manage collaborators", nil)
	}

	targetID, err := dep.HashIDEncoder().Decode(s.UserID, hashid.UserID)
	if err != nil || targetID <= 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid user id", err)
	}
	target, err := dep.UserClient().GetByID(c, int(targetID))
	if err != nil || target == nil {
		return nil, serializer.NewError(serializer.CodeUserNotFound, "User not found", err)
	}

	// 只有本项目成员才能成为协作者（项目 owner 天然在成员表里，这里一并兜底）
	if _, merr := dep.TeamClient().GetMember(c, task.ProjectID, int(targetID)); merr != nil &&
		int(targetID) != access.Project.OwnerID {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "The user is not a member of this project", merr)
	}

	// 负责人不需要再作为协作者（语义重复），直接返回当前状态（幂等）
	if task.AssigneeID != nil && *task.AssigneeID == int(targetID) {
		builder := newUserBriefBuilder(c, dep)
		return (&TaskService{}).build(c, dep, task, builder), nil
	}

	// 已存在时 AddCollaborator 内部直接返回，不会重复插入（幂等）
	if err := dep.TeamClient().AddCollaborator(c, tid, int(targetID)); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to add collaborator", err)
	}

	_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
		ProjectID: task.ProjectID,
		TaskID:    &tid,
		UserID:    uid,
		Action:    "task_collaborator_add",
		Payload:   map[string]any{"nickname": target.Nick, "title": task.Title},
	})

	return (&TaskIDService{}).Get(c)
}

// Remove 移除协作者（需 edit_task 能力；幂等）
func (s *RemoveCollaboratorService) Remove(c *gin.Context) (*TaskResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	tid := pathID(c)

	task, err := dep.TeamClient().GetTaskByID(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	access, err := checkAccess(c, dep, task.ProjectID, uid)
	if err != nil {
		return nil, err
	}
	if !Capability(access.Role, CapEditTask) {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to manage collaborators", nil)
	}

	targetID, err := dep.HashIDEncoder().Decode(s.UserID, hashid.UserID)
	if err != nil || targetID <= 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid user id", err)
	}

	// 不存在时 RemoveCollaborator 是空操作（幂等）
	if err := dep.TeamClient().RemoveCollaborator(c, tid, int(targetID)); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to remove collaborator", err)
	}

	_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
		ProjectID: task.ProjectID,
		TaskID:    &tid,
		UserID:    uid,
		Action:    "task_collaborator_remove",
		Payload:   map[string]any{"user_id": int(targetID), "title": task.Title},
	})

	return (&TaskIDService{}).Get(c)
}
