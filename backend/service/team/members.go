package team

import (
	"strings"
	"time"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 附件（引用 Cloudreve 文件，不复制本体）
// ============================================================

type (
	CreateAttachmentService struct {
		FileID string `json:"file_id" binding:"required"`
	}
	CreateAttachmentParamCtx struct{}

	// AttachmentParamCtx 附件列表的参数上下文
	AttachmentParamCtx struct{}

	AttachmentIDService struct{}
	AttachmentIDParamCtx struct{}
)

func (s *CreateAttachmentService) Create(c *gin.Context) ([]*AttachmentResponse, error) {
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
	if !CanEdit(access.Role) {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to add attachments", nil)
	}

	// 解析 file_id（前端传 hashid），并校验文件存在
	fileID, err := dep.HashIDEncoder().Decode(s.FileID, hashid.FileID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid file id", err)
	}

	file, err := dep.FileClient().GetByID(c, int(fileID))
	if err != nil || file == nil {
		return nil, serializer.NewError(serializer.CodeFileNotFound, "File not found", err)
	}

	// 只允许引用自己的文件，避免越权把别人的文件挂到任务上
	if file.OwnerID != uid {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You can only attach your own files", nil)
	}

	if _, err := dep.TeamClient().CreateAttachment(c, &inventory.NewTeamAttachmentArgs{
		TaskID: tid,
		FileID: int(fileID),
		UserID: uid,
		Name:   file.Name,
		Size:   file.Size,
	}); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to attach file", err)
	}

	_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
		ProjectID: task.ProjectID,
		TaskID:    &tid,
		UserID:    uid,
		Action:    "attachment.added",
		Payload:   map[string]any{"name": file.Name, "title": task.Title},
	})

	return (&AttachmentService{}).List(c)
}

// AttachmentService 附件列表
type AttachmentService struct{}

func (s *AttachmentService) List(c *gin.Context) ([]*AttachmentResponse, error) {
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

	attas, err := dep.TeamClient().ListAttachments(c, tid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list attachments", err)
	}

	builder := newUserBriefBuilder(c, dep)
	res := make([]*AttachmentResponse, 0, len(attas))
	for _, a := range attas {
		res = append(res, &AttachmentResponse{
			ID:        a.ID,
			TaskID:    a.TaskID,
			FileID:    hashid.EncodeFileID(dep.HashIDEncoder(), a.FileID),
			Name:      a.Name,
			Size:      a.Size,
			User:      builder.build(a.UserID),
			CreatedAt: a.CreatedAt,
		})
	}
	return res, nil
}

func (s *AttachmentIDService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return err
	}
	aid := pathID(c)

	// 通过 task 反查归属
	atta, err := dep.TeamClient().GetClient().TeamTaskAttachment.Get(c, aid)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Attachment not found", err)
	}
	task, err := dep.TeamClient().GetTaskByID(c, atta.TaskID)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Task not found", err)
	}
	access, err := checkAccess(c, dep, task.ProjectID, uid)
	if err != nil {
		return err
	}
	// 上传者本人或项目管理者可移除
	if atta.UserID != uid && !CanManage(access.Role) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to remove this attachment", nil)
	}

	if err := dep.TeamClient().DeleteAttachment(c, aid); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to remove attachment", err)
	}
	return nil
}

// ============================================================
// 成员
// ============================================================

type (
	AddMemberService struct {
		UserID string `json:"user_id" binding:"required"`
		Role   string `json:"role"`
	}
	AddMemberParamCtx struct{}

	MemberUserIDService struct{}
	MemberUserIDParamCtx struct{}

	// RemoveMemberService 移除/退出项目成员。user_id 为 Cloudreve 用户的 hashid。
	RemoveMemberService struct {
		UserID string `form:"user_id" binding:"required"`
	}
	RemoveMemberParamCtx struct{}
)

func (s *MemberService) List(c *gin.Context) ([]*MemberResponse, error) {
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
	res := make([]*MemberResponse, 0)

	// owner 始终在列表中
	res = append(res, &MemberResponse{
		User:     builder.build(access.Project.OwnerID),
		Role:     inventory.TeamRoleOwner,
		JoinedAt: access.Project.CreatedAt,
	})

	members, err := dep.TeamClient().ListMembers(c, pid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list members", err)
	}
	for _, m := range members {
		if m.UserID == access.Project.OwnerID {
			continue
		}
		res = append(res, &MemberResponse{
			User:     builder.build(m.UserID),
			Role:     m.Role,
			JoinedAt: m.CreatedAt,
		})
	}
	return res, nil
}

// MemberService 成员列表
type MemberService struct{}

// MemberParamCtx 成员列表的参数上下文
type MemberParamCtx struct{}

func (s *AddMemberService) Add(c *gin.Context) error {
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
		return serializer.NewError(serializer.CodeNoPermissionErr, "Only owner or admin can manage members", nil)
	}

	targetID, err := dep.HashIDEncoder().Decode(s.UserID, hashid.UserID)
	if err != nil {
		return serializer.NewError(serializer.CodeParamErr, "Invalid user id", err)
	}

	target, err := dep.UserClient().GetByID(c, int(targetID))
	if err != nil || target == nil {
		return serializer.NewError(serializer.CodeUserNotFound, "User not found", err)
	}

	role := s.Role
	if role == "" {
		role = inventory.TeamRoleMember
	}
	if !ValidRole(role) {
		return serializer.NewError(serializer.CodeParamErr, "Invalid role", nil)
	}
	if role == inventory.TeamRoleOwner {
		return serializer.NewError(serializer.CodeParamErr, "Cannot assign owner role directly", nil)
	}

	if _, err := dep.TeamClient().UpsertMember(c, pid, int(targetID), role); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to add member", err)
	}

	_, _ = dep.TeamClient().CreateActivity(c, &inventory.NewTeamActivityArgs{
		ProjectID: pid,
		UserID:    uid,
		Action:    "member.added",
		Payload:   map[string]any{"nickname": target.Nick, "role": role},
	})
	return nil
}

