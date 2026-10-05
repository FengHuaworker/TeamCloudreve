package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// FilePermission 文件访问控制列表（ACL）条目。
//
// 设计要点（见《契约补遗二-文件ACL.md》§2）：
//   - 逻辑上属于 Cloudreve 文件系统，但迁移沿用团队模块的版本标记机制
//     （TeamSchemaVersionKey：db_version_team_v4 → v5）；
//   - file_id / subject_id / created_by 一律是内部整型 ID，不加 ent 边，
//     与既有 schema 解耦（与 TeamTopic 的处理方式一致）；
//   - 唯一索引 (file_id, subject_type, subject_id)：同一个文件上
//     同一个主体只有一条记录，PUT 即「覆盖写」。
//
// 继承采用「查询时向上回溯」，本表不做任何写时扇出：
// 一条记录只描述「它自己所在的那个文件」。
type FilePermission struct {
	ent.Schema
}

// Fields of the FilePermission.
func (FilePermission) Fields() []ent.Field {
	return []ent.Field{
		// Cloudreve 文件/文件夹的内部 ID（ent.File.ID）
		field.Int("file_id"),
		// "user" 或 "group"
		field.String("subject_type").MaxLen(16),
		// 用户 ID 或用户组 ID
		field.Int("subject_id"),
		// "read" 或 "write"，与分享权限枚举对齐
		field.String("permission").MaxLen(16),
		// 是否向子项继承。默认 true。
		// false 表示「只对自身生效，且阻断更上层 ACL 向下传递」——即继承边界。
		field.Bool("inherit").Default(true),
		// 设置者（Cloudreve 用户 ID）
		field.Int("created_by"),
	}
}

// Indexes of the FilePermission.
func (FilePermission) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("file_id", "subject_type", "subject_id").Unique(),
		index.Fields("file_id"),
	}
}

func (FilePermission) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
