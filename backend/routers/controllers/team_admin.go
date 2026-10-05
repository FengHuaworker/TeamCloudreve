package controllers

import (
	"github.com/cloudreve/Cloudreve/v4/service/team"
	"github.com/gin-gonic/gin"
)

// ============================ 管理端：团队概览 ============================

// AdminTeamListProjects 全站项目列表（仅全局管理员）
func AdminTeamListProjects(c *gin.Context) {
	s := ParametersFromContext[*team.AdminListProjectService](c, team.AdminListProjectParamCtx{})
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// AdminTeamListMembers 全站成员总表（仅全局管理员）
func AdminTeamListMembers(c *gin.Context) {
	s := ParametersFromContext[*team.AdminListMemberService](c, team.AdminListMemberParamCtx{})
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// AdminTeamStats 团队概览统计（仅全局管理员）
func AdminTeamStats(c *gin.Context) {
	s := &team.AdminStatsService{}
	res, err := s.Get(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// ============================ 管理端：权限矩阵 ============================

// AdminTeamGetPermission 读取权限矩阵（仅全局管理员）
func AdminTeamGetPermission(c *gin.Context) {
	s := &team.GetPermissionService{}
	res, err := s.Get(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// AdminTeamSetPermission 更新权限矩阵（仅全局管理员）
func AdminTeamSetPermission(c *gin.Context) {
	s := ParametersFromContext[*team.SetPermissionService](c, team.SetPermissionParamCtx{})
	if err := s.Set(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}
