package inventory

import (
	"context"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/teamactivity"
	"github.com/cloudreve/Cloudreve/v4/ent/teammember"
	"github.com/cloudreve/Cloudreve/v4/ent/teampost"
	"github.com/cloudreve/Cloudreve/v4/ent/teamproject"
	"github.com/cloudreve/Cloudreve/v4/ent/teamtask"
	"github.com/cloudreve/Cloudreve/v4/ent/teamtaskattachment"
	"github.com/cloudreve/Cloudreve/v4/ent/teamtaskcollaborator"
	"github.com/cloudreve/Cloudreve/v4/ent/teamtaskcomment"
	"github.com/cloudreve/Cloudreve/v4/ent/teamtopic"
	"github.com/cloudreve/Cloudreve/v4/ent/teamtopicattachment"
	"github.com/cloudreve/Cloudreve/v4/ent/user"
	"github.com/cloudreve/Cloudreve/v4/pkg/conf"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
)

// 团队成员角色
const (
	TeamRoleOwner  = "owner"
	TeamRoleAdmin  = "admin"
	TeamRoleMember = "member"
	TeamRoleViewer = "viewer"
)

// 任务状态
const (
	TeamTaskStatusTodo     = "todo"
	TeamTaskStatusDoing    = "doing"
	TeamTaskStatusReview   = "review"
	TeamTaskStatusDone     = "done"
	TeamTaskStatusArchived = "archived"
)

// 任务优先级
const (
	TeamPriorityLow    = "low"
	TeamPriorityNormal = "normal"
	TeamPriorityHigh   = "high"
	TeamPriorityUrgent = "urgent"
)

// 讨论区话题分类
const (
	TeamTopicCategoryDiscuss  = "discuss"
	TeamTopicCategoryAnnounce = "announce"
	TeamTopicCategoryQuestion = "question"
	TeamTopicCategoryShare    = "share"
)

