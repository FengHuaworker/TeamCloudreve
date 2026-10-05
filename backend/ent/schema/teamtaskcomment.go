package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamTaskComment 任务下的讨论/评论。user_id 指向 Cloudreve 的 User（不加边）。
type TeamTaskComment struct {
	ent.Schema
}

// Fields of the TeamTaskComment.
func (TeamTaskComment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("task_id"),
		field.Int("user_id"),
		field.Text("content"),
	}
}

// Edges of the TeamTaskComment.
func (TeamTaskComment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", TeamTask.Type).
			Ref("comments").
			Field("task_id").
			Required().
			Unique(),
	}
}

// Indexes of the TeamTaskComment.
func (TeamTaskComment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id"),
		index.Fields("user_id"),
	}
}

func (TeamTaskComment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
