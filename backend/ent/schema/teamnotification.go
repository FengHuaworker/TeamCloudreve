package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamNotification 站内通知。
//
// user_id 为接收者，actor_id 为触发者（均为 Cloudreve 的 User，不加边）。
// 通知不建立 ent 边，避免与核心 schema 耦合；相关对象用裸 ID 引用。
type TeamNotification struct {
	ent.Schema
}

// 通知类型的常量定义在 inventory/team_doc.go（唯一来源），此处不再重复。

// Fields of the TeamNotification.
func (TeamNotification) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.Int("actor_id").Optional().Default(0),
		field.String("type").MaxLen(32),
		field.Int("project_id").Optional().Nillable(),
		field.Int("task_id").Optional().Nillable(),
		field.Int("doc_id").Optional().Nillable(),
		// 讨论区话题：@提及 通知需要能跳回被提及的话题
		field.Int("topic_id").Optional().Nillable(),
		field.String("title").MaxLen(512),
		field.Text("body").Optional().Default(""),
		field.Bool("is_read").Default(false),
	}
}

// Indexes of the TeamNotification.
func (TeamNotification) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "is_read"),
		index.Fields("user_id", "created_at"),
		index.Fields("task_id"),
		index.Fields("doc_id"),
		index.Fields("topic_id"),
	}
}

func (TeamNotification) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
