package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamTaskCollaborator 任务参与人（除负责人外的协作成员）。user_id 指向 Cloudreve 的 User（不加边）。
type TeamTaskCollaborator struct {
	ent.Schema
}

// Fields of the TeamTaskCollaborator.
func (TeamTaskCollaborator) Fields() []ent.Field {
	return []ent.Field{
		field.Int("task_id"),
		field.Int("user_id"),
	}
}

// Edges of the TeamTaskCollaborator.
func (TeamTaskCollaborator) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", TeamTask.Type).
			Ref("collaborators").
			Field("task_id").
			Required().
			Unique(),
	}
}

// Indexes of the TeamTaskCollaborator.
func (TeamTaskCollaborator) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id", "user_id").Unique(),
		index.Fields("user_id"),
	}
}

func (TeamTaskCollaborator) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
