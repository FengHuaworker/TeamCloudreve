package inventory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/teamtopic"
	"github.com/cloudreve/Cloudreve/v4/pkg/conf"
	_ "modernc.org/sqlite"
)

// TestTeamTopicRuntime 用内存 SQLite 跑一遍讨论区数据访问层，
// 重点验证 ORM 生成的 SQL（尤其是 COALESCE 排序）在真实数据库上可用。
func TestTeamTopicRuntime(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("sqlite3", "file:team_runtime_test?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", db)))
	defer client.Close()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("schema create: %v", err)
	}

	tc := NewTeamClient(client, conf.SQLiteDB, nil)

	tcc := &teamClient{client: client, maxSQlParam: sqlParamLimit(conf.SQLiteDB)}
	_ = tc
	tc = tcc

	project, err := tc.CreateProject(ctx, &NewTeamProjectArgs{Name: "内网协作平台", Description: "test", OwnerID: 1})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	now := time.Now()
	// 置顶的话题：创建最早，但没有回复
	pinned, err := tcc.client.TeamTopic.Create().
		SetProjectID(project.ID).
		SetTitle("置顶公告").
		SetContent("欢迎").
		SetCategory(TeamTopicCategoryAnnounce).
		SetUserID(1).
		SetIsPinned(true).
		SetCreatedAt(now.Add(-5 * time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}

	// 普通话题：创建很早，但刚刚有回复（用于验证 COALESCE(last_reply_at, created_at)）
	replied, err := tc.CreateTopic(ctx, &NewTeamTopicArgs{
		ProjectID: project.ID, Title: "数据库选型讨论", Content: "PostgreSQL 还是 MySQL", UserID: 2,
	})
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	// 另一个话题：创建较新，但没有回复
	fresh, err := tcc.client.TeamTopic.Create().
		SetProjectID(project.ID).
		SetTitle("周会纪要").
		SetContent("本周进展").
		SetCategory(TeamTopicCategoryDiscuss).
		SetUserID(1).
		SetCreatedAt(now.Add(-3 * time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}

	if err := tc.UpdateTopic(ctx, replied.ID, func(up *ent.TeamTopicUpdateOne) {
		up.SetLastReplyAt(now.Add(-1 * time.Hour)).
			SetLastReplyUserID(3)
	}); err != nil {
		t.Fatalf("touch replied topic: %v", err)
	}

	list, err := tc.ListTopics(ctx, project.ID, nil)
	if err != nil {
		t.Fatalf("list topics (COALESCE order): %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expect 3 topics, got %d", len(list))
	}
	want := []int{pinned.ID, replied.ID, fresh.ID}
	for i, id := range want {
		if list[i].ID != id {
			t.Fatalf("order mismatch at %d: got id=%d want id=%d (order=%v)", i, list[i].ID, id,
				[]int{list[0].ID, list[1].ID, list[2].ID})
		}
	}
	t.Logf("order OK: pinned=%d replied=%d fresh=%d", list[0].ID, list[1].ID, list[2].ID)

	// keyword 命中标题与正文
	n, err := tc.CountTopics(ctx, project.ID, &TeamTopicFilter{Keyword: "选型"})
	if err != nil || n != 1 {
		t.Fatalf("keyword title search: n=%d err=%v", n, err)
	}
	n, err = tc.CountTopics(ctx, project.ID, &TeamTopicFilter{Keyword: "PostgreSQL"})
	if err != nil || n != 1 {
		t.Fatalf("keyword content search: n=%d err=%v", n, err)
	}

	// 分页 + 分类过滤
	page, err := tc.ListTopics(ctx, project.ID, &TeamTopicFilter{Category: TeamTopicCategoryAnnounce, Limit: 10})
	if err != nil || len(page) != 1 {
		t.Fatalf("category filter: len=%d err=%v", len(page), err)
	}
	second, err := tc.ListTopics(ctx, project.ID, &TeamTopicFilter{Offset: 1, Limit: 1})
	if err != nil || len(second) != 1 || second[0].ID != replied.ID {
		t.Fatalf("paging: %+v err=%v", second, err)
	}

	// 楼层：首帖为 1 楼，回复从 2 开始
	floor, err := tc.NextPostFloor(ctx, fresh.ID)
	if err != nil || floor != 2 {
		t.Fatalf("first reply floor: %d err=%v", floor, err)
	}
	post, err := tc.CreatePost(ctx, &NewTeamPostArgs{TopicID: fresh.ID, UserID: 2, Content: "收到", Floor: floor})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := tc.IncreaseTopicReply(ctx, fresh.ID, 2, now); err != nil {
		t.Fatalf("increase reply: %v", err)
	}
	if err := tc.IncreaseTopicView(ctx, fresh.ID); err != nil {
		t.Fatalf("increase view: %v", err)
	}
	got, err := tc.GetTopicByID(ctx, fresh.ID)
	if err != nil {
		t.Fatalf("get topic: %v", err)
	}
	if got.ReplyTotal != 1 || got.LastReplyUserID != 2 || got.LastReplyAt == nil || got.ViewTotal != 1 {
		t.Fatalf("topic counters wrong: total=%d last_user=%d last_at=%v view=%d",
			got.ReplyTotal, got.LastReplyUserID, got.LastReplyAt, got.ViewTotal)
	}
	floor, _ = tc.NextPostFloor(ctx, fresh.ID)
	if floor != 3 {
		t.Fatalf("second reply floor: %d", floor)
	}
	if cnt, _ := tc.CountPosts(ctx, fresh.ID); cnt != 1 {
		t.Fatalf("count posts: %d", cnt)
	}

	// 删除回复：计数回退 + 最后回复清空
	if err := tc.DeletePost(ctx, post.ID); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	if err := tc.DecreaseTopicReply(ctx, fresh.ID); err != nil {
		t.Fatalf("decrease reply: %v", err)
	}
	latest, err := tc.LatestPost(ctx, fresh.ID)
	if err != nil || latest != nil {
		t.Fatalf("latest post should be nil, got %+v err=%v", latest, err)
	}
	if err := tc.ClearTopicLastReply(ctx, fresh.ID); err != nil {
		t.Fatalf("clear last reply: %v", err)
	}
	got, _ = tc.GetTopicByID(ctx, fresh.ID)
	if got.ReplyTotal != 0 || got.LastReplyUserID != 0 || got.LastReplyAt != nil {
		t.Fatalf("counters not rolled back: %+v", got)
	}
	// 下限保护：不会减成负数
	_ = tc.DecreaseTopicReply(ctx, fresh.ID)
	if got, _ = tc.GetTopicByID(ctx, fresh.ID); got.ReplyTotal != 0 {
		t.Fatalf("reply total should stay 0, got %d", got.ReplyTotal)
	}

	// 级联删除话题 → 楼层一并删除
	if _, err := tc.CreatePost(ctx, &NewTeamPostArgs{TopicID: fresh.ID, UserID: 2, Content: "再来一层", Floor: 3}); err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := tc.DeleteTopicCascade(ctx, fresh.ID); err != nil {
		t.Fatalf("delete topic cascade: %v", err)
	}
	if cnt, _ := tc.CountPosts(ctx, fresh.ID); cnt != 0 {
		t.Fatalf("posts should be cascade deleted, got %d", cnt)
	}
	if _, err := tc.GetTopicByID(ctx, fresh.ID); !ent.IsNotFound(err) {
		t.Fatalf("topic should be gone, err=%v", err)
	}

	// 管理端查询：项目 keyword、成员 keyword（项目名命中）、任务状态聚合
	if _, err := tc.CreateTask(ctx, &NewTeamTaskArgs{
		ProjectID: project.ID, Title: "接入 CI", CreatorID: 1, Status: TeamTaskStatusDone,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := tc.UpsertMember(ctx, project.ID, 7, TeamRoleMember); err != nil {
		t.Fatalf("upsert member: %v", err)
	}
	if n, err := tc.CountProjects(ctx, "协作"); err != nil || n != 1 {
		t.Fatalf("count projects by keyword: n=%d err=%v", n, err)
	}
	if members, err := tc.ListAllMembers(ctx, "内网", 0, 20); err != nil || len(members) != 1 {
		t.Fatalf("list members by project keyword: len=%d err=%v", len(members), err)
	}
	if members, err := tc.ListAllMembers(ctx, "不存在的项目", 0, 20); err != nil || len(members) != 0 {
		t.Fatalf("list members unmatched: len=%d err=%v", len(members), err)
	}
	byStatus, err := tc.CountTasksByStatus(ctx)
	if err != nil || byStatus[TeamTaskStatusDone] != 1 {
		t.Fatalf("count tasks by status: %+v err=%v", byStatus, err)
	}
	if n, err := tc.CountAllTopics(ctx); err != nil || n != 2 {
		t.Fatalf("count all topics: n=%d err=%v", n, err)
	}

	// 项目级联删除应连带清掉话题与楼层
	if _, err := tc.CreatePost(ctx, &NewTeamPostArgs{TopicID: replied.ID, UserID: 2, Content: "x", Floor: 2}); err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := tc.DeleteProjectCascade(ctx, project.ID); err != nil {
		t.Fatalf("delete project cascade: %v", err)
	}
	if topics, _ := tcc.client.TeamTopic.Query().Where(teamtopic.ProjectID(project.ID)).Count(ctx); topics != 0 {
		t.Fatalf("topics should be cascade deleted, got %d", topics)
	}
	if posts, _ := tc.CountAllPosts(ctx); posts != 0 {
		t.Fatalf("posts should be cascade deleted, got %d", posts)
	}
}

// TestTeamMemberSoftDeletedProjectFilter 回归测试：管理端成员总表不得出现
// 「所属项目已软删除」的成员。
//
// 背景（真实缺陷，已在 instance2 上复现）：管理面板 → 团队管理 → 成员 tab
// 出现了项目名为空的行。根因是 ListAllMembers/CountAllMembers 只依赖
// CommonMixin 拦截器排除「成员自身已软删除」，但拦截器不会跟随 edge 去看
// 关联项目是否已删除；而服务层用 ListAllProjects（已被拦截器过滤）拼项目名，
// 于是软删项目的成员查不到项目名 → 显示为空行。
//
// 本例刻意用 DeleteProject **单表软删除**（而非 DeleteProjectCascade）来构造
// 那种「项目软删了、成员还在」的脏数据 —— 这正是线上数据的实际状态。
func TestTeamMemberSoftDeletedProjectFilter(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("sqlite3", "file:team_softdelete_test?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", db)))
	defer client.Close()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("schema create: %v", err)
	}

	tc := &teamClient{client: client, maxSQlParam: sqlParamLimit(conf.SQLiteDB)}

	// 项目 1、2 均属于同一 owner；项目 1 随后被软删除。
	dead, err := tc.CreateProject(ctx, &NewTeamProjectArgs{Name: "被删项目", OwnerID: 2})
	if err != nil {
		t.Fatalf("create dead project: %v", err)
	}
	live, err := tc.CreateProject(ctx, &NewTeamProjectArgs{Name: "存活项目", OwnerID: 2})
	if err != nil {
		t.Fatalf("create live project: %v", err)
	}

	for _, pair := range []struct {
		pid  int
		uid  int
		role string
	}{
		{dead.ID, 2, TeamRoleOwner},
		{live.ID, 2, TeamRoleOwner},
		{live.ID, 3, TeamRoleMember},
	} {
		if _, err := tc.UpsertMember(ctx, pair.pid, pair.uid, pair.role); err != nil {
			t.Fatalf("upsert member: %v", err)
		}
	}

	// 软删除项目 1（单表删除 → CommonMixin 钩子转成 SetDeletedAt）。
	// 注意此处**不**清理成员，复现线上「孤儿成员」状态。
	if err := tc.DeleteProject(ctx, dead.ID); err != nil {
		t.Fatalf("soft delete project: %v", err)
	}

	// 项目计数：应只剩 1 个（拦截器已正确过滤）
	if n, err := tc.CountProjects(ctx, ""); err != nil || n != 1 {
		t.Fatalf("CountProjects: n=%d err=%v", n, err)
	}

	// 成员列表：不得包含软删项目下的成员（修复前此处为 3）
	members, err := tc.ListAllMembers(ctx, "", 0, 20)
	if err != nil {
		t.Fatalf("ListAllMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("ListAllMembers should return 2 (live project only), got %d", len(members))
	}
	for _, m := range members {
		if m.ProjectID == dead.ID {
			t.Fatalf("member of soft-deleted project leaked: project=%d user=%d", m.ProjectID, m.UserID)
		}
	}

	// 计数必须与列表一致，否则分页错乱
	n, err := tc.CountAllMembers(ctx, "")
	if err != nil {
		t.Fatalf("CountAllMembers: %v", err)
	}
	if n != len(members) {
		t.Fatalf("count(%d) != list(%d): pagination would break", n, len(members))
	}

	// 带 keyword 时口径也必须一致（keyword 走另一条分支）
	if n, err := tc.CountAllMembers(ctx, "项目"); err != nil || n != 2 {
		t.Fatalf("CountAllMembers(keyword): n=%d err=%v", n, err)
	}

	// 概览统计：成员/话题/楼层都只应统计存活项目
	deadTopic, err := tc.CreateTopic(ctx, &NewTeamTopicArgs{
		ProjectID: dead.ID, Title: "被删项目的话题", Content: "x", UserID: 2,
	})
	if err != nil {
		t.Fatalf("create dead topic: %v", err)
	}
	if _, err := tc.CreatePost(ctx, &NewTeamPostArgs{
		TopicID: deadTopic.ID, UserID: 2, Content: "reply", Floor: 2,
	}); err != nil {
		t.Fatalf("create dead post: %v", err)
	}
	if _, err := tc.CreateTask(ctx, &NewTeamTaskArgs{
		ProjectID: dead.ID, Title: "被删项目的任务", CreatorID: 2, Status: TeamTaskStatusDone,
	}); err != nil {
		t.Fatalf("create dead task: %v", err)
	}

	if n, err := tc.CountAllTopics(ctx); err != nil || n != 0 {
		t.Fatalf("CountAllTopics should be 0 (live projects only), n=%d err=%v", n, err)
	}
	if n, err := tc.CountAllPosts(ctx); err != nil || n != 0 {
		t.Fatalf("CountAllPosts should be 0 (live projects only), n=%d err=%v", n, err)
	}
	byStatus, err := tc.CountTasksByStatus(ctx)
	if err != nil {
		t.Fatalf("CountTasksByStatus: %v", err)
	}
	if byStatus[TeamTaskStatusDone] != 0 {
		t.Fatalf("CountTasksByStatus should exclude soft-deleted project, got %+v", byStatus)
	}
}

// TestTeamTaskAssigneePersistence 锁定「任务指派」的数据层语义。
//
// 背景：线上曾出现「任务指派从未生效」——请求侧 assignee_id 是普通整型，
// 而响应侧 assignee.id 是 hashid 字符串，前端 Number("pBiM") → NaN →
// JSON.stringify 后变成 null → 后端视作「不指派」。契约 §6.1 已把请求侧
// 改为 hashid，由 service 层的 decodeAssignee 解码成整型后传给本层。
//
// 本测试固定数据层这一侧的约定：AssigneeID 为 nil = 不指派；
// 非 nil = 落库；UpdateTask 里 ClearAssigneeID = 取消指派。
func TestTeamTaskAssigneePersistence(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("sqlite3", "file:team_assignee_test?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", db)))
	defer client.Close()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("schema create: %v", err)
	}

	tc := &teamClient{client: client, maxSQlParam: sqlParamLimit(conf.SQLiteDB)}

	project, err := tc.CreateProject(ctx, &NewTeamProjectArgs{Name: "指派测试", OwnerID: 1})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// 1) 不指派：AssigneeID 为 nil → 落库后应为 NULL
	unassigned, err := tc.CreateTask(ctx, &NewTeamTaskArgs{
		ProjectID: project.ID, Title: "待认领", CreatorID: 1,
	})
	if err != nil {
		t.Fatalf("create unassigned task: %v", err)
	}
	if unassigned.AssigneeID != nil {
		t.Fatalf("expected nil assignee, got %v", *unassigned.AssigneeID)
	}

	// 2) 指派：AssigneeID 非 nil → 应落库
	uid := 42
	assigned, err := tc.CreateTask(ctx, &NewTeamTaskArgs{
		ProjectID: project.ID, Title: "已指派", CreatorID: 1, AssigneeID: &uid,
	})
	if err != nil {
		t.Fatalf("create assigned task: %v", err)
	}
	if assigned.AssigneeID == nil || *assigned.AssigneeID != uid {
		t.Fatalf("expected assignee %d, got %v", uid, assigned.AssigneeID)
	}

	// 3) 取消指派：ClearAssigneeID → 应回到 NULL
	if err := tc.UpdateTask(ctx, assigned.ID, func(up *ent.TeamTaskUpdateOne) {
		up.ClearAssigneeID()
	}); err != nil {
		t.Fatalf("clear assignee: %v", err)
	}
	reloaded, err := tc.GetTaskByID(ctx, assigned.ID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if reloaded.AssigneeID != nil {
		t.Fatalf("expected cleared assignee, got %v", *reloaded.AssigneeID)
	}

	// 4) 指派筛选（工作台「我负责的任务」依赖此路径）
	uid2 := 43
	if _, err := tc.CreateTask(ctx, &NewTeamTaskArgs{
		ProjectID: project.ID, Title: "用户43的任务", CreatorID: 1, AssigneeID: &uid2,
	}); err != nil {
		t.Fatalf("create task for uid2: %v", err)
	}
	mine, err := tc.ListTasks(ctx, project.ID, &TeamTaskFilter{AssigneeID: &uid2})
	if err != nil {
		t.Fatalf("list tasks by assignee: %v", err)
	}
	if len(mine) != 1 {
		t.Fatalf("expected 1 task for uid2, got %d", len(mine))
	}
}
