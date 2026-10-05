package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamActivity 项目动态（谁在什么时候做了什么），用于协作留痕与工作台展示。
// user_id 指向 Cloudreve 的 User（不加边）。
type TeamActivity struct {
	ent.Schema
}

// Fields of the TeamActivity.
func (TeamActivity) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id"),
		field.Int("task_id").Optional().Nillable(),
		field.Int("user_id"),
		// project.created / task.created / task.moved / task.assigned /
		// task.completed / comment.added / attachment.added / member.added ...
		field.String("action").MaxLen(64),
		field.JSON("payload", map[string]any{}).Optional(),
	}
}

// Edges of the TeamActivity.
func (TeamActivity) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", TeamProject.Type).
			Ref("activities").
			Field("project_id").
			Required().
			Unique(),
	}
}

// Indexes of the TeamActivity.
func (TeamActivity) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id"),
		index.Fields("task_id"),
		index.Fields("user_id"),
	}
}

func (TeamActivity) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
