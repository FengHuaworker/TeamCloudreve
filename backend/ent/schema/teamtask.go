package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamTask 团队任务。assignee_id / creator_id 指向 Cloudreve 的 User（不加边）。
type TeamTask struct {
	ent.Schema
}

// Fields of the TeamTask.
func (TeamTask) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id"),
		// 父任务，用于子任务拆解
		field.Int("parent_id").Optional().Nillable(),
		field.String("title").MaxLen(512),
		field.Text("description").Optional().Default(""),
		// todo / doing / review / done / archived
		field.String("status").Default("todo"),
		// low / normal / high / urgent
		field.String("priority").Default("normal"),
		// 负责人（可为空 = 待认领）
		field.Int("assignee_id").Optional().Nillable(),
		field.Int("creator_id"),
		field.Time("start_at").Optional().Nillable(),
		field.Time("due_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		// 看板内排序（浮点便于插入）
		field.Float("sort_order").Default(0),
		field.JSON("tags", []string{}).Optional(),
		// 完成度 0-100
		field.Int("progress").Default(0),
	}
}

// Edges of the TeamTask.
func (TeamTask) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", TeamProject.Type).
			Ref("tasks").
			Field("project_id").
			Required().
			Unique(),
		edge.To("comments", TeamTaskComment.Type),
		edge.To("attachments", TeamTaskAttachment.Type),
		edge.To("collaborators", TeamTaskCollaborator.Type),
	}
}

// Indexes of the TeamTask.
func (TeamTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "status", "sort_order"),
		index.Fields("assignee_id"),
		index.Fields("parent_id"),
		index.Fields("due_at"),
	}
}

func (TeamTask) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
