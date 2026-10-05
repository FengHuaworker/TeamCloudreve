package controllers

import (
	"github.com/cloudreve/Cloudreve/v4/service/fileacl"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 文件 ACL 控制器（契约补遗二 §4）
//
// 沿用团队模块的 teamOK / teamErr 包装模式（同一个 controllers 包内）。
// 注意：只有挂了 FromJSON / FromQuery 中间件的端点才能用
// ParametersFromContext 取参数，否则会 panic。
// ============================================================

// FileListPermissions 查看某个文件上的 ACL（query: uri）
func FileListPermissions(c *gin.Context) {
	s := ParametersFromContext[*fileacl.ListPermissionService](c, fileacl.ListPermissionParamCtx{})
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// FileSetPermission 设置/覆盖一条 ACL（body）
func FileSetPermission(c *gin.Context) {
	s := ParametersFromContext[*fileacl.SetPermissionService](c, fileacl.SetPermissionParamCtx{})
	res, err := s.Set(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// FileDeletePermission 删除一条 ACL（body）
func FileDeletePermission(c *gin.Context) {
	s := ParametersFromContext[*fileacl.DeletePermissionService](c, fileacl.DeletePermissionParamCtx{})
	if err := s.Delete(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}
