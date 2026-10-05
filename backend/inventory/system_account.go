package inventory

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudreve/Cloudreve/v4/application/constants"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/group"
	"github.com/cloudreve/Cloudreve/v4/ent/setting"
	"github.com/cloudreve/Cloudreve/v4/ent/user"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/boolset"
	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
	"github.com/cloudreve/Cloudreve/v4/pkg/util"
	"github.com/google/uuid"
)

// 团队空间系统账号相关的 settings 键。
//
// 为什么不写死源码常量：
//
//	Cloudreve 官方对系统账号/系统组一律硬编码 ID —— AnonymousGroupID = 3、
//	migration.go 里的 group.ID(2)、admin/user.go 里的 id == 1。
//	但这些 ID 由数据库自增分配，实测本实例的 users 表在只有 3 个用户时
//	自增序列已走到 18（历史测试账号删除留下空洞），硬编码必然失准。
//
//	因此团队系统账号的 ID 一律从 settings 表读取，重建环境（ID 变化）不会错。
const (
	// SystemTeamGroupIDKey 记录团队空间专用组的 ID。
	SystemTeamGroupIDKey = "system_team_group_id"
	// SystemTeamUserIDKey 记录团队空间系统账号的 ID。
	SystemTeamUserIDKey = "system_team_user_id"
	// SystemTeamInitializedKey 标记团队空间系统账号已初始化（幂等用）。
	SystemTeamInitializedKey = "system_team_initialized"

	// SystemTeamEmailDomain 是团队系统账号使用的邮箱域。
	//
	// RFC 2606 保留 TLD，永不解析。使用 UUID 作为 local part 保证唯一。
	// ⚠️ 注意：该域能通过注册校验（实测 go-playground/validator 的 email 正则），
	// 因此注册流程必须显式拦截，见 IsSystemTeamEmail。
	SystemTeamEmailDomain = "team-space.invalid"
)

// ErrSystemAccountProtected 表示操作目标是被保护的系统账号。
var ErrSystemAccountProtected = errors.New("system account is protected")

// IsSystemTeamEmail 判断邮箱是否属于团队系统账号专用域。
//
// 用于注册拦截：该域能通过常规 email 校验，若不拦截，
// 任何人都可以注册一个 xxx@team-space.invalid 的账号，
// 与团队系统账号的邮箱空间发生冲突。
func IsSystemTeamEmail(email string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@"+SystemTeamEmailDomain)
}

// loadSystemTeamIDs 从 settings 表读取团队系统账号/组的 ID。
// 未初始化时返回 (0, 0, nil)，调用方据此视为"无系统账号"。
func loadSystemTeamIDs(ctx context.Context, client *ent.Client) (userID, groupID int, err error) {
	rows, err := client.Setting.Query().
		Where(setting.NameIn(SystemTeamUserIDKey, SystemTeamGroupIDKey)).
		All(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to load system team ids: %w", err)
	}

	for _, row := range rows {
		v, convErr := strconv.Atoi(row.Value)
		if convErr != nil {
			return 0, 0, fmt.Errorf("invalid %s value %q: %w", row.Name, row.Value, convErr)
		}

		switch row.Name {
		case SystemTeamUserIDKey:
			userID = v
		case SystemTeamGroupIDKey:
			groupID = v
		}
	}

	return userID, groupID, nil
}

// SystemTeamIDs 返回团队系统账号与专用组的 ID。
// 未初始化时返回 (0, 0, nil)。
func SystemTeamIDs(ctx context.Context, client *ent.Client) (userID, groupID int, err error) {
	return loadSystemTeamIDs(ctx, client)
}

// IsSystemAccount 判断给定用户是否为受保护的系统账号。
//
// 判定依据是 settings 表登记的 ID（不写死源码常量），因此：
//   - 重建环境（自增 ID 变化）后依然正确
//   - 无需新增 ent schema 字段
//
// 所有"需要排除/保护系统账号"的地方都必须调用本函数，
// 不要散落 ID 字面量比较 —— 官方 id == 1 那种写法就是反面教材。
func IsSystemAccount(ctx context.Context, client *ent.Client, u *ent.User) bool {
	if u == nil || u.ID == 0 {
		return false
	}

	userID, _, err := loadSystemTeamIDs(ctx, client)
	if err != nil {
		// 读不到设置时保守返回 false，避免因读写异常把真实用户当成系统账号而误伤。
		return false
	}

	return userID != 0 && u.ID == userID
}

// IsSystemAccountGroupID 判断给定组 ID 是否为团队空间专用组。
func IsSystemAccountGroupID(ctx context.Context, client *ent.Client, groupID int) bool {
	if groupID == 0 {
		return false
	}

	_, sysGroupID, err := loadSystemTeamIDs(ctx, client)
	if err != nil {
		return false
	}

	return sysGroupID != 0 && groupID == sysGroupID
}

