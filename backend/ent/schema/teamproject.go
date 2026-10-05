package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamProject 团队协作项目（看板的所属空间）。
// 注意：owner_id 指向 Cloudreve 的 User，但此处刻意不建立 ent 边，
// 以保持团队模块与 Cloudreve 核心 schema 解耦，便于后续升级。
type TeamProject struct {
	ent.Schema
}

// Fields of the TeamProject.
func (TeamProject) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(255),
		field.Text("description").Optional().Default(""),
		field.Int("owner_id"),
		field.String("status").Default("active"), // active / archived
		field.JSON("settings", map[string]any{}).Optional(),
	}
}

// Edges of the TeamProject.
func (TeamProject) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("tasks", TeamTask.Type),
		edge.To("members", TeamMember.Type),
		edge.To("activities", TeamActivity.Type),
		edge.To("docs", TeamDoc.Type),
	}
}

// Indexes of the TeamProject.
func (TeamProject) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_id"),
		index.Fields("status"),
	}
}

func (TeamProject) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
