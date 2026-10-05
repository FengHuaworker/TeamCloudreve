package inventory

import (
	"context"

	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/teamdoc"
	"github.com/cloudreve/Cloudreve/v4/ent/teamnotification"
)

// 通知类型（唯一来源，service 层统一引用这里）
const (
	NotificationMention       = "mention"        // 在评论/文档中被 @
	NotificationAssigned      = "assigned"       // 被指派为负责人
	NotificationCommented     = "commented"      // 关注的任务有新评论
	NotificationStatusChanged = "status_changed" // 任务状态变更
	NotificationDueSoon       = "due_soon"       // 任务即将到期
	NotificationMemberAdded   = "member_added"   // 被加入项目

	// 讨论区的提及通知与 Wiki 分开，便于前端区分展示与配色，
	// 也让「文档里被 @」与「讨论里被 @」将来可以有不同的跳转目标。
	NotificationTopicMention = "topic_mention" // 在讨论区话题正文中被 @
	NotificationPostMention  = "post_mention"  // 在讨论区回复中被 @
)

type (
	// NewTeamDocArgs 创建文档入参
	NewTeamDocArgs struct {
		ProjectID int
		ParentID  *int
		Title     string
		Content   string
		CreatorID int
		IsFolder  bool
		Icon      string
	}

	// NewTeamNotificationArgs 创建通知入参
	NewTeamNotificationArgs struct {
		UserID    int
		ActorID   int
		Type      string
		ProjectID *int
		TaskID    *int
		DocID     *int
		// TopicID 讨论区话题；与 TaskID / DocID 互斥
		TopicID *int
		Title   string
		Body    string
	}

	// TeamDocClient 文档（Wiki）数据访问
	TeamDocClient interface {
		TxOperator

		CreateDoc(ctx context.Context, args *NewTeamDocArgs) (*ent.TeamDoc, error)
		GetDocByID(ctx context.Context, id int) (*ent.TeamDoc, error)
		ListDocs(ctx context.Context, projectID int) ([]*ent.TeamDoc, error)
		ListDocsByParent(ctx context.Context, projectID int, parentID *int) ([]*ent.TeamDoc, error)
		UpdateDoc(ctx context.Context, id int, set func(*ent.TeamDocUpdateOne)) error
		DeleteDocCascade(ctx context.Context, projectID, id int) error
	}

	// TeamNotificationClient 通知数据访问
	TeamNotificationClient interface {
		TxOperator

		CreateNotification(ctx context.Context, args *NewTeamNotificationArgs) (*ent.TeamNotification, error)
		ListNotifications(ctx context.Context, userID int, onlyUnread bool, limit int) ([]*ent.TeamNotification, error)
		CountUnread(ctx context.Context, userID int) (int, error)
		MarkRead(ctx context.Context, userID int, ids []int) error
		MarkAllRead(ctx context.Context, userID int) error
		// ExistsNotification 用于去重（如到期提醒只发一次）
		ExistsNotification(ctx context.Context, userID int, ntype string, taskID int) (bool, error)
		// ExistsTopicMention 判断用户是否已因该话题收到过某类型的提及通知（讨论区去重）
		ExistsTopicMention(ctx context.Context, userID, topicID int, ntype string) (bool, error)
	}
)

func NewTeamDocClient(client *ent.Client) TeamDocClient {
	return &teamDocClient{client: client}
}

type teamDocClient struct{ client *ent.Client }

func (c *teamDocClient) SetClient(n *ent.Client) TxOperator { return &teamDocClient{client: n} }
func (c *teamDocClient) GetClient() *ent.Client             { return c.client }

func (c *teamDocClient) CreateDoc(ctx context.Context, args *NewTeamDocArgs) (*ent.TeamDoc, error) {
	b := c.client.TeamDoc.Create().
		SetProjectID(args.ProjectID).
		SetTitle(args.Title).
		SetContent(args.Content).
		SetCreatorID(args.CreatorID).
		SetLastEditorID(args.CreatorID).
		SetIsFolder(args.IsFolder).
		SetIcon(args.Icon)
	if args.ParentID != nil {
		b = b.SetParentID(*args.ParentID)
	}
	return b.Save(ctx)
}

func (c *teamDocClient) GetDocByID(ctx context.Context, id int) (*ent.TeamDoc, error) {
	return c.client.TeamDoc.Query().Where(teamdoc.ID(id)).First(ctx)
}

func (c *teamDocClient) ListDocs(ctx context.Context, projectID int) ([]*ent.TeamDoc, error) {
	return c.client.TeamDoc.Query().
		Where(teamdoc.ProjectID(projectID)).
		Order(ent.Asc(teamdoc.FieldSortOrder), ent.Asc(teamdoc.FieldID)).
		All(ctx)
}

func (c *teamDocClient) ListDocsByParent(ctx context.Context, projectID int, parentID *int) ([]*ent.TeamDoc, error) {
	q := c.client.TeamDoc.Query().Where(teamdoc.ProjectID(projectID))
	if parentID == nil {
		q = q.Where(teamdoc.ParentIDIsNil())
	} else {
		q = q.Where(teamdoc.ParentID(*parentID))
	}
	return q.Order(ent.Asc(teamdoc.FieldSortOrder), ent.Asc(teamdoc.FieldID)).All(ctx)
}

