package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamPost 讨论区楼层（话题下的回复）。
//
// 首帖即话题正文（floor = 1），因此楼层从 2 开始编号。
// user_id 指向 Cloudreve 的 User（不加边）。
type TeamPost struct {
	ent.Schema
}

// Fields of the TeamPost.
func (TeamPost) Fields() []ent.Field {
	return []ent.Field{
		field.Int("topic_id"),
		field.Int("user_id"),
		field.Text("content"),
		// 楼层号：首帖为 1，回复从 2 开始
		field.Int("floor").Default(2),
		// 楼中楼引用的楼层 id
		field.Int("parent_id").Optional().Nillable(),
	}
}

// Edges of the TeamPost.
func (TeamPost) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("topic", TeamTopic.Type).
			Ref("posts").
			Field("topic_id").
			Required().
			Unique(),
	}
}

// Indexes of the TeamPost.
func (TeamPost) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("topic_id"),
		index.Fields("user_id"),
	}
}

func (TeamPost) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
