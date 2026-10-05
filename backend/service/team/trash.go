package team

import (
	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/cloudreve/Cloudreve/v4/service/teamtrash"
	"github.com/gin-gonic/gin"
)

// ===================== 项目级团队回收站（口径乙 + 权限口径甲）=====================
//
// 路由（见 routers/router.go teamGroup）：
//
//	GET   /api/v4/team/project/:id/trash          列出本项目回收站
//	PUT   /api/v4/team/project/:id/trash/restore   恢复（body: file_hash_ids）
//
// 权限口径 = 甲（Peer 2026-10-05 拍板）：
//
//	可见 = 可恢复 = 能删（项目成员 + 项目根 ACL write 或无 ACL 回落）
//
// 恢复语义与个人空间一致：原目录不存在 -> 明确报错（不静默丢到根）。

// TrashListParamCtx 回收站列表的参数 context key（:id 在路径里，
// 无请求体，故无需 FromJSON 中间件 —— 与 ListProjectService 同型）。
type TrashListParamCtx struct{}

// TrashListService 项目回收站列表。
type TrashListService struct{}

// TrashRestoreParamCtx 恢复请求的 context key。
type TrashRestoreParamCtx struct{}

// TrashRestoreService 恢复请求体。
//
// 文件是 Cloudreve 实体，按惯例用 hashid（见 pathID 注释：
// "团队模块内部实体用普通整型 ID…而 Cloudreve 的实体仍沿用 hashid"）。
type TrashRestoreService struct {
	FileHashIDs []string `json:"file_hash_ids"`
}

// trashSvc 从依赖装配 teamtrash 服务。
func trashSvc(dep dependency.Dep) *teamtrash.Service {
	return teamtrash.New(
		dep.DBClient(),
		dep.FileClient(),
		dep.FilePermissionClient(),
		dep.UserClient(),
		dep.HashIDEncoder(),
		dep.Logger(),
	)
}

// List 列出项目回收站。
func (s *TrashListService) List(c *gin.Context) (*teamtrash.ListResult, error) {
	dep := dependency.FromContext(c)
	pid := pathID(c)
	if pid <= 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "invalid project id", nil)
	}

	u := inventory.UserFromContext(c)
	if u == nil {
		return nil, serializer.NewError(serializer.CodeCheckLogin, "Login required", nil)
	}

	return trashSvc(dep).List(c, u, pid)
}

// Restore 恢复项目回收站中的文件。
func (s *TrashRestoreService) Restore(c *gin.Context) error {
	dep := dependency.FromContext(c)

	pid := pathID(c)
	if pid <= 0 {
		return serializer.NewError(serializer.CodeParamErr, "invalid project id", nil)
	}
	if len(s.FileHashIDs) == 0 {
		return serializer.NewError(serializer.CodeParamErr, "file_hash_ids is required", nil)
	}

	u := inventory.UserFromContext(c)
	if u == nil {
		return serializer.NewError(serializer.CodeCheckLogin, "Login required", nil)
	}

	return trashSvc(dep).Restore(c, u, pid, s.FileHashIDs)
}
