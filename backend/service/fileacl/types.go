package fileacl

import (
	"time"
)

// ============================================================
// 文件 ACL 响应类型
//
// 字段名严格对齐《契约补遗二-文件ACL.md》§4，不得改名。
// ============================================================

// SubjectBrief ACL 主体（用户或用户组）。
// 用户时 id 是用户 hashid、nickname 是昵称；用户组时 id 是组的 hashid、nickname 是组名。
type SubjectBrief struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
}

// AclEntry 直接设在某个文件上的一条 ACL 记录（契约 §4 AclEntry）
type AclEntry struct {
	ID          int           `json:"id"`
	FileID      int           `json:"file_id"`
	SubjectType string        `json:"subject_type"`
	SubjectID   int           `json:"subject_id"`
	Subject     *SubjectBrief `json:"subject,omitempty"`
	Permission  string        `json:"permission"`
	Inherit     bool          `json:"inherit"`
	CreatedBy   int           `json:"created_by"`
	CreatedAt   time.Time     `json:"created_at"`
}

// EffectiveAcl 对当前请求者实际生效的权限（可能来自祖先目录，契约 §4 EffectiveAcl）
type EffectiveAcl struct {
	Permission string `json:"permission"`
	// 该权限来自哪个文件
	SourceFileID int `json:"source_file_id"`
	// 来自哪个文件夹；直接命中时为 null
	InheritedFrom *string `json:"inherited_from"`
	// 是否直接设在目标文件上
	Direct bool `json:"direct"`
}

// FileAclResponse GET/PUT 的统一响应：直接记录 + 当前请求者的生效权限
type FileAclResponse struct {
	Self      []*AclEntry     `json:"self"`
	Effective []*EffectiveAcl `json:"effective"`
}
