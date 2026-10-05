package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamTopicAttachment 讨论区话题附件。
//
// 与 TeamTaskAttachment 完全同构，关键设计同样是【只存引用，不复制文件】：
// file_id 指向 Cloudreve 的 File，因此天然复用 Cloudreve 的存储、权限
// 与去重（Entity 引用计数）机制。name / size 为写入时的快照，
// 用于文件被删除后仍能正确显示历史。
//
// 为什么 file_id 是 int 而不是 hashid 字符串：
// 与 team_task_attachment 保持一致 —— 对外 API 收发 hashid，
// 对内落库为整型 ID。存字符串会丢掉与 Cloudreve Entity 表的关联。
type TeamTopicAttachment struct {
	ent.Schema
}

// Fields of the TeamTopicAttachment.
func (TeamTopicAttachment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("topic_id"),
		field.Int("user_id"),
		field.Int("file_id"),
		field.String("name").MaxLen(512),
		field.Int64("size").Default(0),
	}
}

// Edges of the TeamTopicAttachment.
//
// 附件 → 话题 的边在这里建立：两者都是团队模块自有的 schema，
// 不涉及修改既有的 TeamProject，因此不受「不改既有文件」的约束。
func (TeamTopicAttachment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("topic", TeamTopic.Type).
			Ref("attachments").
			Field("topic_id").
			Required().
			Unique(),
	}
}

// Indexes of the TeamTopicAttachment.
func (TeamTopicAttachment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("topic_id"),
		index.Fields("file_id"),
	}
}

func (TeamTopicAttachment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
