package dbfs

import (
	"context"
	"database/sql"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/boolset"
	"github.com/cloudreve/Cloudreve/v4/pkg/conf"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
	_ "modernc.org/sqlite"
)

// ============================================================
// ACL 判定接入点的回归测试（契约补遗二 §5「生效逻辑」）
//
// 这里直接测 aclGuard —— 也就是 dbfs 各文件操作真正调用的那道闸门，
// 而不是只测数据层，确保「接入点真的接上了」。
//
// 判定顺序必须严格是：
//   全局管理员不受限 → 命中 ACL 以 ACL 为准 → 未命中完全回落到原有判定。
// ============================================================

func newAclGateFixture(t *testing.T, name string) (context.Context, *DBFS, *ent.User, *ent.File) {
	t.Helper()
	ctx := context.Background()

	// 同 inventory 侧：不开外键约束，避免为了 files.owner_id 造完整用户记录
	db, err := sql.Open("sqlite3", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 【必须与生产一致】inventory/client.go:123-124 对 sqlite 设
	// SetMaxOpenConns(1)。不设的话，"事务内另开连接会死锁"这个真实风险
	// 在测试里根本复现不出来 —— 测试会因为连接池够用而假装通过。
	db.SetMaxIdleConns(1)
	db.SetMaxOpenConns(1)

	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", db)))
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("schema create: %v", err)
	}

	// 目录结构：root → D1 → F
	// files.owner_id 是指向 users 的外键，先造真实的组 + 用户
	g, err := client.Group.Create().
		SetName("gate-group").
		SetPermissions(&boolset.BooleanSet{}).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dbUser, err := client.User.Create().
		SetEmail(name + "@test.local").
		SetNick("gate-user").
		SetGroup(g).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	root, err := client.File.Create().SetName("root").SetOwnerID(dbUser.ID).
		SetType(int(types.FileTypeFolder)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := client.File.Create().SetName("项目文档").SetOwnerID(dbUser.ID).
		SetType(int(types.FileTypeFolder)).SetFileChildren(root.ID).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := client.File.Create().SetName("F.txt").SetOwnerID(dbUser.ID).
		SetType(int(types.FileTypeFile)).SetFileChildren(d1.ID).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// 项目根行（team_navigator.findProjectRoot 的判定对象）。
	// 必须满足：OwnerID == 该项目系统账号、Name == projectRootName(pid)、
	// Type == folder、FileChildren 为 nil（表示"这是根"）。
	//
	// 本夹具里 owner 就是 dbUser（真实团队中它是系统账号，
	// 但 findProjectRoot 只按传入的 owner 过滤，故这里用同一个用户即可）。
	projRoot, err := client.File.Create().
		SetName(projectRootName(testProjectID)).
		SetOwnerID(dbUser.ID).
		SetType(int(types.FileTypeFolder)).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	perms := &boolset.BooleanSet{}
	user := &ent.User{ID: dbUser.ID}
	user.Edges.Group = &ent.Group{ID: g.ID, Permissions: perms}

	fs := &DBFS{
		user:       user,
		l:          logging.NewConsoleLogger(logging.LevelError),
		aclClient:  inventory.NewFilePermissionClient(client, conf.SQLiteDB, nil),
		fileClient: inventory.NewFileClient(client, conf.SQLiteDB, mustHashidEncoder()),
	}
	// 把文件句柄包成 dbfs.File（aclGuard 只读 Model.ID）。
	// projRoot 与 client 供能力位用例构造 navigator（见 newTeamNavFixture）。
	lastFixtureClient = client
	lastFixtureProjRoot = projRoot
	lastFixtureUserClient = inventory.NewUserClient(client)
	return ctx, fs, user, f
}

// 能力位用例需要拿到夹具的 DB 客户端与项目根行。
// 测试在单进程内串行执行、且每例自建库，故用包级变量传递是安全的。
var (
	lastFixtureClient     *ent.Client
	lastFixtureProjRoot   *ent.File
	lastFixtureUserClient inventory.UserClient
)

// newTeamNav 在夹具的 DBFS 上构造一个 teamNavigator。
//
// owner 直接指向夹具用户：生产里 owner 是 id=18 的系统账号
// （resolveOwner -> SystemTeamAccount），单测库没有该账号。
// 由于 findProjectRoot 只按传入 owner 过滤，而夹具的项目根行
// 也是同一位用户创建，故语义等价。
func newTeamNav(t *testing.T, fs *DBFS) *teamNavigator {
	t.Helper()
	return &teamNavigator{
		user:          fs.user,
		aclClient:     fs.aclClient,
		fileClient:    fs.fileClient,
		userClient:    lastFixtureUserClient,
		l:             fs.l,
		owner:         fs.user,
		ownerResolved: true,
	}
}

// newTeamNavFixture 构造一个 teamNavigator，owner 直接指向夹具用户，
// 避免 resolveOwner 去查 SystemTeamAccount（那是生产语义，
// 单测库里没有 id=18 的系统账号）。
func newTeamNavFixture(t *testing.T, name string) (context.Context, *DBFS, *teamNavigator) {
	t.Helper()
	ctx, fs, _, _ := newAclGateFixture(t, name)
	return ctx, fs, newTeamNav(t, fs)
}

// mustHashidEncoder 构造一个测试用 hashid 编码器。
// findProjectRoot 不依赖 hashid，但 FileClient 的构造函数要求它非 nil。
func mustHashidEncoder() hashid.Encoder {
	e, err := hashid.New("test-salt-for-unit-tests")
	if err != nil {
		panic(err)
	}
	return e
}

// mustSetAcl 写一条 ACL
func mustSetAcl(t *testing.T, ctx context.Context, fs *DBFS, fileID int, subjectType string,
	subjectID int, permission string, inherit bool) {
	t.Helper()
	if _, err := fs.aclClient.UpsertFilePermission(ctx, &inventory.NewFilePermissionArgs{
		FileID: fileID, SubjectType: subjectType, SubjectID: subjectID,
		Permission: permission, Inherit: inherit, CreatedBy: 1,
	}); err != nil {
		t.Fatalf("set acl: %v", err)
	}
}

// TestAclGuardNoAclFallsThrough 硬要求：没有 ACL 的文件，闸门必须「没有意见」。
// 这正是「行为与本补丁之前一模一样」的那条线。
func TestAclGuardNoAclFallsThrough(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "acl_gate_noacl")

	target := &File{Model: f}
	if err := fs.aclGuard(ctx, target, AclRequireWrite); err != nil {
		t.Fatalf("no ACL -> write must fall through, got %v", err)
	}
	if err := fs.aclGuard(ctx, target, AclRequireRead); err != nil {
		t.Fatalf("no ACL -> read must fall through, got %v", err)
	}

	// ACL 客户端未注入时也必须放行（迁移未跑 / 单机 slave 等场景）
	bare := &DBFS{user: fs.user, l: fs.l, aclClient: nil}
	if err := bare.aclGuard(ctx, target, AclRequireWrite); err != nil {
		t.Fatalf("nil acl client must fall through, got %v", err)
	}
}