type (
	// NewTeamProjectArgs 创建项目的入参
	NewTeamProjectArgs struct {
		Name        string
		Description string
		OwnerID     int
	}

	// NewTeamTaskArgs 创建任务的入参
	NewTeamTaskArgs struct {
		ProjectID   int
		ParentID    *int
		Title       string
		Description string
		Status      string
		Priority    string
		AssigneeID  *int
		CreatorID   int
		DueAt       *time.Time
		Tags        []string
	}

	// TeamTaskFilter 任务查询过滤条件
	TeamTaskFilter struct {
		Status     string
		AssigneeID *int
		Keyword    string
		ParentID   *int
	}

	// NewTeamCommentArgs 创建评论入参
	NewTeamCommentArgs struct {
		TaskID  int
		UserID  int
		Content string
	}

	// NewTeamAttachmentArgs 创建附件入参
	NewTeamAttachmentArgs struct {
		TaskID int
		FileID int
		UserID int
		Name   string
		Size   int64
	}

	// NewTeamTopicAttachmentArgs 创建讨论区话题附件入参。
	// 与任务附件同构：只存 Cloudreve 文件引用，不复制文件。
	NewTeamTopicAttachmentArgs struct {
		TopicID int
		FileID  int
		UserID  int
		Name    string
		Size    int64
	}

	// NewTeamActivityArgs 写入动态入参
	NewTeamActivityArgs struct {
		ProjectID int
		TaskID    *int
		UserID    int
		Action    string
		Payload   map[string]any
	}

	// NewTeamTopicArgs 创建讨论区话题入参
	NewTeamTopicArgs struct {
		ProjectID int
		Title     string
		Content   string
		Category  string
		UserID    int
	}

	// TeamTopicFilter 话题查询过滤条件。
	// Offset / Limit 由 service 层按分页换算，0 表示不限。
	TeamTopicFilter struct {
		Keyword  string
		Category string
		Offset   int
		Limit    int
	}

	// NewTeamPostArgs 创建楼层（回复）入参
	NewTeamPostArgs struct {
		TopicID  int
		UserID   int
		Content  string
		Floor    int
		ParentID *int
	}

	// TeamClient 团队协作模块数据访问接口
	TeamClient interface {
		TxOperator

		// ---- 项目 ----
		CreateProject(ctx context.Context, args *NewTeamProjectArgs) (*ent.TeamProject, error)
		GetProjectByID(ctx context.Context, id int) (*ent.TeamProject, error)
		ListProjects(ctx context.Context, userID int, includeArchived bool) ([]*ent.TeamProject, error)
		UpdateProject(ctx context.Context, id int, set func(*ent.TeamProjectUpdateOne)) error
		DeleteProject(ctx context.Context, id int) error
		// DeleteProjectCascade 级联删除项目及其全部关联数据
		// （任务、参与人、评论、附件、成员、动态），避免残留孤儿记录。
		DeleteProjectCascade(ctx context.Context, id int) error

		// ---- 成员 ----
		UpsertMember(ctx context.Context, projectID, userID int, role string) (*ent.TeamMember, error)
		GetMember(ctx context.Context, projectID, userID int) (*ent.TeamMember, error)
		ListMembers(ctx context.Context, projectID int) ([]*ent.TeamMember, error)
		DeleteMember(ctx context.Context, projectID, userID int) error
		ListProjectIDsByUser(ctx context.Context, userID int) ([]int, error)

		// ---- 任务 ----
		CreateTask(ctx context.Context, args *NewTeamTaskArgs) (*ent.TeamTask, error)
		GetTaskByID(ctx context.Context, id int) (*ent.TeamTask, error)
		ListTasks(ctx context.Context, projectID int, f *TeamTaskFilter) ([]*ent.TeamTask, error)
		UpdateTask(ctx context.Context, id int, set func(*ent.TeamTaskUpdateOne)) error
		DeleteTask(ctx context.Context, id int) error
		CountTaskByAssignee(ctx context.Context, projectID int) (map[int]int, error)
		CountTaskByStatus(ctx context.Context, projectID int) (map[string]int, error)

		// ---- 参与人 ----
		AddCollaborator(ctx context.Context, taskID, userID int) error
		ListCollaborators(ctx context.Context, taskID int) ([]*ent.TeamTaskCollaborator, error)
		RemoveCollaborator(ctx context.Context, taskID, userID int) error

		// ---- 评论 ----
		CreateComment(ctx context.Context, args *NewTeamCommentArgs) (*ent.TeamTaskComment, error)
		ListComments(ctx context.Context, taskID int) ([]*ent.TeamTaskComment, error)

		// ---- 附件 ----
		CreateAttachment(ctx context.Context, args *NewTeamAttachmentArgs) (*ent.TeamTaskAttachment, error)
		ListAttachments(ctx context.Context, taskID int) ([]*ent.TeamTaskAttachment, error)
		// GetAttachmentByID 按附件 ID 取任务附件。
		// B+ 的附件访问通道需要由附件反查其所属项目（attachment → task → project_id），
		// 再据此判定请求者是否为该项目成员，故需要单条查询。
		GetAttachmentByID(ctx context.Context, id int) (*ent.TeamTaskAttachment, error)
		DeleteAttachment(ctx context.Context, id int) error

		// ---- 讨论区附件 ----
		CreateTopicAttachment(ctx context.Context, args *NewTeamTopicAttachmentArgs) (*ent.TeamTopicAttachment, error)
		ListTopicAttachments(ctx context.Context, topicID int) ([]*ent.TeamTopicAttachment, error)
		GetTopicAttachmentByID(ctx context.Context, id int) (*ent.TeamTopicAttachment, error)
		DeleteTopicAttachment(ctx context.Context, id int) error

		// ---- 动态 ----
		CreateActivity(ctx context.Context, args *NewTeamActivityArgs) (*ent.TeamActivity, error)
		ListActivities(ctx context.Context, projectID int, limit int) ([]*ent.TeamActivity, error)

		// ---- 讨论区：话题 ----
		CreateTopic(ctx context.Context, args *NewTeamTopicArgs) (*ent.TeamTopic, error)
		GetTopicByID(ctx context.Context, id int) (*ent.TeamTopic, error)
		// ListTopics 列出话题。projectID <= 0 时不限项目（管理端用）。
		// 排序固定为：置顶优先，其余按「最后回复时间（无回复则创建时间）」倒序。
		ListTopics(ctx context.Context, projectID int, f *TeamTopicFilter) ([]*ent.TeamTopic, error)
		CountTopics(ctx context.Context, projectID int, f *TeamTopicFilter) (int, error)
		UpdateTopic(ctx context.Context, id int, set func(*ent.TeamTopicUpdateOne)) error
		// DeleteTopicCascade 删除话题及其全部楼层，避免残留孤儿楼层。
		DeleteTopicCascade(ctx context.Context, id int) error
		// IncreaseTopicView 原子地把 view_total +1
		IncreaseTopicView(ctx context.Context, id int) error
		// IncreaseTopicReply 原子地更新回复计数与最后回复信息
		// （reply_total +1 / last_reply_at / last_reply_user_id）
		IncreaseTopicReply(ctx context.Context, id int, userID int, at time.Time) error
		// DecreaseTopicReply 原子地把 reply_total -1（带下限保护，不会减成负数）
		DecreaseTopicReply(ctx context.Context, id int) error
		// ClearTopicLastReply 清空最后回复信息（最后一条回复被删除时使用）
		ClearTopicLastReply(ctx context.Context, id int) error

		// ---- 讨论区：楼层 ----
		CreatePost(ctx context.Context, args *NewTeamPostArgs) (*ent.TeamPost, error)
		GetPostByID(ctx context.Context, id int) (*ent.TeamPost, error)
		// ListPosts 按楼层号升序分页取楼层；offset <= 0 表示从第一楼开始，limit <= 0 表示不限。
		ListPosts(ctx context.Context, topicID int, offset, limit int) ([]*ent.TeamPost, error)
		CountPosts(ctx context.Context, topicID int) (int, error)
		// NextPostFloor 计算下一个楼层号（首帖占 1 楼，因此回复从 2 开始）
		NextPostFloor(ctx context.Context, topicID int) (int, error)
		// LatestPost 取楼层号最大的回复，无回复时返回 (nil, nil)
		LatestPost(ctx context.Context, topicID int) (*ent.TeamPost, error)
		DeletePost(ctx context.Context, id int) error

		// ---- 管理端统计（全站，不做可见性过滤）----
		ListAllProjects(ctx context.Context, keyword string, offset, limit int) ([]*ent.TeamProject, error)
		CountProjects(ctx context.Context, keyword string) (int, error)
		ListAllMembers(ctx context.Context, keyword string, offset, limit int) ([]*ent.TeamMember, error)
		CountAllMembers(ctx context.Context, keyword string) (int, error)
		CountTasksByStatus(ctx context.Context) (map[string]int, error)
		CountAllTopics(ctx context.Context) (int, error)
		CountAllPosts(ctx context.Context) (int, error)
	}
)