func (s *RemoveMemberService) Remove(c *gin.Context) error {
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

	targetID, err := dep.HashIDEncoder().Decode(s.UserID, hashid.UserID)
	if err != nil || targetID <= 0 {
		return serializer.NewError(serializer.CodeParamErr, "Invalid user id", err)
	}

	// 本人可以退出项目；管理者可以移除他人
	if targetID != uid && !CanManage(access.Role) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "You have no permission to remove this member", nil)
	}
	if targetID == access.Project.OwnerID {
		return serializer.NewError(serializer.CodeParamErr, "Project owner cannot be removed", nil)
	}

	if err := dep.TeamClient().DeleteMember(c, pid, targetID); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to remove member", err)
	}
	return nil
}

// ============================================================
// 动态
// ============================================================

type (
	ListActivityService struct {
		Limit int `form:"limit"`
	}
	ListActivityParamCtx struct{}
)

func (s *ListActivityService) List(c *gin.Context) ([]*ActivityResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	pid := pathID(c)

	if _, err := checkAccess(c, dep, pid, uid); err != nil {
		return nil, err
	}

	items, err := dep.TeamClient().ListActivities(c, pid, s.Limit)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list activities", err)
	}

	builder := newUserBriefBuilder(c, dep)
	res := make([]*ActivityResponse, 0, len(items))
	for _, a := range items {
		res = append(res, &ActivityResponse{
			ID:        a.ID,
			TaskID:    a.TaskID,
			User:      builder.build(a.UserID),
			Action:    a.Action,
			Payload:   a.Payload,
			CreatedAt: a.CreatedAt,
		})
	}
	return res, nil
}

// ============================================================
// 统计（分工视图）
// ============================================================

type (
	StatsService struct{}
	StatsParamCtx struct{}
)

func (s *StatsService) Get(c *gin.Context) (*StatsResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}
	pid := pathID(c)

	if _, err := checkAccess(c, dep, pid, uid); err != nil {
		return nil, err
	}

	tasks, err := dep.TeamClient().ListTasks(c, pid, nil)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load tasks", err)
	}

	now := time.Now()
	resp := &StatsResponse{ByStatus: make(map[string]int)}
	perUser := make(map[int]*AssigneeStat)

	for _, t := range tasks {
		resp.Total++
		resp.ByStatus[t.Status]++
		switch t.Status {
		case inventory.TeamTaskStatusDone:
			resp.Done++
		case inventory.TeamTaskStatusDoing:
			resp.Doing++
		case inventory.TeamTaskStatusTodo:
			resp.Todo++
		}
		overdue := t.DueAt != nil && t.Status != inventory.TeamTaskStatusDone && t.DueAt.Before(now)
		if overdue {
			resp.Overdue++
		}

		if t.AssigneeID == nil {
			continue
		}
		st, ok := perUser[*t.AssigneeID]
		if !ok {
			st = &AssigneeStat{}
			perUser[*t.AssigneeID] = st
		}
		st.Total++
		if overdue {
			st.Overdue++
		}
		switch t.Status {
		case inventory.TeamTaskStatusDone:
			st.Done++
		case inventory.TeamTaskStatusDoing:
			st.Doing++
		default:
			st.Todo++
		}
	}

	builder := newUserBriefBuilder(c, dep)
	resp.ByAssignee = make([]*AssigneeStat, 0, len(perUser))
	for userID, st := range perUser {
		st.User = builder.build(userID)
		if st.Total > 0 {
			st.DoneRate = float64(st.Done) / float64(st.Total)
		}
		resp.ByAssignee = append(resp.ByAssignee, st)
	}
	return resp, nil
}

// ============================================================
// 我参与的任务（跨项目工作台）
// ============================================================

type (
	MyTasksService struct {
		Status string `form:"status"`
	}
	MyTasksParamCtx struct{}
)

func (s *MyTasksService) List(c *gin.Context) ([]*TaskResponse, error) {
	dep := dependency.FromContext(c)
	uid, err := currentUserID(c)
	if err != nil {
		return nil, err
	}

	projects, err := dep.TeamClient().ListProjects(c, uid, false)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list projects", err)
	}

	status := strings.TrimSpace(s.Status)
	if status != "" && !ValidTaskStatus(status) {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid status filter", nil)
	}

	builder := newUserBriefBuilder(c, dep)
	taskSvc := &TaskService{}
	res := make([]*TaskResponse, 0)

	for _, p := range projects {
		filter := &inventory.TeamTaskFilter{AssigneeID: &uid}
		if status != "" {
			filter.Status = status
		}
		tasks, err := dep.TeamClient().ListTasks(c, p.ID, filter)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			res = append(res, taskSvc.build(c, dep, t, builder))
		}
	}
	return res, nil
}
