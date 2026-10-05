package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamTopic 讨论区话题（一个项目下的一篇主帖，正文即首帖/1 楼）。
//
// project_id 指向 TeamProject，但刻意只保留整型字段、不建 ent 边：
// ent 的 edge.From 必须配套对端的 edge.To（Ref），而 TeamProject 属于既有 schema，
// 本次不修改它（与 owner_id / user_id 的处理方式一致，保持模块与既有 schema 解耦）。
//
// user_id / last_reply_user_id 指向 Cloudreve 的 User（同样不加边）。
type TeamTopic struct {
	ent.Schema
}

// Fields of the TeamTopic.
func (TeamTopic) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id"),
		field.String("title").MaxLen(512),
		// 首帖正文（Markdown），楼层从 2 开始
		field.Text("content").Optional().Default(""),
		// discuss / announce / question / share
		field.String("category").Default("discuss"),
		// 发帖人
		field.Int("user_id"),
		// 置顶（列表排序优先）
		field.Bool("is_pinned").Default(false),
		// 锁定后禁止回复
		field.Bool("is_locked").Default(false),
		// 已解决（question 类话题）
		field.Bool("is_resolved").Default(false),
		// 回复数（不含首帖）
		field.Int("reply_total").Default(0),
		field.Int("view_total").Default(0),
		// 最后回复时间（无回复时为空，排序时回落到 created_at）
		field.Time("last_reply_at").Optional().Nillable(),
		// 最后回复人，0 表示无回复
		field.Int("last_reply_user_id").Default(0),
	}
}

// Edges of the TeamTopic.
func (TeamTopic) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("posts", TeamPost.Type),
		// 话题附件（讨论区附件能力，与任务的附件设计同构）
		edge.To("attachments", TeamTopicAttachment.Type),
	}
}

// Indexes of the TeamTopic.
func (TeamTopic) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id"),
		index.Fields("user_id"),
		index.Fields("category"),
	}
}

func (TeamTopic) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
