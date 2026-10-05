package controllers

import (
	"github.com/cloudreve/Cloudreve/v4/service/team"
	"github.com/gin-gonic/gin"
)

// ============================ 讨论区：话题 ============================

// TeamListTopics 话题列表（keyword / category / 分页）
func TeamListTopics(c *gin.Context) {
	s := ParametersFromContext[*team.ListTopicService](c, team.ListTopicParamCtx{})
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamCreateTopic 创建话题
func TeamCreateTopic(c *gin.Context) {
	s := ParametersFromContext[*team.CreateTopicService](c, team.CreateTopicParamCtx{})
	res, err := s.Create(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamGetTopic 话题详情（含当前页楼层，浏览量 +1）
func TeamGetTopic(c *gin.Context) {
	s := &team.TopicIDService{}
	res, err := s.Get(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamUpdateTopic 更新话题（作者或 moderate_topic）
func TeamUpdateTopic(c *gin.Context) {
	s := ParametersFromContext[*team.UpdateTopicService](c, team.UpdateTopicParamCtx{})
	if err := s.Update(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// TeamDeleteTopic 删除话题（级联删除楼层）
func TeamDeleteTopic(c *gin.Context) {
	s := &team.TopicIDService{}
	if err := s.Delete(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// TeamTopicState 置顶 / 锁定 / 标记已解决（moderate_topic）
func TeamTopicState(c *gin.Context) {
	s := ParametersFromContext[*team.TopicStateService](c, team.TopicStateParamCtx{})
	if err := s.State(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// ============================ 讨论区：楼层 ============================

// TeamReplyTopic 回复话题（返回刷新后的当前页楼层）
func TeamReplyTopic(c *gin.Context) {
	s := ParametersFromContext[*team.ReplyTopicService](c, team.ReplyTopicParamCtx{})
	res, err := s.Reply(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamDeletePost 删除楼层（作者或 moderate_topic）
func TeamDeletePost(c *gin.Context) {
	s := &team.PostIDService{}
	if err := s.Delete(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// ============================ 讨论区：附件（契约补遗 §C）============================

// TeamListTopicAttachments 话题附件列表（项目成员可读）
func TeamListTopicAttachments(c *gin.Context) {
	s := &team.TopicAttachmentParamCtx{}
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamCreateTopicAttachment 上传附件到话题，返回刷新后的全量列表
func TeamCreateTopicAttachment(c *gin.Context) {
	s := ParametersFromContext[*team.CreateTopicAttachmentService](c, team.CreateTopicAttachmentParamCtx{})
	res, err := s.Create(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamDeleteTopicAttachment 删除附件（上传者本人或 moderate_topic）
func TeamDeleteTopicAttachment(c *gin.Context) {
	s := &team.TopicAttachmentIDService{}
	if err := s.Delete(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}