func NewTeamClient(client *ent.Client, dbType conf.DBType, hasher hashid.Encoder) TeamClient {
	return &teamClient{
		client:      client,
		hasher:      hasher,
		maxSQlParam: sqlParamLimit(dbType),
	}
}

type teamClient struct {
	maxSQlParam int
	client      *ent.Client
	hasher      hashid.Encoder
}

func (c *teamClient) SetClient(newClient *ent.Client) TxOperator {
	return &teamClient{client: newClient, hasher: c.hasher, maxSQlParam: c.maxSQlParam}
}

func (c *teamClient) GetClient() *ent.Client {
	return c.client
}

// ============================ 项目 ============================

func (c *teamClient) CreateProject(ctx context.Context, args *NewTeamProjectArgs) (*ent.TeamProject, error) {
	return c.client.TeamProject.Create().
		SetName(args.Name).
		SetDescription(args.Description).
		SetOwnerID(args.OwnerID).
		SetStatus("active").
		Save(ctx)
}

func (c *teamClient) GetProjectByID(ctx context.Context, id int) (*ent.TeamProject, error) {
	return c.client.TeamProject.Query().
		Where(teamproject.ID(id)).
		First(ctx)
}

func (c *teamClient) ListProjects(ctx context.Context, userID int, includeArchived bool) ([]*ent.TeamProject, error) {
	q := c.client.TeamProject.Query()
	if !includeArchived {
		q = q.Where(teamproject.StatusEQ("active"))
	}

	if userID > 0 {
		// 可见范围：自己拥有的 + 自己作为成员加入的
		memberIDs, err := c.ListProjectIDsByUser(ctx, userID)
		if err != nil {
			return nil, err
		}

		if len(memberIDs) > 0 {
			q = q.Where(
				teamproject.Or(
					teamproject.OwnerID(userID),
					teamproject.IDIn(memberIDs...),
				),
			)
		} else {
			q = q.Where(teamproject.OwnerID(userID))
		}
	}

	return q.Order(ent.Desc(teamproject.FieldUpdatedAt)).All(ctx)
}

func (c *teamClient) UpdateProject(ctx context.Context, id int, set func(*ent.TeamProjectUpdateOne)) error {
	up := c.client.TeamProject.UpdateOneID(id)
	set(up)
	return up.Exec(ctx)
}

func (c *teamClient) DeleteProject(ctx context.Context, id int) error {
	return c.client.TeamProject.DeleteOneID(id).Exec(ctx)
}

// DeleteProjectCascade 级联删除项目及其全部关联数据。
// 逐表显式清理，避免留下孤儿记录（成员/动态/评论等）。
func (c *teamClient) DeleteProjectCascade(ctx context.Context, id int) error {
	// 先取出该项目下所有任务 ID
	taskIDs, err := c.client.TeamTask.Query().
		Where(teamtask.ProjectID(id)).
		Select(teamtask.FieldID).
		Ints(ctx)
	if err != nil {
		return err
	}

	if len(taskIDs) > 0 {
		if _, err := c.client.TeamTaskComment.Delete().
			Where(teamtaskcomment.TaskIDIn(taskIDs...)).Exec(ctx); err != nil {
			return err
		}
		if _, err := c.client.TeamTaskAttachment.Delete().
			Where(teamtaskattachment.TaskIDIn(taskIDs...)).Exec(ctx); err != nil {
			return err
		}
		if _, err := c.client.TeamTaskCollaborator.Delete().
			Where(teamtaskcollaborator.TaskIDIn(taskIDs...)).Exec(ctx); err != nil {
			return err
		}
		if _, err := c.client.TeamTask.Delete().
			Where(teamtask.ProjectID(id)).Exec(ctx); err != nil {
			return err
		}
	}

	if _, err := c.client.TeamMember.Delete().
		Where(teammember.ProjectID(id)).Exec(ctx); err != nil {
		return err
	}
	if _, err := c.client.TeamActivity.Delete().
		Where(teamactivity.ProjectID(id)).Exec(ctx); err != nil {
		return err
	}

	// 讨论区：先删楼层再删话题（楼层按 topic_id 关联）
	if _, err := c.client.TeamPost.Delete().
		Where(teampost.HasTopicWith(teamtopic.ProjectID(id))).Exec(ctx); err != nil {
		return err
	}
	if _, err := c.client.TeamTopic.Delete().
		Where(teamtopic.ProjectID(id)).Exec(ctx); err != nil {
		return err
	}

	return c.client.TeamProject.DeleteOneID(id).Exec(ctx)
}

// ============================ 成员 ============================

