package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamTaskAttachment 任务附件。
//
// 关键设计：file_id 指向 Cloudreve 的 File，本表【只存引用，不复制文件】，
// 因此天然复用 Cloudreve 的存储、权限与去重（Entity 引用计数）机制。
// name / size 为写入时的快照，用于文件被删除后仍能正确显示历史。
type TeamTaskAttachment struct {
	ent.Schema
}

// Fields of the TeamTaskAttachment.
func (TeamTaskAttachment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("task_id"),
		field.Int("file_id"),
		field.Int("user_id"),
		field.String("name").MaxLen(512),
		field.Int64("size").Default(0),
	}
}

// Edges of the TeamTaskAttachment.
func (TeamTaskAttachment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("task", TeamTask.Type).
			Ref("attachments").
			Field("task_id").
			Required().
			Unique(),
	}
}

// Indexes of the TeamTaskAttachment.
func (TeamTaskAttachment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id"),
		index.Fields("file_id"),
	}
}

func (TeamTaskAttachment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
