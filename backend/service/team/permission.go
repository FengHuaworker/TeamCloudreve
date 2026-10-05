package team

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/setting"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 团队角色权限矩阵（契约 §3.2）
//
// 矩阵持久化在 Cloudreve 的 Setting 表（name = team_role_matrix，value = JSON 字符串），
// 与 Cloudreve 的用户组体系并存：全局管理员不参与配置（天然全权），
// owner 也不可配置（恒为全部能力），可配置的只有 admin / member / viewer。
//
// 读取带进程内缓存：普通请求走缓存，管理端的读写会强制刷新缓存，
// 避免每个请求都去查一次 Setting 表。
// ============================================================

// RoleMatrixSettingName 权限矩阵在 Cloudreve Setting 表中的键名
const RoleMatrixSettingName = "team_role_matrix"

// 能力项。顺序与契约 §3.2 的 capabilities 完全一致（前端按此渲染列）。
const (
	CapViewProject   = "view_project"
	CapCreateTask    = "create_task"
	CapEditTask      = "edit_task"
	CapDeleteTask    = "delete_task"
	CapManageMember  = "manage_member"
	CapManageProject = "manage_project"
	CapPostTopic     = "post_topic"
	CapReplyTopic    = "reply_topic"
	CapModerateTopic = "moderate_topic"
)

// TeamCapabilities 全部能力项，顺序固定不可调整
var TeamCapabilities = []string{
	CapViewProject,
	CapCreateTask,
	CapEditTask,
	CapDeleteTask,
	CapManageMember,
	CapManageProject,
	CapPostTopic,
	CapReplyTopic,
	CapModerateTopic,
}

// ConfigurableRoles 可配置的角色（owner 恒为全权，不参与配置）
var ConfigurableRoles = []string{
	inventory.TeamRoleAdmin,
	inventory.TeamRoleMember,
	inventory.TeamRoleViewer,
}

// RoleMatrix 角色能力矩阵：角色 → 能力 → 是否允许
type RoleMatrix map[string]map[string]bool

// defaultRoleMatrix 契约 §3.2 的默认矩阵。
// 矩阵缺失或损坏时回落到这里；同时它也是 normalize 时的补全依据。
func defaultRoleMatrix() RoleMatrix {
	return RoleMatrix{
		inventory.TeamRoleAdmin: {
			CapViewProject:   true,
			CapCreateTask:    true,
			CapEditTask:      true,
			CapDeleteTask:    true,
			CapManageMember:  true,
			CapManageProject: true,
			CapPostTopic:     true,
			CapReplyTopic:    true,
			CapModerateTopic: true,
		},
		inventory.TeamRoleMember: {
			CapViewProject:   true,
			CapCreateTask:    true,
			CapEditTask:      true,
			CapDeleteTask:    false,
			CapManageMember:  false,
			CapManageProject: false,
			CapPostTopic:     true,
			CapReplyTopic:    true,
			CapModerateTopic: false,
		},
		inventory.TeamRoleViewer: {
			CapViewProject:   true,
			CapCreateTask:    false,
			CapEditTask:      false,
			CapDeleteTask:    false,
			CapManageMember:  false,
			CapManageProject: false,
			CapPostTopic:     false,
			CapReplyTopic:    false,
			CapModerateTopic: false,
		},
	}
}

var (
	roleMatrixMu    sync.RWMutex
	roleMatrixCache RoleMatrix
)

// Capability 判断角色是否具备某项能力（契约 §3.2）。
//
// owner 恒为 true（不可配置）；其余角色查缓存中的矩阵；
// 缓存未就绪（进程刚启动）或矩阵损坏时回落到默认矩阵。
func Capability(role, cap string) bool {
	if role == inventory.TeamRoleOwner {
		return true
	}

	roleMatrixMu.RLock()
	m := roleMatrixCache
	roleMatrixMu.RUnlock()
	if m == nil {
		m = defaultRoleMatrix()
	}
	return matrixAllows(m, role, cap)
}

func matrixAllows(m RoleMatrix, role, cap string) bool {
	caps, ok := m[role]
	if !ok {
		return false
	}
	return caps[cap]
}