func (c *teamClient) UpsertMember(ctx context.Context, projectID, userID int, role string) (*ent.TeamMember, error) {
	existing, err := c.GetMember(ctx, projectID, userID)
	if err == nil && existing != nil {
		if err := c.client.TeamMember.UpdateOneID(existing.ID).SetRole(role).Exec(ctx); err != nil {
			return nil, err
		}
		return c.GetMember(ctx, projectID, userID)
	}

	return c.client.TeamMember.Create().
		SetProjectID(projectID).
		SetUserID(userID).
		SetRole(role).
		Save(ctx)
}

func (c *teamClient) GetMember(ctx context.Context, projectID, userID int) (*ent.TeamMember, error) {
	return c.client.TeamMember.Query().
		Where(teammember.ProjectID(projectID), teammember.UserID(userID)).
		First(ctx)
}

func (c *teamClient) ListMembers(ctx context.Context, projectID int) ([]*ent.TeamMember, error) {
	return c.client.TeamMember.Query().
		Where(teammember.ProjectID(projectID)).
		Order(ent.Asc(teammember.FieldID)).
		All(ctx)
}

func (c *teamClient) DeleteMember(ctx context.Context, projectID, userID int) error {
	_, err := c.client.TeamMember.Delete().
		Where(teammember.ProjectID(projectID), teammember.UserID(userID)).
		Exec(ctx)
	return err
}

func (c *teamClient) ListProjectIDsByUser(ctx context.Context, userID int) ([]int, error) {
	ids, err := c.client.TeamMember.Query().
		Where(teammember.UserID(userID)).
		Select(teammember.FieldProjectID).
		Ints(ctx)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ============================ 任务 ============================

func (c *teamClient) CreateTask(ctx context.Context, args *NewTeamTaskArgs) (*ent.TeamTask, error) {
	b := c.client.TeamTask.Create().
		SetProjectID(args.ProjectID).
		SetTitle(args.Title).
		SetDescription(args.Description).
		SetCreatorID(args.CreatorID).
		SetStatus(orDefault(args.Status, TeamTaskStatusTodo)).
		SetPriority(orDefault(args.Priority, TeamPriorityNormal))

	if args.ParentID != nil {
		b = b.SetParentID(*args.ParentID)
	}
	if args.AssigneeID != nil {
		b = b.SetAssigneeID(*args.AssigneeID)
	}
	if args.DueAt != nil {
		b = b.SetDueAt(*args.DueAt)
	}
	if len(args.Tags) > 0 {
		b = b.SetTags(args.Tags)
	}

	return b.Save(ctx)
}

func (c *teamClient) GetTaskByID(ctx context.Context, id int) (*ent.TeamTask, error) {
	return c.client.TeamTask.Query().Where(teamtask.ID(id)).First(ctx)
}

func (c *teamClient) ListTasks(ctx context.Context, projectID int, f *TeamTaskFilter) ([]*ent.TeamTask, error) {
	q := c.client.TeamTask.Query().Where(teamtask.ProjectID(projectID))

	if f != nil {
		if f.Status != "" {
			q = q.Where(teamtask.StatusEQ(f.Status))
		}
		if f.AssigneeID != nil {
			q = q.Where(teamtask.AssigneeID(*f.AssigneeID))
		}
		if f.Keyword != "" {
			q = q.Where(teamtask.TitleContainsFold(f.Keyword))
		}
		if f.ParentID != nil {
			q = q.Where(teamtask.ParentID(*f.ParentID))
		}
	}

	return q.Order(
		ent.Asc(teamtask.FieldSortOrder),
		ent.Desc(teamtask.FieldID),
	).All(ctx)
}

func (c *teamClient) UpdateTask(ctx context.Context, id int, set func(*ent.TeamTaskUpdateOne)) error {
	up := c.client.TeamTask.UpdateOneID(id)
	set(up)
	return up.Exec(ctx)
}

func (c *teamClient) DeleteTask(ctx context.Context, id int) error {
	// 级联清理关联数据
	if _, err := c.client.TeamTaskComment.Delete().Where(teamtaskcomment.TaskID(id)).Exec(ctx); err != nil {
		return err
	}
	if _, err := c.client.TeamTaskAttachment.Delete().Where(teamtaskattachment.TaskID(id)).Exec(ctx); err != nil {
		return err
	}
	if _, err := c.client.TeamTaskCollaborator.Delete().Where(teamtaskcollaborator.TaskID(id)).Exec(ctx); err != nil {
		return err
	}
	return c.client.TeamTask.DeleteOneID(id).Exec(ctx)
}

func (c *teamClient) CountTaskByAssignee(ctx context.Context, projectID int) (map[int]int, error) {
	type row struct {
		AssigneeID int `json:"assignee_id"`
		Count      int `json:"count"`
	}
	var rows []row
	err := c.client.TeamTask.Query().
		Where(teamtask.ProjectID(projectID), teamtask.AssigneeIDNotNil()).
		GroupBy(teamtask.FieldAssigneeID).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}

	res := make(map[int]int, len(rows))
	for _, r := range rows {
		res[r.AssigneeID] = r.Count
	}
	return res, nil
}

func (c *teamClient) CountTaskByStatus(ctx context.Context, projectID int) (map[string]int, error) {
	type row struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	var rows []row
	err := c.client.TeamTask.Query().
		Where(teamtask.ProjectID(projectID)).
		GroupBy(teamtask.FieldStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}

	res := make(map[string]int, len(rows))
	for _, r := range rows {
		res[r.Status] = r.Count
	}
	return res, nil
}

