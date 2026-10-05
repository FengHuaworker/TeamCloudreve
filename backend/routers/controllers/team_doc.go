package controllers

import (
	"github.com/cloudreve/Cloudreve/v4/service/team"
	"github.com/gin-gonic/gin"
)

// ============================ 文档（Wiki）============================

// TeamDocTree 文档树
func TeamDocTree(c *gin.Context) {
	s := &team.ListDocService{}
	res, err := s.Tree(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamCreateDoc 新建文档
func TeamCreateDoc(c *gin.Context) {
	s := ParametersFromContext[*team.CreateDocService](c, team.CreateDocParamCtx{})
	res, err := s.Create(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamGetDoc 文档详情（含正文）
func TeamGetDoc(c *gin.Context) {
	s := &team.DocIDService{}
	res, err := s.Get(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamUpdateDoc 更新文档
func TeamUpdateDoc(c *gin.Context) {
	s := ParametersFromContext[*team.UpdateDocService](c, team.UpdateDocParamCtx{})
	if err := s.Update(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// TeamDeleteDoc 删除文档（级联子文档）
func TeamDeleteDoc(c *gin.Context) {
	s := &team.DocIDService{}
	if err := s.Delete(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// ============================ 通知 ============================

// TeamListNotifications 我的通知
func TeamListNotifications(c *gin.Context) {
	s := ParametersFromContext[*team.ListNotificationService](c, team.ListNotificationParamCtx{})
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamUnreadCount 未读数
func TeamUnreadCount(c *gin.Context) {
	s := &team.UnreadCountService{}
	res, err := s.Get(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamReadNotifications 标记已读
func TeamReadNotifications(c *gin.Context) {
	s := ParametersFromContext[*team.ReadNotificationService](c, team.ReadNotificationParamCtx{})
	if err := s.Read(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}