// LoadRoleMatrix 读取权限矩阵。缓存命中时零查库；未命中时从 Setting 表加载。
func LoadRoleMatrix(ctx context.Context, dep dependency.Dep) RoleMatrix {
	roleMatrixMu.RLock()
	cached := roleMatrixCache
	roleMatrixMu.RUnlock()
	if cached != nil {
		return cached
	}
	return RefreshRoleMatrix(ctx, dep)
}

// RefreshRoleMatrix 强制从 Setting 表重新加载权限矩阵并刷新缓存。
// 管理端读取时使用，保证看到的一定是库里的最新配置。
func RefreshRoleMatrix(ctx context.Context, dep dependency.Dep) RoleMatrix {
	if dep == nil {
		return defaultRoleMatrix()
	}

	m := defaultRoleMatrix()
	if raw, err := dep.SettingClient().Get(ctx, RoleMatrixSettingName); err == nil {
		if parsed, perr := parseRoleMatrix(raw); perr == nil && parsed != nil {
			m = parsed
		}
		// 值为空或解析失败（损坏）时保持默认矩阵
	}

	roleMatrixMu.Lock()
	roleMatrixCache = m
	roleMatrixMu.Unlock()
	return m
}

// SaveRoleMatrix 持久化权限矩阵（写穿缓存）。
func SaveRoleMatrix(ctx context.Context, dep dependency.Dep, in RoleMatrix) error {
	m := normalizeRoleMatrix(in)

	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}

	client := dep.DBClient()
	existing, err := client.Setting.Query().
		Where(setting.NameEQ(RoleMatrixSettingName)).
		First(ctx)
	switch {
	case err == nil:
		if uerr := client.Setting.UpdateOneID(existing.ID).
			SetValue(string(raw)).Exec(ctx); uerr != nil {
			return uerr
		}
	case ent.IsNotFound(err):
		// Setting 表里没有这一条时创建（SettingClient.Set 只能更新已存在的键）
		if _, cerr := client.Setting.Create().
			SetName(RoleMatrixSettingName).
			SetValue(string(raw)).Save(ctx); cerr != nil {
			return cerr
		}
	default:
		return err
	}

	roleMatrixMu.Lock()
	roleMatrixCache = m
	roleMatrixMu.Unlock()
	return nil
}

// parseRoleMatrix 解析库中的 JSON；格式非法时返回错误（由调用方回落到默认矩阵）
func parseRoleMatrix(raw string) (RoleMatrix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	decoded := make(map[string]map[string]bool)
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, err
	}
	return normalizeRoleMatrix(decoded), nil
}

// normalizeRoleMatrix 以默认矩阵为底做归一化：
// 丢弃未知角色（含 owner）与未知能力，缺失的项用默认值补全。
func normalizeRoleMatrix(in map[string]map[string]bool) RoleMatrix {
	m := defaultRoleMatrix()
	for role, caps := range in {
		if role == inventory.TeamRoleOwner {
			continue
		}
		known, ok := m[role]
		if !ok {
			continue
		}
		for cap, allowed := range caps {
			if _, ok := known[cap]; !ok {
				continue
			}
			known[cap] = allowed
		}
	}
	return m
}

// ============================================================
// 管理端：权限矩阵读写（契约 §3.2）
// ============================================================

type (
	// GetPermissionService 读取权限矩阵
	GetPermissionService struct{}
	GetPermissionParamCtx struct{}

	// SetPermissionService 更新权限矩阵
	SetPermissionService struct {
		Matrix map[string]map[string]bool `json:"matrix" binding:"required"`
	}
	SetPermissionParamCtx struct{}
)

func (s *GetPermissionService) Get(c *gin.Context) (*PermissionResponse, error) {
	dep := dependency.FromContext(c)
	if err := requireGlobalAdmin(c); err != nil {
		return nil, err
	}

	return &PermissionResponse{
		Matrix:       RefreshRoleMatrix(c, dep),
		Capabilities: TeamCapabilities,
		Roles:        ConfigurableRoles,
	}, nil
}

func (s *SetPermissionService) Set(c *gin.Context) error {
	dep := dependency.FromContext(c)
	if err := requireGlobalAdmin(c); err != nil {
		return err
	}
	if len(s.Matrix) == 0 {
		return serializer.NewError(serializer.CodeParamErr, "Permission matrix is required", nil)
	}

	if err := SaveRoleMatrix(c, dep, s.Matrix); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to save team permission matrix", err)
	}
	return nil
}