// SystemTeamAccount 返回团队空间系统账号（未初始化时返回 error）。
func SystemTeamAccount(ctx context.Context, client *ent.Client) (*ent.User, error) {
	userID, _, err := loadSystemTeamIDs(ctx, client)
	if err != nil {
		return nil, err
	}

	if userID == 0 {
		return nil, ErrSystemAccountNotFound
	}

	u, err := client.User.Query().Where(user.ID(userID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load system team account: %w", err)
	}

	return u, nil
}

// ErrSystemAccountNotFound 表示团队系统账号尚未初始化。
var ErrSystemAccountNotFound = errors.New("system team account is not initialized")

// EnsureSystemTeamAccount 幂等地建立团队空间系统账号与专用组。
//
// # 为什么必须有这个函数
//
// 在它存在之前，全仓【没有任何一处创建系统账号】：
// SystemTeamAccount / SystemTeamIDs 都只读 settings 里登记的 ID，
// 读不到就返回 ErrSystemAccountNotFound。
// 后果是：一个新建的库（用户全新安装）里系统账号永远不存在，
// 访问 cloudreve://team 稳定失败：
//
//	parent not exist: failed to resolve team space owner:
//	system team account is not initialized
//
// 且不会自愈。已有环境之所以正常，只是因为迁移脚本手工建过一次。
// 也就是说：**升级路径正常，安装路径是坏的** —— 而交付给用户的是安装路径。
//
// # 为什么放在迁移里，而不是"首次访问时懒创建"
//
// 懒创建的调用点必然落在 team_navigator 的构造里（读路径），
// 而读路径会在 WithTx 事务内被调用（见 dbfs.go getNavigator）。
// 在事务内再开一个写连接，正是本项目已经踩过的单连接 SQLite 死锁
// （inventory/client.go 对 sqlite 设 SetMaxOpenConns(1)）。
// 而且它的触发条件极窄 —— 只在"库不完整"时走到，
// 平时测不出来、用户装完才炸，与本缺陷完全同形。
//
// # 幂等与共存（两条硬要求）
//
//  1. 幂等：以 settings 里的 SystemTeamInitializedKey 为标记先行短路，
//     重复跑迁移不产生第二份数据。
//  2. 与既有数据共存：本函数必须能安全跑在【已经手工建过系统账号的库】上
//     （我们生产库就是这种状态：settings 里已有 ID 登记）。
//     因此【先读 settings】—— 登记存在即视为已初始化，直接返回，
//     不改动既有行的 id/email，避免把生产库的系统账号换掉。
//
// 注意不要用"按 email 查重"来判断是否已存在：email 的 local part 是 UUID，
// 每次生成的都不同，查重必然查不到，从而建出第二份。判定必须以 settings 标记为准。
func EnsureSystemTeamAccount(ctx context.Context, client *ent.Client) (*ent.User, error) {
	return EnsureSystemTeamAccountWithLog(ctx, client, nil)
}

// EnsureSystemTeamAccountWithLog 是带日志的实现。
//
// 日志不是装饰：【"迁移跑了"和"迁移什么都没改"是两件事】，必须分别可观测。
// 没有日志时，只能看到"服务起来了"，无法判断这一步是否执行过。
// （本函数第一次提交时漏了日志，正是因此无法验证它有没有被调用。）
func EnsureSystemTeamAccountWithLog(ctx context.Context, client *ent.Client, l logging.Logger) (*ent.User, error) {
	infof := func(format string, a ...any) {
		if l != nil {
			l.Info(format, a...)
		}
	}

	// ---- 幂等短路 1：settings 已登记 ID -> 说明初始化过（含手工建立的库）----
	userID, groupID, err := loadSystemTeamIDs(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to load system team ids: %w", err)
	}
	if userID != 0 && groupID != 0 {
		if u, qErr := client.User.Query().Where(user.ID(userID)).Only(ctx); qErr == nil {
			infof("System team account already initialized (user id=%d, group id=%d), skip.", u.ID, groupID)
			return u, nil
		}
		infof("System team account id=%d is recorded but missing, recreating.", userID)
	}

	// ---- 幂等短路 2：标记位 ----
	//
	// ⚠️ 标记位的【值不固定】：既有库写的是 "installed"（与 db_version_team_v1..v5 同一惯例），
	// 本函数写的是 "1"。所以判定必须是"非空即视为已初始化"，
	// 不能写成 == "1" —— 否则对既有库失效，
	// 而且会让新库和老库用两种不同的值表达同一件事。
	if v, err := client.Setting.Query().
		Where(setting.NameEQ(SystemTeamInitializedKey)).
		Only(ctx); err == nil && strings.TrimSpace(v.Value) != "" && userID != 0 {
		if u, qErr := client.User.Query().Where(user.ID(userID)).Only(ctx); qErr == nil {
			infof("System team account initialized marker present, skip.")
			return u, nil
		}
	}

	infof("Initializing system team account...")

	// ---- 专用组 ----
	sysGroup, err := ensureSystemTeamGroup(ctx, client, groupID)
	if err != nil {
		return nil, err
	}

	// ---- 系统账号 ----
	sysUser, err := ensureSystemTeamUser(ctx, client, userID, sysGroup.ID)
	if err != nil {
		return nil, err
	}

	// ---- 登记 ID（幂等 upsert）----
	if err := upsertSetting(ctx, client, SystemTeamUserIDKey, strconv.Itoa(sysUser.ID)); err != nil {
		return nil, fmt.Errorf("failed to record system team user id: %w", err)
	}
	if err := upsertSetting(ctx, client, SystemTeamGroupIDKey, strconv.Itoa(sysGroup.ID)); err != nil {
		return nil, fmt.Errorf("failed to record system team group id: %w", err)
	}
	if err := upsertSetting(ctx, client, SystemTeamInitializedKey, "1"); err != nil {
		return nil, fmt.Errorf("failed to record system team initialized flag: %w", err)
	}

	infof("System team account initialized (user id=%d, group id=%d).", sysUser.ID, sysGroup.ID)
	return sysUser, nil
}

// ensureSystemTeamGroup 返回团队空间专用组：已存在则原样返回，否则创建。
func ensureSystemTeamGroup(ctx context.Context, client *ent.Client, knownID int) (*ent.Group, error) {
	if knownID != 0 {
		if g, err := client.Group.Query().Where(group.ID(knownID)).Only(ctx); err == nil {
			return g, nil
		}
	}

	// 复用既有的系统组名做二次确认，避免"登记丢了但组还在"时建出重复组。
	if g, err := client.Group.Query().Where(group.NameEQ(SystemTeamGroupName)).Only(ctx); err == nil {
		return g, nil
	}

	permissions := &boolset.BooleanSet{}
	boolset.Sets(map[types.GroupPermission]bool{
		// 与 User 组同权：能读写自己的文件、能分享，但【不是】管理员。
		types.GroupPermissionShare:            true,
		types.GroupPermissionShareDownload:    true,
		types.GroupPermissionRedirectedSource: true,
	}, permissions)

	g, err := client.Group.Create().
		SetName(SystemTeamGroupName).
		SetStoragePoliciesID(1).
		SetMaxStorage(100 * constants.GB).
		SetPermissions(permissions).
		SetSettings(&types.GroupSetting{
			SourceBatchSize: 10,
			Aria2BatchSize:  1,
			MaxWalkedFiles:  100000,
			TrashRetention:  7 * 24 * 3600,
		}).
		Save(ctx)
	if err != nil {
		// 并发下可能已被别的进程建出 —— 再查一次，查得到就用它。
		if g2, qErr := client.Group.Query().Where(group.NameEQ(SystemTeamGroupName)).Only(ctx); qErr == nil {
			return g2, nil
		}
		return nil, fmt.Errorf("failed to create system team group: %w", err)
	}

	return g, nil
}

// ensureSystemTeamUser 返回团队空间系统账号：已存在则原样返回，否则创建。
func ensureSystemTeamUser(ctx context.Context, client *ent.Client, knownID, groupID int) (*ent.User, error) {
	if knownID != 0 {
		if u, err := client.User.Query().Where(user.ID(knownID)).Only(ctx); err == nil {
			return u, nil
		}
	}

	// 邮箱 local part 用 UUID：每次生成的都不同，所以【不能】靠 email 查重，
	// 这里的查询只用于兜住"同一次 UUID 恰好已存在"的极端并发。
	email := uuid.NewString() + "@" + SystemTeamEmailDomain
	if u, err := client.User.Query().Where(user.EmailEQ(email)).Only(ctx); err == nil {
		return u, nil
	}

	// 密码取随机值：系统账号不接受密码登录（且需要密码摘要非空）。
	pwdDigest, err := digestPassword(util.RandStringRunesCrypto(32))
	if err != nil {
		return nil, fmt.Errorf("failed to generate system account password: %w", err)
	}

	u, err := client.User.Create().
		SetEmail(email).
		SetNick(SystemTeamAccountNick).
		SetPassword(pwdDigest).
		SetStorage(0).
		SetGroupID(groupID).
		SetStatus(user.StatusActive).
		SetSettings(&types.UserSetting{VersionRetention: true, VersionRetentionMax: 10}).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create system team account: %w", err)
	}

	return u, nil
}

// upsertSetting 写入或更新一条 settings。
//
// 不能直接用 Create()：settings.name 上存在唯一约束，
// 已存在时会报唯一冲突（而不是更新）。
func upsertSetting(ctx context.Context, client *ent.Client, name, value string) error {
	exists, err := client.Setting.Query().Where(setting.NameEQ(name)).Exist(ctx)
	if err != nil {
		return err
	}

	if exists {
		if err := client.Setting.Update().
			Where(setting.NameEQ(name)).
			SetValue(value).
			Exec(ctx); err != nil {
			return err
		}
		return nil
	}

	_, err = client.Setting.Create().SetName(name).SetValue(value).Save(ctx)
	return err
}

const (
	// SystemTeamGroupName 是团队空间专用组的名称。
	SystemTeamGroupName = "System Team Space"
	// SystemTeamAccountNick 是团队空间系统账号的显示名。
	SystemTeamAccountNick = "Team Space"
)