// ============================ 参与人 ============================

func (c *teamClient) AddCollaborator(ctx context.Context, taskID, userID int) error {
	exist, err := c.client.TeamTaskCollaborator.Query().
		Where(teamtaskcollaborator.TaskID(taskID), teamtaskcollaborator.UserID(userID)).
		Exist(ctx)
	if err != nil {
		return err
	}
	if exist {
		return nil
	}

	return c.client.TeamTaskCollaborator.Create().
		SetTaskID(taskID).
		SetUserID(userID).
		Exec(ctx)
}

func (c *teamClient) ListCollaborators(ctx context.Context, taskID int) ([]*ent.TeamTaskCollaborator, error) {
	return c.client.TeamTaskCollaborator.Query().
		Where(teamtaskcollaborator.TaskID(taskID)).
		All(ctx)
}

func (c *teamClient) RemoveCollaborator(ctx context.Context, taskID, userID int) error {
	_, err := c.client.TeamTaskCollaborator.Delete().
		Where(teamtaskcollaborator.TaskID(taskID), teamtaskcollaborator.UserID(userID)).
		Exec(ctx)
	return err
}

// ============================ 评论 ============================

func (c *teamClient) CreateComment(ctx context.Context, args *NewTeamCommentArgs) (*ent.TeamTaskComment, error) {
	return c.client.TeamTaskComment.Create().
		SetTaskID(args.TaskID).
		SetUserID(args.UserID).
		SetContent(args.Content).
		Save(ctx)
}

func (c *teamClient) ListComments(ctx context.Context, taskID int) ([]*ent.TeamTaskComment, error) {
	return c.client.TeamTaskComment.Query().
		Where(teamtaskcomment.TaskID(taskID)).
		Order(ent.Asc(teamtaskcomment.FieldCreatedAt)).
		All(ctx)
}

// ============================ 附件 ============================

func (c *teamClient) CreateAttachment(ctx context.Context, args *NewTeamAttachmentArgs) (*ent.TeamTaskAttachment, error) {
	return c.client.TeamTaskAttachment.Create().
		SetTaskID(args.TaskID).
		SetFileID(args.FileID).
		SetUserID(args.UserID).
		SetName(args.Name).
		SetSize(args.Size).
		Save(ctx)
}

func (c *teamClient) ListAttachments(ctx context.Context, taskID int) ([]*ent.TeamTaskAttachment, error) {
	return c.client.TeamTaskAttachment.Query().
		Where(teamtaskattachment.TaskID(taskID)).
		Order(ent.Asc(teamtaskattachment.FieldID)).
		All(ctx)
}

// GetAttachmentByID 按附件 ID 取单条任务附件（软删除记录不可见）。
func (c *teamClient) GetAttachmentByID(ctx context.Context, id int) (*ent.TeamTaskAttachment, error) {
	return c.client.TeamTaskAttachment.Query().
		Where(teamtaskattachment.ID(id)).
		Only(ctx)
}

func (c *teamClient) DeleteAttachment(ctx context.Context, id int) error {
	return c.client.TeamTaskAttachment.DeleteOneID(id).Exec(ctx)
}

// ======================== 讨论区附件 ========================

func (c *teamClient) CreateTopicAttachment(ctx context.Context,
	args *NewTeamTopicAttachmentArgs) (*ent.TeamTopicAttachment, error) {
	return c.client.TeamTopicAttachment.Create().
		SetTopicID(args.TopicID).
		SetFileID(args.FileID).
		SetUserID(args.UserID).
		SetName(args.Name).
		SetSize(args.Size).
		Save(ctx)
}

func (c *teamClient) ListTopicAttachments(ctx context.Context, topicID int) ([]*ent.TeamTopicAttachment, error) {
	return c.client.TeamTopicAttachment.Query().
		Where(teamtopicattachment.TopicID(topicID)).
		Order(ent.Asc(teamtopicattachment.FieldID)).
		All(ctx)
}

func (c *teamClient) GetTopicAttachmentByID(ctx context.Context, id int) (*ent.TeamTopicAttachment, error) {
	return c.client.TeamTopicAttachment.Get(ctx, id)
}

func (c *teamClient) DeleteTopicAttachment(ctx context.Context, id int) error {
	return c.client.TeamTopicAttachment.DeleteOneID(id).Exec(ctx)
}

// ============================ 动态 ============================

func (c *teamClient) CreateActivity(ctx context.Context, args *NewTeamActivityArgs) (*ent.TeamActivity, error) {
	b := c.client.TeamActivity.Create().
		SetProjectID(args.ProjectID).
		SetUserID(args.UserID).
		SetAction(args.Action)

	if args.TaskID != nil {
		b = b.SetTaskID(*args.TaskID)
	}
	if args.Payload != nil {
		b = b.SetPayload(args.Payload)
	}

	return b.Save(ctx)
}

