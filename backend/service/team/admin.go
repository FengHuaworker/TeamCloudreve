package team

import (
	"strings"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 管理端：团队概览（契约 §3.1）
//
// 与 /team/project 的区别：管理端不做可见性过滤，列出全站所有项目
// （含当前用户不是成员的项目），并附带话题数。
// 全部接口要求 isGlobalAdmin(ctx) 为 true。
// ============================================================

// requireGlobalAdmin 管理端统一鉴权：非 Cloudreve 全局管理员一律拒绝。
//
// 路由上已经挂了 middleware.IsAdmin()，这里再判一次是为了让服务层
// 自身也满足契约 §3 的「isGlobalAdmin(ctx) 为 false 时返回无权限错误」。
func requireGlobalAdmin(c *gin.Context) error {
	if !isGlobalAdmin(c) {
		return serializer.NewError(serializer.CodeNoPermissionErr, "Only global administrators can access team administration", nil)
	}
	return nil
}

type (
	// AdminListProjectService 全站项目列表（query：keyword / page / page_size）
	AdminListProjectService struct {
		Keyword  string `form:"keyword"`
		Page     int    `form:"page"`
		PageSize int    `form:"page_size"`
	}
	AdminListProjectParamCtx struct{}

	// AdminListMemberService 全站成员总表（query：keyword / page / page_size）
	AdminListMemberService struct {
		Keyword  string `form:"keyword"`
		Page     int    `form:"page"`
		PageSize int    `form:"page_size"`
	}
	AdminListMemberParamCtx struct{}

	// AdminStatsService 团队概览统计
	AdminStatsService struct{}
	AdminStatsParamCtx struct{}
)

// List 全站项目列表（附带任务/成员/话题计数）
func (s *AdminListProjectService) List(c *gin.Context) (*AdminProjectListResponse, error) {
	dep := dependency.FromContext(c)
	if err := requireGlobalAdmin(c); err != nil {
		return nil, err
	}

	page, pageSize := pagination(s.Page, s.PageSize)
	keyword := strings.TrimSpace(s.Keyword)

	total, err := dep.TeamClient().CountProjects(c, keyword)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count projects", err)
	}

	projects, err := dep.TeamClient().ListAllProjects(c, keyword, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list projects", err)
	}

	builder := newUserBriefBuilder(c, dep)
	res := make([]*AdminProjectResponse, 0, len(projects))
	for _, p := range projects {
		item := &AdminProjectResponse{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Status:      p.Status,
			Owner:       builder.build(p.OwnerID),
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		}

		if byStatus, cerr := dep.TeamClient().CountTaskByStatus(c, p.ID); cerr == nil {
			for _, n := range byStatus {
				item.TaskTotal += n
			}
			item.DoneTotal = byStatus[inventory.TeamTaskStatusDone]
		}
		// owner 已在成员表里，成员数即成员表行数（不可再 +1，否则重复计数）
		if members, merr := dep.TeamClient().ListMembers(c, p.ID); merr == nil {
			item.MemberTotal = len(members)
		}
		if n, terr := dep.TeamClient().CountTopics(c, p.ID, nil); terr == nil {
			item.TopicTotal = n
		}

		res = append(res, item)
	}

	return &AdminProjectListResponse{Projects: res, Total: total, Page: page, PageSize: pageSize}, nil
}

// List 全站成员总表（某个人在哪些项目里、什么角色）
func (s *AdminListMemberService) List(c *gin.Context) (*AdminMemberListResponse, error) {
	dep := dependency.FromContext(c)
	if err := requireGlobalAdmin(c); err != nil {
		return nil, err
	}

	page, pageSize := pagination(s.Page, s.PageSize)
	keyword := strings.TrimSpace(s.Keyword)

	total, err := dep.TeamClient().CountAllMembers(c, keyword)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count members", err)
	}

	members, err := dep.TeamClient().ListAllMembers(c, keyword, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list members", err)
	}

	// 批量取项目名（项目数量在 10–50 人规模下很小，一次全量查询即可）
	projectNames := make(map[int]string)
	if projects, perr := dep.TeamClient().ListAllProjects(c, "", 0, 0); perr == nil {
		for _, p := range projects {
			projectNames[p.ID] = p.Name
		}
	}

	builder := newUserBriefBuilder(c, dep)
	res := make([]*AdminMemberResponse, 0, len(members))
	for _, m := range members {
		res = append(res, &AdminMemberResponse{
			ProjectID:   m.ProjectID,
			ProjectName: projectNames[m.ProjectID],
			User:        builder.build(m.UserID),
			Role:        m.Role,
			JoinedAt:    m.CreatedAt,
		})
	}

	return &AdminMemberListResponse{Members: res, Total: total, Page: page, PageSize: pageSize}, nil
}

// Get 团队概览统计
func (s *AdminStatsService) Get(c *gin.Context) (*AdminStatsResponse, error) {
	dep := dependency.FromContext(c)
	if err := requireGlobalAdmin(c); err != nil {
		return nil, err
	}

	resp := &AdminStatsResponse{}

	projectTotal, err := dep.TeamClient().CountProjects(c, "")
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count projects", err)
	}
	resp.ProjectTotal = projectTotal

	byStatus, err := dep.TeamClient().CountTasksByStatus(c)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count tasks", err)
	}
	for _, n := range byStatus {
		resp.TaskTotal += n
	}
	resp.DoneTotal = byStatus[inventory.TeamTaskStatusDone]

	memberTotal, err := dep.TeamClient().CountAllMembers(c, "")
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count members", err)
	}
	resp.MemberTotal = memberTotal

	topicTotal, err := dep.TeamClient().CountAllTopics(c)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count topics", err)
	}
	resp.TopicTotal = topicTotal

	postTotal, err := dep.TeamClient().CountAllPosts(c)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count replies", err)
	}
	resp.PostTotal = postTotal

	return resp, nil
}
