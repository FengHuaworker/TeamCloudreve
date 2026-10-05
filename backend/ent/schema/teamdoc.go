package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamDoc 项目文档（Wiki 页面）。
//
// 设计为树状结构（parent_id 自引用），支持无限层级，
// 与 Notion / 飞书的页面树一致。content 为 Markdown 文本，
// 前端用 Cloudreve 已内置的 MDXEditor 渲染与编辑。
//
// creator_id / last_editor_id 指向 Cloudreve 的 User（不加边）。
type TeamDoc struct {
	ent.Schema
}

// Fields of the TeamDoc.
func (TeamDoc) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id"),
		field.Int("parent_id").Optional().Nillable(),
		field.String("title").MaxLen(512),
		field.Text("content").Optional().Default(""),
		field.Int("creator_id"),
		field.Int("last_editor_id"),
		field.Float("sort_order").Default(0),
		// 允许存在仅用于分组的目录节点
		field.Bool("is_folder").Default(false),
		// 图标（emoji 或图标名），Notion 风格
		field.String("icon").Optional().Default(""),
	}
}

// Edges of the TeamDoc.
func (TeamDoc) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", TeamProject.Type).
			Ref("docs").
			Field("project_id").
			Required().
			Unique(),
	}
}

// Indexes of the TeamDoc.
func (TeamDoc) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "parent_id", "sort_order"),
		index.Fields("creator_id"),
	}
}

func (TeamDoc) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
