package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamMember 项目成员及其角色。user_id 指向 Cloudreve 的 User（不加边）。
type TeamMember struct {
	ent.Schema
}

// Fields of the TeamMember.
func (TeamMember) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id"),
		field.Int("user_id"),
		field.String("role").Default("member"), // owner / admin / member / viewer
	}
}

// Edges of the TeamMember.
func (TeamMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", TeamProject.Type).
			Ref("members").
			Field("project_id").
			Required().
			Unique(),
	}
}

// Indexes of the TeamMember.
func (TeamMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "user_id").Unique(),
		index.Fields("user_id"),
	}
}

func (TeamMember) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
