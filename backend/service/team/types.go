package team

import (
	"context"
	"strconv"
	"time"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	itypes "github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// isGlobalAdmin 判断当前用户是否为 Cloudreve 全局管理员。
//
// 团队模块的权限体系与 Cloudreve 的用户组合并（不另建一套）：
// 拥有 GroupPermissionIsAdmin 的用户天然对所有项目具备管理权限，
// 无需被逐个加入项目成员。
//
// 依赖前提：auth 中间件通过 GetLoginUserByID 设置用户上下文，
// 该方法会预加载 group 边，因此 Edges.Group 可直接读取。
func isGlobalAdmin(ctx context.Context) bool {
	u := inventory.UserFromContext(ctx)
	if u == nil || u.Edges.Group == nil {
		return false
	}
	return u.Edges.Group.Permissions.Enabled(int(itypes.GroupPermissionIsAdmin))
}

// pathID 取路径参数 :id。
//
// 团队模块内部实体（项目/任务/附件/评论）使用普通整型 ID：
// 其访问控制由「项目成员校验」保证，无需 hashid 混淆。
// 而 Cloudreve 的实体（用户、文件）仍沿用 hashid。
func pathID(c *gin.Context) int {
	v, err := strconv.Atoi(c.Param("id"))
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// ============================ 响应类型 ============================

// UserBrief 用户简要信息（用于负责人/成员/评论人展示）
type UserBrief struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Email    string `json:"email,omitempty"`
}

// ProjectResponse 项目
type ProjectResponse struct {
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Owner       *UserBrief `json:"owner,omitempty"`
	MyRole      string     `json:"my_role,omitempty"`
	TaskTotal   int        `json:"task_total"`
	DoneTotal   int        `json:"done_total"`
	MemberTotal int        `json:"member_total"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TaskResponse 任务
type TaskResponse struct {
	ID            int          `json:"id"`
	ProjectID     int          `json:"project_id"`
	ParentID      *int         `json:"parent_id,omitempty"`
	Title         string       `json:"title"`
	Description   string       `json:"description"`
	Status        string       `json:"status"`
	Priority      string       `json:"priority"`
	Progress      int          `json:"progress"`
	SortOrder     float64      `json:"sort_order"`
	Tags          []string     `json:"tags"`
	Assignee      *UserBrief   `json:"assignee,omitempty"`
	Creator       *UserBrief   `json:"creator,omitempty"`
	Collaborators []*UserBrief `json:"collaborators,omitempty"`
	StartAt       *time.Time   `json:"start_at,omitempty"`
	DueAt         *time.Time   `json:"due_at,omitempty"`
	CompletedAt   *time.Time   `json:"completed_at,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	CommentTotal  int          `json:"comment_total"`
	AttachTotal   int          `json:"attachment_total"`
	SubTaskTotal  int          `json:"sub_task_total"`
	Overdue       bool         `json:"overdue"`
}

// BoardResponse 看板数据
type BoardResponse struct {
	Project *ProjectResponse `json:"project"`
	Columns []*BoardColumn   `json:"columns"`
	Members []*UserBrief     `json:"members"`
}

// BoardColumn 看板列
type BoardColumn struct {
	Status string          `json:"status"`
	Tasks  []*TaskResponse `json:"tasks"`
}

// CommentResponse 评论
type CommentResponse struct {
	ID        int        `json:"id"`
	TaskID    int        `json:"task_id"`
	User      *UserBrief `json:"user,omitempty"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// AttachmentResponse 附件（引用 Cloudreve 文件）
type AttachmentResponse struct {
	ID        int        `json:"id"`
	TaskID    int        `json:"task_id"`
	FileID    string     `json:"file_id"`
	Name      string     `json:"name"`
	Size      int64      `json:"size"`
	User      *UserBrief `json:"user,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// TopicAttachmentResponse 讨论区话题附件（契约补遗 §C）。
// 与任务附件同构，只是归属对象换成话题。
type TopicAttachmentResponse struct {
	ID        int        `json:"id"`
	TopicID   int        `json:"topic_id"`
	FileID    string     `json:"file_id"`
	Name      string     `json:"name"`
	Size      int64      `json:"size"`
	User      *UserBrief `json:"user,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// MemberResponse 项目成员
type MemberResponse struct {
	User     *UserBrief `json:"user"`
	Role     string     `json:"role"`
	JoinedAt time.Time  `json:"joined_at"`
}

// ActivityResponse 动态
type ActivityResponse struct {
	ID        int            `json:"id"`
	TaskID    *int           `json:"task_id,omitempty"`
	User      *UserBrief     `json:"user,omitempty"`
	Action    string         `json:"action"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// AssigneeStat 分工统计
type AssigneeStat struct {
	User      *UserBrief `json:"user"`
	Total     int        `json:"total"`
	Done      int        `json:"done"`
	Doing     int        `json:"doing"`
	Todo      int        `json:"todo"`
	Overdue   int        `json:"overdue"`
	DoneRate  float64    `json:"done_rate"`
}

// StatsResponse 工作量统计
type StatsResponse struct {
	Total      int             `json:"total"`
	Done       int             `json:"done"`
	Doing      int             `json:"doing"`
	Todo       int             `json:"todo"`
	Overdue    int             `json:"overdue"`
	ByStatus   map[string]int  `json:"by_status"`
	ByAssignee []*AssigneeStat `json:"by_assignee"`
}

// ============================ 讨论区响应类型（契约 §2.1）============================

// TopicResponse 讨论区话题
type TopicResponse struct {
	ID            int        `json:"id"`
	ProjectID     int        `json:"project_id"`
	Title         string     `json:"title"`
	Content       string     `json:"content"`
	Category      string     `json:"category"`
	User          *UserBrief `json:"user,omitempty"`
	IsPinned      bool       `json:"is_pinned"`
	IsLocked      bool       `json:"is_locked"`
	IsResolved    bool       `json:"is_resolved"`
	ReplyTotal    int        `json:"reply_total"`
	ViewTotal     int        `json:"view_total"`
	LastReplyAt   *time.Time `json:"last_reply_at,omitempty"`
	LastReplyUser *UserBrief `json:"last_reply_user,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// PostResponse 讨论区楼层（回复）。floor 首帖为 1，回复从 2 开始。
type PostResponse struct {
	ID        int        `json:"id"`
	TopicID   int        `json:"topic_id"`
	User      *UserBrief `json:"user,omitempty"`
	Content   string     `json:"content"`
	Floor     int        `json:"floor"`
	ParentID  *int       `json:"parent_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TopicListResponse 话题分页列表
type TopicListResponse struct {
	Topics   []*TopicResponse `json:"topics"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// TopicDetailResponse 话题详情（含当前页楼层）
type TopicDetailResponse struct {
	Topic    *TopicResponse  `json:"topic"`
	Posts    []*PostResponse `json:"posts"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// ============================ 管理端响应类型（契约 §3）============================

// AdminProjectResponse 管理端项目概览（含话题数）
type AdminProjectResponse struct {
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Owner       *UserBrief `json:"owner,omitempty"`
	TaskTotal   int        `json:"task_total"`
	DoneTotal   int        `json:"done_total"`
	MemberTotal int        `json:"member_total"`
	TopicTotal  int        `json:"topic_total"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// AdminMemberResponse 管理端成员总表的一行（某个用户 + 某个项目）
type AdminMemberResponse struct {
	ProjectID   int        `json:"project_id"`
	ProjectName string     `json:"project_name"`
	User        *UserBrief `json:"user,omitempty"`
	Role        string     `json:"role"`
	JoinedAt    time.Time  `json:"joined_at"`
}

// AdminProjectListResponse 管理端项目分页列表
type AdminProjectListResponse struct {
	Projects []*AdminProjectResponse `json:"projects"`
	Total    int                     `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
}

// AdminMemberListResponse 管理端成员分页列表
type AdminMemberListResponse struct {
	Members  []*AdminMemberResponse `json:"members"`
	Total    int                    `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
}

// AdminStatsResponse 管理端团队概览统计
type AdminStatsResponse struct {
	ProjectTotal int `json:"project_total"`
	TaskTotal    int `json:"task_total"`
	DoneTotal    int `json:"done_total"`
	MemberTotal  int `json:"member_total"`
	TopicTotal   int `json:"topic_total"`
	PostTotal    int `json:"post_total"`
}

// PermissionResponse 权限矩阵响应：matrix + 固定顺序的能力项 + 可配置角色
type PermissionResponse struct {
	Matrix       RoleMatrix `json:"matrix"`
	Capabilities []string   `json:"capabilities"`
	Roles        []string   `json:"roles"`
}

// ============================ 看板列顺序 ============================

// BoardStatusOrder 看板列的固定顺序
var BoardStatusOrder = []string{
	inventory.TeamTaskStatusTodo,
	inventory.TeamTaskStatusDoing,
	inventory.TeamTaskStatusReview,
	inventory.TeamTaskStatusDone,
}

// ValidTaskStatus 校验状态合法性
func ValidTaskStatus(s string) bool {
	switch s {
	case inventory.TeamTaskStatusTodo,
		inventory.TeamTaskStatusDoing,
		inventory.TeamTaskStatusReview,
		inventory.TeamTaskStatusDone,
		inventory.TeamTaskStatusArchived:
		return true
	}
	return false
}

// ValidPriority 校验优先级合法性
func ValidPriority(s string) bool {
	switch s {
	case inventory.TeamPriorityLow, inventory.TeamPriorityNormal,
		inventory.TeamPriorityHigh, inventory.TeamPriorityUrgent:
		return true
	}
	return false
}

// ValidRole 校验角色合法性
func ValidRole(s string) bool {
	switch s {
	case inventory.TeamRoleOwner, inventory.TeamRoleAdmin,
		inventory.TeamRoleMember, inventory.TeamRoleViewer:
		return true
	}
	return false
}

// CanEdit 判断该角色是否有编辑权限。
//
// 基于权限矩阵实现（契约 §3.2）：create_task || edit_task。
// 默认矩阵下 owner / admin / member 为 true、viewer 为 false，
// 与改造前的硬编码行为一致，调用点无需改动。
func CanEdit(role string) bool {
	return Capability(role, CapCreateTask) || Capability(role, CapEditTask)
}

// CanManage 判断该角色是否有管理权限（改项目、管成员）。
//
// 基于权限矩阵实现（契约 §3.2）：manage_member || manage_project。
func CanManage(role string) bool {
	return Capability(role, CapManageMember) || Capability(role, CapManageProject)
}

// ============================ 公共辅助 ============================

// userBriefBuilder 批量构建用户简要信息，内部带缓存避免重复查询
type userBriefBuilder struct {
	dep   dependency.Dep
	ctx   context.Context
	cache map[int]*UserBrief
}

func newUserBriefBuilder(ctx context.Context, dep dependency.Dep) *userBriefBuilder {
	return &userBriefBuilder{dep: dep, ctx: ctx, cache: make(map[int]*UserBrief)}
}

func (b *userBriefBuilder) build(userID int) *UserBrief {
	if userID <= 0 {
		return nil
	}
	if v, ok := b.cache[userID]; ok {
		return v
	}

	u, err := b.dep.UserClient().GetByID(b.ctx, userID)
	if err != nil || u == nil {
		// 用户已被删除等情况，返回占位，避免整个列表查询失败
		placeholder := &UserBrief{ID: "", Nickname: "#" + strconv.Itoa(userID), Avatar: ""}
		b.cache[userID] = placeholder
		return placeholder
	}

	brief := buildUserBrief(b.dep, u)
	b.cache[userID] = brief
	return brief
}

func buildUserBrief(dep dependency.Dep, u *ent.User) *UserBrief {
	if u == nil {
		return nil
	}
	return &UserBrief{
		ID:       hashid.EncodeUserID(dep.HashIDEncoder(), u.ID),
		Nickname: u.Nick,
		Avatar:   u.Avatar,
		Email:    u.Email,
	}
}

// accessInfo 当前用户对某项目的访问信息
type accessInfo struct {
	Project *ent.TeamProject
	Role    string
	UserID  int
}

// checkAccess 校验当前用户对项目的访问权限。
//
// 权限来源（与 Cloudreve 用户组合并）：
//  1. 项目 owner        → owner
//  2. 项目成员表记录    → 该记录的角色
//  3. Cloudreve 全局管理员 → admin（可管理任意项目）
func checkAccess(ctx context.Context, dep dependency.Dep, projectID, userID int) (*accessInfo, error) {
	project, err := dep.TeamClient().GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Project not found", err)
	}

	// 预热权限矩阵缓存（命中缓存时零成本），保证 CanEdit / CanManage
	// 读到的是管理端配置过的矩阵，而不是进程冷启动时的默认值。
	LoadRoleMatrix(ctx, dep)

	if project.OwnerID == userID {
		return &accessInfo{Project: project, Role: inventory.TeamRoleOwner, UserID: userID}, nil
	}

	member, err := dep.TeamClient().GetMember(ctx, projectID, userID)
	if err != nil {
		if isGlobalAdmin(ctx) {
			return &accessInfo{Project: project, Role: inventory.TeamRoleAdmin, UserID: userID}, nil
		}
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You are not a member of this project", err)
	}

	return &accessInfo{Project: project, Role: member.Role, UserID: userID}, nil
}

// currentUserID 取当前登录用户 ID
func currentUserID(ctx context.Context) (int, error) {
	u := inventory.UserFromContext(ctx)
	if u == nil {
		return 0, serializer.NewError(serializer.CodeCheckLogin, "Login required", nil)
	}
	return u.ID, nil
}