func (c *teamClient) ListActivities(ctx context.Context, projectID int, limit int) ([]*ent.TeamActivity, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return c.client.TeamActivity.Query().
		Where(teamactivity.ProjectID(projectID)).
		Order(ent.Desc(teamactivity.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
}

// ============================ 讨论区：话题 ============================

func (c *teamClient) CreateTopic(ctx context.Context, args *NewTeamTopicArgs) (*ent.TeamTopic, error) {
	return c.client.TeamTopic.Create().
		SetProjectID(args.ProjectID).
		SetTitle(args.Title).
		SetContent(args.Content).
		SetCategory(orDefault(args.Category, TeamTopicCategoryDiscuss)).
		SetUserID(args.UserID).
		Save(ctx)
}

func (c *teamClient) GetTopicByID(ctx context.Context, id int) (*ent.TeamTopic, error) {
	return c.client.TeamTopic.Query().
		Where(teamtopic.ID(id)).
		First(ctx)
}

// topicsQuery 组装话题的过滤条件（列表与计数共用）。
func (c *teamClient) topicsQuery(projectID int, f *TeamTopicFilter) *ent.TeamTopicQuery {
	q := c.client.TeamTopic.Query()
	if projectID > 0 {
		q = q.Where(teamtopic.ProjectID(projectID))
	}
	if f != nil {
		if f.Category != "" {
			q = q.Where(teamtopic.CategoryEQ(f.Category))
		}
		if f.Keyword != "" {
			// keyword 同时匹配标题与正文（契约 §2.2）
			q = q.Where(teamtopic.Or(
				teamtopic.TitleContainsFold(f.Keyword),
				teamtopic.ContentContainsFold(f.Keyword),
			))
		}
	}
	return q
}

func (c *teamClient) ListTopics(ctx context.Context, projectID int, f *TeamTopicFilter) ([]*ent.TeamTopic, error) {
	q := c.topicsQuery(projectID, f)

	// 契约 §2.2 的排序：is_pinned DESC, last_reply_at DESC NULLS LAST, created_at DESC。
	// SQLite 与 MySQL 都没有可移植的 NULLS LAST 写法，用 COALESCE 等价实现
	// （无回复的话题回落到创建时间参与比较）。
	q = q.Order(func(s *sql.Selector) {
		s.OrderExprFunc(func(b *sql.Builder) {
			b.WriteString("is_pinned DESC, COALESCE(last_reply_at, created_at) DESC")
		})
	})

	if f != nil {
		if f.Limit > 0 {
			q = q.Limit(f.Limit)
		}
		if f.Offset > 0 {
			q = q.Offset(f.Offset)
		}
	}
	return q.All(ctx)
}

func (c *teamClient) CountTopics(ctx context.Context, projectID int, f *TeamTopicFilter) (int, error) {
	return c.topicsQuery(projectID, f).Count(ctx)
}

func (c *teamClient) UpdateTopic(ctx context.Context, id int, set func(*ent.TeamTopicUpdateOne)) error {
	up := c.client.TeamTopic.UpdateOneID(id)
	set(up)
	return up.Exec(ctx)
}

// DeleteTopicCascade 删除话题及其全部楼层与附件（契约 §2.2：级联删除）
func (c *teamClient) DeleteTopicCascade(ctx context.Context, id int) error {
	if _, err := c.client.TeamPost.Delete().
		Where(teampost.TopicID(id)).Exec(ctx); err != nil {
		return err
	}
	// 附件与楼层一样必须级联清理，否则会留下永远无法访问的孤儿行
	// （附件接口都要求先定位到话题，话题没了这些行就再也点不到）
	if _, err := c.client.TeamTopicAttachment.Delete().
		Where(teamtopicattachment.TopicID(id)).Exec(ctx); err != nil {
		return err
	}
	return c.client.TeamTopic.DeleteOneID(id).Exec(ctx)
}

// IncreaseTopicView 浏览计数 +1。
// 用 SQL 层的自增（而非「读出来再写回去」）保证并发下不丢计数。
func (c *teamClient) IncreaseTopicView(ctx context.Context, id int) error {
	return c.client.TeamTopic.UpdateOneID(id).
		AddViewTotal(1).
		Exec(ctx)
}

// IncreaseTopicReply 回复后原子更新话题的回复计数与最后回复信息
func (c *teamClient) IncreaseTopicReply(ctx context.Context, id int, userID int, at time.Time) error {
	return c.client.TeamTopic.UpdateOneID(id).
		AddReplyTotal(1).
		SetLastReplyAt(at).
		SetLastReplyUserID(userID).
		Exec(ctx)
}

// DecreaseTopicReply 楼层被删除时回退回复计数。
// 下限保护写在 WHERE 里，避免重复删除把计数减成负数。
func (c *teamClient) DecreaseTopicReply(ctx context.Context, id int) error {
	return c.client.TeamTopic.Update().
		Where(teamtopic.ID(id), teamtopic.ReplyTotalGT(0)).
		AddReplyTotal(-1).
		Exec(ctx)
}

// ClearTopicLastReply 最后一个回复被删除时清空最后回复信息
func (c *teamClient) ClearTopicLastReply(ctx context.Context, id int) error {
	return c.client.TeamTopic.UpdateOneID(id).
		ClearLastReplyAt().
		SetLastReplyUserID(0).
		Exec(ctx)
}

// ============================ 讨论区：楼层 ============================

func (c *teamClient) CreatePost(ctx context.Context, args *NewTeamPostArgs) (*ent.TeamPost, error) {
	b := c.client.TeamPost.Create().
		SetTopicID(args.TopicID).
		SetUserID(args.UserID).
		SetContent(args.Content).
		SetFloor(args.Floor)
	if args.ParentID != nil {
		b = b.SetParentID(*args.ParentID)
	}
	return b.Save(ctx)
}

func (c *teamClient) GetPostByID(ctx context.Context, id int) (*ent.TeamPost, error) {
	return c.client.TeamPost.Query().
		Where(teampost.ID(id)).
		First(ctx)
}

func (c *teamClient) ListPosts(ctx context.Context, topicID int, offset, limit int) ([]*ent.TeamPost, error) {
	q := c.client.TeamPost.Query().
		Where(teampost.TopicID(topicID)).
		Order(ent.Asc(teampost.FieldFloor), ent.Asc(teampost.FieldID))
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	return q.All(ctx)
}

func (c *teamClient) CountPosts(ctx context.Context, topicID int) (int, error) {
	return c.client.TeamPost.Query().
		Where(teampost.TopicID(topicID)).
		Count(ctx)
}

// postFloors 取话题下所有楼层号（讨论区单话题的回复量级很小，直接取回内存比较）
func (c *teamClient) postFloors(ctx context.Context, topicID int) ([]int, error) {
	return c.client.TeamPost.Query().
		Where(teampost.TopicID(topicID)).
		Select(teampost.FieldFloor).
		Ints(ctx)
}

// NextPostFloor 下一个楼层号：首帖占 1 楼，因此第一条回复为 2 楼。
// 用「当前最大楼层 + 1」而不是 reply_total + 2，避免楼层被删除后号段重复。
func (c *teamClient) NextPostFloor(ctx context.Context, topicID int) (int, error) {
	floors, err := c.postFloors(ctx, topicID)
	if err != nil {
		return 0, err
	}
	last := 1
	for _, f := range floors {
		if f > last {
			last = f
		}
	}
	return last + 1, nil
}

// LatestPost 楼层号最大的回复（用于删除回复后回填「最后回复」信息）
func (c *teamClient) LatestPost(ctx context.Context, topicID int) (*ent.TeamPost, error) {
	post, err := c.client.TeamPost.Query().
		Where(teampost.TopicID(topicID)).
		Order(ent.Desc(teampost.FieldFloor), ent.Desc(teampost.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return post, nil
}

func (c *teamClient) DeletePost(ctx context.Context, id int) error {
	return c.client.TeamPost.DeleteOneID(id).Exec(ctx)
}

// ============================ 管理端统计 ============================
//
// 这些方法服务于管理面板的「团队概览」，刻意不做**可见性**过滤（列出全站数据，
// 即包含当前管理员不是成员的项目）。
//
// ⚠️ 但**软删除过滤必须一致**，口径统一为「只统计存活项目下的数据」：
//
//   - ProjectTotal  : CountProjects → TeamProject 查询（拦截器自动排除软删项目）
//   - MemberTotal   : CountAllMembers → 显式排除所属项目已软删的成员（membersQuery）
//   - TaskTotal     : CountTasksByStatus → 经 task→project 边排除软删项目
//   - TopicTotal    : CountAllTopics → 存活项目 ID 筛选（TeamTopic 无 project 边）
//   - PostTotal     : CountAllPosts → 再经 topic 收敛（TeamPost 无 project 边）
//
// 统一口径的理由：概览页是给人看的，若「项目 1 个、任务 7 个、成员 3 个」这类
// 数字彼此矛盾，会让人怀疑功能坏了。改动此处时请同步检查这五个计数是否同口径。
//
// 注意：**可见性过滤**与**软删除过滤**是两件事。前者是权限问题（管理端要看不全），
// 后者是数据有效性问题（删了就不该再出现），两者都不冲突，可同时成立。

func (c *teamClient) projectsQuery(keyword string) *ent.TeamProjectQuery {
	q := c.client.TeamProject.Query()
	if keyword != "" {
		q = q.Where(teamproject.Or(
			teamproject.NameContainsFold(keyword),
			teamproject.DescriptionContainsFold(keyword),
		))
	}
	return q
}

func (c *teamClient) ListAllProjects(ctx context.Context, keyword string, offset, limit int) ([]*ent.TeamProject, error) {
	q := c.projectsQuery(keyword).Order(ent.Desc(teamproject.FieldUpdatedAt))
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	return q.All(ctx)
}

func (c *teamClient) CountProjects(ctx context.Context, keyword string) (int, error) {
	return c.projectsQuery(keyword).Count(ctx)
}

// membersQuery 成员总表的过滤条件：keyword 同时匹配「项目名」与「用户昵称/邮箱」。
//
// 用户昵称/邮箱在 Cloudreve 的 users 表里，团队模块与它没有 ent 边
// （见项目基线的解耦决策），因此这里先按昵称/邮箱查出用户 ID 再回填条件。
//
// ⚠️ 软删除口径（本函数是 ListAllMembers 与 CountAllMembers 的唯一条件来源，
// 两者必须共用，否则分页会出现「列表有 N 行、total 却是另一个数」的错乱）：
//
//  1. 成员记录自身已软删除 —— 由 CommonMixin 的拦截器自动排除
//     （见 ent/schema/common.go：默认查询都会附加 deleted_at IS NULL），
//     这里无需显式处理。
//  2. **所属项目已软删除 —— 拦截器管不到**，因为它只过滤当前实体的 deleted_at，
//     不会跟随 edges 去看关联项目。必须在这里显式排除。
//
// 第 2 条曾经缺失，导致管理端成员总表出现「项目名为空的行」：
// 项目已软删除 → 列表仍返回其成员 → 但服务层用 ListAllProjects（已被拦截器过滤）
// 去拼项目名 → 查不到 → 项目名留空。
func (c *teamClient) membersQuery(ctx context.Context, keyword string) (*ent.TeamMemberQuery, error) {
	// 排除「所属项目已软删除」的成员。HasProjectWith(teamproject.DeletedAtIsNil())
	// 会在连表时附带 projects.deleted_at IS NULL 条件。
	q := c.client.TeamMember.Query().
		Where(teammember.HasProjectWith(teamproject.DeletedAtIsNil()))
	if keyword == "" {
		return q, nil
	}

	userIDs, err := c.client.User.Query().
		Where(user.Or(
			user.NickContainsFold(keyword),
			user.EmailContainsFold(keyword),
		)).
		Select(user.FieldID).
		Ints(ctx)
	if err != nil {
		return nil, err
	}

	if len(userIDs) > 0 {
		return q.Where(teammember.Or(
			teammember.HasProjectWith(teamproject.NameContainsFold(keyword)),
			teammember.UserIDIn(userIDs...),
		)), nil
	}
	return q.Where(teammember.HasProjectWith(teamproject.NameContainsFold(keyword))), nil
}

func (c *teamClient) ListAllMembers(ctx context.Context, keyword string, offset, limit int) ([]*ent.TeamMember, error) {
	q, err := c.membersQuery(ctx, keyword)
	if err != nil {
		return nil, err
	}
	q = q.Order(ent.Asc(teammember.FieldProjectID), ent.Asc(teammember.FieldID))
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	return q.All(ctx)
}

func (c *teamClient) CountAllMembers(ctx context.Context, keyword string) (int, error) {
	q, err := c.membersQuery(ctx, keyword)
	if err != nil {
		return 0, err
	}
	return q.Count(ctx)
}

// CountTasksByStatus 全站任务按状态计数（管理端概览用）
//
// ⚠️ 口径：只统计「所属项目仍存活」的任务，与 CountProjects 的口径保持一致。
// 原实现直接聚合 team_tasks，会把已软删除项目下的任务也算进来，
// 导致概览页出现「项目 1 个，任务却有 7 个」这类自相矛盾的数字。
func (c *teamClient) CountTasksByStatus(ctx context.Context) (map[string]int, error) {
	type row struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	var rows []row
	err := c.client.TeamTask.Query().
		Where(teamtask.HasProjectWith(teamproject.DeletedAtIsNil())).
		GroupBy(teamtask.FieldStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}

	res := make(map[string]int, len(rows))
	for _, r := range rows {
		res[r.Status] = r.Count
	}
	return res, nil
}

// CountAllTopics 全站话题计数（管理端概览用）。
//
// ⚠️ 口径：只统计「所属项目仍存活」的话题，理由同 CountTasksByStatus。
// 注意 TeamTopic **没有到 TeamProject 的 ent 边**（schema 只保留 project_id 整型字段，
// 见项目基线的解耦决策），所以这里不能用 HasProjectWith，改为先取存活项目 ID 再筛。
func (c *teamClient) CountAllTopics(ctx context.Context) (int, error) {
	liveProjectIDs, err := c.liveProjectIDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(liveProjectIDs) == 0 {
		return 0, nil
	}
	return c.client.TeamTopic.Query().
		Where(teamtopic.ProjectIDIn(liveProjectIDs...)).
		Count(ctx)
}

// CountAllPosts 全站楼层（回复）计数（管理端概览用）。
//
// ⚠️ 口径：只统计「所属话题的所属项目仍存活」的楼层。
// 楼层有到 topic 的边但没有到 project 的边，因此同样走存活项目 ID 筛选。
func (c *teamClient) CountAllPosts(ctx context.Context) (int, error) {
	liveProjectIDs, err := c.liveProjectIDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(liveProjectIDs) == 0 {
		return 0, nil
	}

	// 先取存活项目下的话题 ID，再统计这些话题的楼层。
	topicIDs, err := c.client.TeamTopic.Query().
		Where(teamtopic.ProjectIDIn(liveProjectIDs...)).
		Select(teamtopic.FieldID).
		Ints(ctx)
	if err != nil {
		return 0, err
	}
	if len(topicIDs) == 0 {
		return 0, nil
	}
	return c.client.TeamPost.Query().
		Where(teampost.TopicIDIn(topicIDs...)).
		Count(ctx)
}

// liveProjectIDs 返回所有未软删除的项目 ID。
//
// 供「没有到 TeamProject 的 ent 边、但需要按项目存活与否过滤」的统计使用
// （TeamTopic / TeamPost 属于这种情况）。
func (c *teamClient) liveProjectIDs(ctx context.Context) ([]int, error) {
	return c.client.TeamProject.Query().
		Select(teamproject.FieldID).
		Ints(ctx)
}

// ============================ 工具 ============================

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