// TestAclGuardReadOnlyBlocksWrite ACL 命中 read：读放行、写被拒
func TestAclGuardReadOnlyBlocksWrite(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "acl_gate_readonly")
	mustSetAcl(t, ctx, fs, f.ID, inventory.FileAclSubjectUser, fs.user.ID, inventory.FileAclPermissionRead, true)

	target := &File{Model: f}
	if err := fs.aclGuard(ctx, target, AclRequireRead); err != nil {
		t.Fatalf("read should be allowed by read-only acl, got %v", err)
	}

	err := fs.aclGuard(ctx, target, AclRequireWrite)
	if err == nil {
		t.Fatal("read-only acl must block write, but aclGuard allowed it")
	}
	if !IsAclDenied(err) {
		t.Fatalf("expect ACL denial, got %v", err)
	}
}

// TestAclGuardWriteAllowsWriteAndRead ACL 命中 write：读写都放行
func TestAclGuardWriteAllowsWriteAndRead(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "acl_gate_write")
	mustSetAcl(t, ctx, fs, f.ID, inventory.FileAclSubjectUser, fs.user.ID, inventory.FileAclPermissionWrite, true)

	target := &File{Model: f}
	if err := fs.aclGuard(ctx, target, AclRequireWrite); err != nil {
		t.Fatalf("write acl should allow write, got %v", err)
	}
	if err := fs.aclGuard(ctx, target, AclRequireRead); err != nil {
		t.Fatalf("write acl should allow read, got %v", err)
	}
}

// TestAclGuardInheritedFromParent 继承：父目录设 read，子文件被拦写
func TestAclGuardInheritedFromParent(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "acl_gate_inherit")
	// 父目录项目文档 = f.FileChildren
	mustSetAcl(t, ctx, fs, f.FileChildren, inventory.FileAclSubjectUser, fs.user.ID,
		inventory.FileAclPermissionRead, true)

	target := &File{Model: f}
	if err := fs.aclGuard(ctx, target, AclRequireRead); err != nil {
		t.Fatalf("inherited read should allow read, got %v", err)
	}
	if err := fs.aclGuard(ctx, target, AclRequireWrite); !IsAclDenied(err) {
		t.Fatalf("inherited read must block write, got %v", err)
	}
}