func (c *teamDocClient) UpdateDoc(ctx context.Context, id int, set func(*ent.TeamDocUpdateOne)) error {
	up := c.client.TeamDoc.UpdateOneID(id)
	set(up)
	return up.Exec(ctx)
}

// DeleteDocCascade 递归删除文档及其所有子文档
func (c *teamDocClient) DeleteDocCascade(ctx context.Context, projectID, id int) error {
	children, err := c.client.TeamDoc.Query().
		Where(teamdoc.ProjectID(projectID), teamdoc.ParentID(id)).
		Select(teamdoc.FieldID).
		Ints(ctx)
	if err != nil {
		return err
	}
	for _, cid := range children {
		if err := c.DeleteDocCascade(ctx, projectID, cid); err != nil {
			return err
		}
	}
	return c.client.TeamDoc.DeleteOneID(id).Exec(ctx)
}

// ============================ 通知 ============================

func NewTeamNotificationClient(client *ent.Client) TeamNotificationClient {
	return &teamNotificationClient{client: client}
}

type teamNotificationClient struct{ client *ent.Client }

func (c *teamNotificationClient) SetClient(n *ent.Client) TxOperator {
	return &teamNotificationClient{client: n}
}
func (c *teamNotificationClient) GetClient() *ent.Client { return c.client }

func (c *teamNotificationClient) CreateNotification(ctx context.Context,
	args *NewTeamNotificationArgs) (*ent.TeamNotification, error) {

	// 不给自己发通知
	if args.UserID <= 0 || args.UserID == args.ActorID {
		return nil, nil
	}

	b := c.client.TeamNotification.Create().
		SetUserID(args.UserID).
		SetType(args.Type).
		SetTitle(args.Title).
		SetBody(args.Body)
	if args.ActorID > 0 {
		b = b.SetActorID(args.ActorID)
	}
	if args.ProjectID != nil {
		b = b.SetProjectID(*args.ProjectID)
	}
	if args.TaskID != nil {
		b = b.SetTaskID(*args.TaskID)
	}
	if args.DocID != nil {
		b = b.SetDocID(*args.DocID)
	}
	if args.TopicID != nil {
		b = b.SetTopicID(*args.TopicID)
	}
	return b.Save(ctx)
}

func (c *teamNotificationClient) ListNotifications(ctx context.Context, userID int,
	onlyUnread bool, limit int) ([]*ent.TeamNotification, error) {

	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := c.client.TeamNotification.Query().Where(teamnotification.UserID(userID))
	if onlyUnread {
		q = q.Where(teamnotification.IsRead(false))
	}
	return q.Order(ent.Desc(teamnotification.FieldCreatedAt)).Limit(limit).All(ctx)
}

func (c *teamNotificationClient) CountUnread(ctx context.Context, userID int) (int, error) {
	return c.client.TeamNotification.Query().
		Where(teamnotification.UserID(userID), teamnotification.IsRead(false)).
		Count(ctx)
}

func (c *teamNotificationClient) MarkRead(ctx context.Context, userID int, ids []int) error {
	_, err := c.client.TeamNotification.Update().
		Where(teamnotification.UserID(userID), teamnotification.IDIn(ids...)).
		SetIsRead(true).
		Save(ctx)
	return err
}

func (c *teamNotificationClient) MarkAllRead(ctx context.Context, userID int) error {
	_, err := c.client.TeamNotification.Update().
		Where(teamnotification.UserID(userID), teamnotification.IsRead(false)).
		SetIsRead(true).
		Save(ctx)
	return err
}

func (c *teamNotificationClient) ExistsNotification(ctx context.Context, userID int,
	ntype string, taskID int) (bool, error) {

	return c.client.TeamNotification.Query().
		Where(
			teamnotification.UserID(userID),
			teamnotification.TypeEQ(ntype),
			teamnotification.TaskID(taskID),
		).
		Exist(ctx)
}

// ExistsTopicMention 判断某用户是否已因【该话题】收到过【该类型】的提及通知。
//
// 用途：讨论区编辑话题/回复时避免重复打扰。去重键是
// (user_id, topic_id, type)，语义是「这个话题我已经被 @ 过了」，
// 因此：
//   - 反复保存同一话题 → 不再重复通知
//   - 编辑时新 @ 了某人 → 该人此前没有记录，正常收到
//
// 注意只看 topic_id 不区分帖子：同一话题下被 @ 多次只通知一次，
// 这是刻意的（避免热帖把通知栏刷屏）。
func (c *teamNotificationClient) ExistsTopicMention(ctx context.Context,
	userID, topicID int, ntype string) (bool, error) {

	return c.client.TeamNotification.Query().
		Where(
			teamnotification.UserID(userID),
			teamnotification.TypeEQ(ntype),
			teamnotification.TopicID(topicID),
		).
		Exist(ctx)
}
