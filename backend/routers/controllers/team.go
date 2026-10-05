package controllers

import (
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/cloudreve/Cloudreve/v4/service/team"
	"github.com/gin-gonic/gin"
)

// 统一的响应包装，避免每个 handler 重复写 c.JSON。
func teamOK(c *gin.Context, data any) {
	c.JSON(200, serializer.Response{Data: data})
}

func teamErr(c *gin.Context, err error) {
	c.JSON(200, serializer.Err(c, err))
}

// 注意：只有挂了 FromJSON / FromQuery / FromForm 中间件的端点，
// 才能用 ParametersFromContext 取参数；否则 context 中无值，
// 类型断言会 panic。无参端点必须直接实例化服务结构体。

// ============================ 项目 ============================

// TeamListProjects 列出当前用户可见的项目
func TeamListProjects(c *gin.Context) {
	s := &team.ListProjectService{}
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamCreateProject 创建项目
func TeamCreateProject(c *gin.Context) {
	s := ParametersFromContext[*team.CreateProjectService](c, team.CreateProjectParamCtx{})
	res, err := s.Create(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamGetBoard 获取项目看板
func TeamGetBoard(c *gin.Context) {
	s := &team.ProjectIDService{}
	res, err := s.Get(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamUpdateProject 更新项目
func TeamUpdateProject(c *gin.Context) {
	s := ParametersFromContext[*team.UpdateProjectService](c, team.UpdateProjectParamCtx{})
	if err := s.Update(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// TeamDeleteProject 删除项目
func TeamDeleteProject(c *gin.Context) {
	s := &team.ProjectIDService{}
	if err := s.Delete(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// ============================ 成员 ============================

// TeamListMembers 列出项目成员
func TeamListMembers(c *gin.Context) {
	s := &team.MemberService{}
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamAddMember 添加项目成员
func TeamAddMember(c *gin.Context) {
	s := ParametersFromContext[*team.AddMemberService](c, team.AddMemberParamCtx{})
	if err := s.Add(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// TeamRemoveMember 移除/退出项目成员
func TeamRemoveMember(c *gin.Context) {
	s := ParametersFromContext[*team.RemoveMemberService](c, team.RemoveMemberParamCtx{})
	if err := s.Remove(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// ============================ 任务 ============================

// TeamCreateTask 创建任务
func TeamCreateTask(c *gin.Context) {
	s := ParametersFromContext[*team.CreateTaskService](c, team.CreateTaskParamCtx{})
	res, err := s.Create(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamGetTask 任务详情
func TeamGetTask(c *gin.Context) {
	s := &team.TaskIDService{}
	res, err := s.Get(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamUpdateTask 更新任务
func TeamUpdateTask(c *gin.Context) {
	s := ParametersFromContext[*team.UpdateTaskService](c, team.UpdateTaskParamCtx{})
	if err := s.Update(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// TeamDeleteTask 删除任务
func TeamDeleteTask(c *gin.Context) {
	s := &team.TaskIDService{}
	if err := s.Delete(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// TeamMoveTask 看板拖拽（改状态/排序）
func TeamMoveTask(c *gin.Context) {
	s := ParametersFromContext[*team.MoveTaskService](c, team.MoveTaskParamCtx{})
	if err := s.Move(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// TeamMyTasks 我负责的任务（跨项目工作台）
func TeamMyTasks(c *gin.Context) {
	s := ParametersFromContext[*team.MyTasksService](c, team.MyTasksParamCtx{})
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// ============================ 评论 ============================

// TeamListComments 评论列表
func TeamListComments(c *gin.Context) {
	s := &team.CommentService{}
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamCreateComment 发表评论
func TeamCreateComment(c *gin.Context) {
	s := ParametersFromContext[*team.CreateCommentService](c, team.CreateCommentParamCtx{})
	res, err := s.Create(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// ============================ 任务协作者 ============================

// TeamAddCollaborator 添加任务协作者（body: user_id=hashid）
func TeamAddCollaborator(c *gin.Context) {
	s := ParametersFromContext[*team.AddCollaboratorService](c, team.AddCollaboratorParamCtx{})
	res, err := s.Add(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamRemoveCollaborator 移除任务协作者（query: user_id=hashid）
func TeamRemoveCollaborator(c *gin.Context) {
	s := ParametersFromContext[*team.RemoveCollaboratorService](c, team.RemoveCollaboratorParamCtx{})
	res, err := s.Remove(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// ============================ 附件 ============================

// TeamListAttachments 附件列表
func TeamListAttachments(c *gin.Context) {
	s := &team.AttachmentService{}
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamCreateAttachment 挂载 Cloudreve 文件为任务附件
func TeamCreateAttachment(c *gin.Context) {
	s := ParametersFromContext[*team.CreateAttachmentService](c, team.CreateAttachmentParamCtx{})
	res, err := s.Create(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamDeleteAttachment 移除附件（不删除原文件）
func TeamDeleteAttachment(c *gin.Context) {
	s := &team.AttachmentIDService{}
	if err := s.Delete(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}

// TeamAttachmentURL 返回任务附件的访问 URL（B+ 访问通道）。
//
// 授权凭证是「该附件所属项目的成员身份」，见
// service/team/attachment_access.go 的说明。
func TeamAttachmentURL(c *gin.Context) {
	s := &team.AttachmentIDParamCtx{}
	res, err := s.GetTaskAttachmentURL(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamTopicAttachmentURL 返回话题附件的访问 URL（B+ 访问通道）。
func TeamTopicAttachmentURL(c *gin.Context) {
	s := &team.TopicAttachmentIDParamCtx{}
	res, err := s.GetTopicAttachmentURL(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// ============================ 动态与统计 ============================

// TeamListActivities 项目动态
func TeamListActivities(c *gin.Context) {
	s := ParametersFromContext[*team.ListActivityService](c, team.ListActivityParamCtx{})
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamGetStats 分工与进度统计
func TeamGetStats(c *gin.Context) {
	s := &team.StatsService{}
	res, err := s.Get(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// ============================ 项目级团队回收站 ============================

// TeamListTrash 列出项目回收站（无请求体 -> 直接实例化，见文件头注释）。
func TeamListTrash(c *gin.Context) {
	s := &team.TrashListService{}
	res, err := s.List(c)
	if err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, res)
}

// TeamRestoreTrash 恢复项目回收站中的文件。
func TeamRestoreTrash(c *gin.Context) {
	s := ParametersFromContext[*team.TrashRestoreService](c, team.TrashRestoreParamCtx{})
	if err := s.Restore(c); err != nil {
		teamErr(c, err)
		return
	}
	teamOK(c, nil)
}