// TestAclGuardBoundary inherit=false 的文件夹是继承边界：
// 其子项既不该拿到该目录自身的条目，也不该继续向上继承。
//
// 场景刻意做成「去掉边界逻辑就会被看出来」：
//
//	root  : user read  , inherit=true
//	└ D1  : user write , inherit=false   ← 边界
//	  └ F : 无 ACL
//
// 有边界逻辑 → 走到 D1 即停止，闸门「没有意见」(nil)，写放行（回落原有判定）；
// 若边界逻辑被删掉 → 会继续向上命中 root 的 read，从而错误地拒绝写入。
func TestAclGuardBoundary(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "acl_gate_boundary")
	d1ID := f.FileChildren // 项目文档
	d1, err := fs.aclClient.GetClient().File.Get(ctx, d1ID)
	if err != nil {
		t.Fatal(err)
	}

	mustSetAcl(t, ctx, fs, d1.FileChildren, inventory.FileAclSubjectUser, fs.user.ID,
		inventory.FileAclPermissionRead, true)
	mustSetAcl(t, ctx, fs, d1ID, inventory.FileAclSubjectUser, fs.user.ID,
		inventory.FileAclPermissionWrite, false)

	target := &File{Model: f}
	if err := fs.aclGuard(ctx, target, AclRequireWrite); err != nil {
		t.Fatalf("inherit=false boundary must stop the upward walk, got %v", err)
	}

	// 反证：把 D1 的 inherit 改成 true 后，子项就应当拿到 write（放行），
	// 而 root 的 read 不再影响它 —— 说明上面那次 nil 确实来自边界逻辑。
	mustSetAcl(t, ctx, fs, d1ID, inventory.FileAclSubjectUser, fs.user.ID,
		inventory.FileAclPermissionWrite, true)
	if err := fs.aclGuard(ctx, target, AclRequireWrite); err != nil {
		t.Fatalf("inherit=true entry on the parent must let the child write, got %v", err)
	}
}

// TestAclGuardGlobalAdminBypass 全局管理员不受 ACL 限制（验收 8）
func TestAclGuardGlobalAdminBypass(t *testing.T) {
	ctx, fs, user, f := newAclGateFixture(t, "acl_gate_admin")
	mustSetAcl(t, ctx, fs, f.ID, inventory.FileAclSubjectUser, fs.user.ID, inventory.FileAclPermissionRead, true)

	target := &File{Model: f}
	if err := fs.aclGuard(ctx, target, AclRequireWrite); !IsAclDenied(err) {
		t.Fatalf("sanity: non-admin should be blocked, got %v", err)
	}

	// 打开 GroupPermissionIsAdmin 后必须放行
	boolset.Set(int(types.GroupPermissionIsAdmin), true, user.Edges.Group.Permissions)
	if err := fs.aclGuard(ctx, target, AclRequireWrite); err != nil {
		t.Fatalf("global admin must bypass ACL, got %v", err)
	}
}

// TestAclGuardIgnoresOwnerBypassKey 所有者旁路【不得】跳过 ACL 判定。
//
// 【2026-10-04 第 3 步】本用例此前名为 TestAclGuardBypassContext，
// 断言的是「ByPassOwnerCheck 上下文必须跳过 ACL」—— 那是把【缺陷当规格】：
//
//	该行为导致 ACL 在所有设置了所有者旁路的路径上从不生效，
//	而团队写路径全部设置旁路（生产 12 处设置点中 9 处在 team），
//	于是 ACL 在团队空间等于装饰（实测：只读主体写只读目录 code=0）。
//
// 现在断言【相反】的行为：两种语义必须分开 ——
//
//	ByPassOwnerCheckCtxKey -> 只豁免「所有者校验」
//	本 key 绝不豁免「ACL 判定」
//
// ⚠️ 这条断言是第 3 步的核心守卫：若将来有人恢复那段短路，本用例立即失败。
func TestAclGuardIgnoresOwnerBypassKey(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "acl_gate_bypass")
	mustSetAcl(t, ctx, fs, f.ID, inventory.FileAclSubjectUser, fs.user.ID, inventory.FileAclPermissionRead, true)

	target := &File{Model: f}
	bypass := context.WithValue(ctx, ByPassOwnerCheckCtxKey{}, true)
	err := fs.aclGuard(bypass, target, AclRequireWrite)
	if !IsAclDenied(err) {
		t.Fatalf("owner bypass must NOT skip ACL check, want AclDenied, got %v", err)
	}

	// 反向：读取需求仍应放行（ACL 只在写时拒绝）——
	// 这保证上面的失败不是因为"整个 aclGuard 坏了"。
	if err := fs.aclGuard(bypass, target, AclRequireRead); err != nil {
		t.Fatalf("read must still pass under owner bypass, got %v", err)
	}
}

// TestAclGuardUnrelatedSubject 别的用户的 ACL 不影响当前用户
func TestAclGuardUnrelatedSubject(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "acl_gate_other")
	mustSetAcl(t, ctx, fs, f.ID, inventory.FileAclSubjectUser, fs.user.ID+1000, inventory.FileAclPermissionRead, true)

	target := &File{Model: f}
	if err := fs.aclGuard(ctx, target, AclRequireWrite); err != nil {
		t.Fatalf("acl for another user must not apply, got %v", err)
	}
}
