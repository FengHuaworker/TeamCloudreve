package dbfs

// ============================================================
// 团队空间【动态能力位】单元测试
//
// 背景：团队空间的能力位必须随主体的 ACL 变化 ——
//   只读主体 -> 写位必须为假（否则前端说谎）
//   可写主体 -> 写位必须为真（否则功能不可用）
// 同时都要求【读位恒真】—— 只读成员必须仍能列目录/进目录，
// 否则他不是"只读"，是"看不见"。
//
// 【2026-10-04 修订：判定对象从 node 改为 projectID】
//
// 原实现让 setCapabilityTarget 查库取项目根、塞进 t.node。
// 实测【死锁】：getNavigator 会在事务内被调用，SQLite 单连接下
// 事务内再查库会永久等待（团队上传 16s 超时，个人空间 0.3s 正常）。
//
// 现在：setCapabilityTarget 只记 capabilityProjectID（零查库），
// canWriteProjectID 在事务外按需查项目根。
// 因此用例改为直接调 canWriteProjectID，语义等价且不依赖 node。
// ============================================================

import (
	"testing"
	"time"

	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/boolset"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
)

// testProjectID 是本组用例使用的项目 id。
// 夹具会在库里建一条 Name == projectRootName(testProjectID) 的项目根行。
const testProjectID = 2

// boolsetOf 构造一个只含指定位的 BooleanSet。
func boolsetOf(bits ...int) *boolset.BooleanSet {
	s := &boolset.BooleanSet{}
	for _, b := range bits {
		boolset.Set(b, true, s)
	}
	return s
}

// mustParseURI 解析 URI，失败即 Fatal。
func mustParseURI(t *testing.T, raw string) *fs.URI {
	t.Helper()
	u, err := fs.NewUriFromString(raw)
	if err != nil {
		t.Fatalf("解析 URI %q 失败: %v", raw, err)
	}
	return u
}

// TestTeamCapabilityReadBitsAlwaysOn 读位与 ACL 无关，恒为真。
//
// 这是本组用例里最重要的一条：若读位被 ACL 影响，
// 只读成员会因 ListChildren=false 而完全无法访问团队空间。
func TestTeamCapabilityReadBitsAlwaysOn(t *testing.T) {
	ctx, fs, _, _ := newAclGateFixture(t, "team_cap_readonly_bits")
	// ACL 必须设在【项目根】上 —— 能力位判定看的是项目根，
	// 不是路径末端那个文件（新建/上传时它甚至还不存在）。
	rootID := lastFixtureProjRoot.ID
	mustSetAcl(t, ctx, fs, rootID, inventory.FileAclSubjectUser, fs.user.ID, inventory.FileAclPermissionRead, true)

	n := newTeamNav(t, fs)

	// 只读主体：写位关
	if n.canWriteProjectID(testProjectID) {
		t.Fatalf("read-only subject must NOT be able to write")
	}

	// 读位必须恒真
	caps := teamNavigatorCapability
	for _, bit := range []NavigatorCapability{
		NavigatorCapabilityListChildren,
		NavigatorCapabilityDownloadFile,
		NavigatorCapabilityInfo,
		NavigatorCapabilityEnterFolder,
		NavigatorCapabilityGenerateThumb,
	} {
		if !caps.Enabled(int(bit)) {
			t.Fatalf("read capability %v must stay enabled for read-only subject (else they cannot even browse)", bit)
		}
	}
	// 只读集本身不含写位
	for _, bit := range []NavigatorCapability{
		NavigatorCapabilityCreateFile,
		NavigatorCapabilityUploadFile,
		NavigatorCapabilityRenameFile,
		NavigatorCapabilityDeleteFile,
	} {
		if caps.Enabled(int(bit)) {
			t.Fatalf("read-only capability set MUST NOT contain write bit %v", bit)
		}
	}
}

// TestTeamCapabilityWriteBitsWhenAclWrite 有 write ACL 时写位开放。
//
// 反向断言：若只测"只读时为假"，无法区分"正确实现"与"写位永远为假"。
func TestTeamCapabilityWriteBitsWhenAclWrite(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "team_cap_write_bits")
	mustSetAcl(t, ctx, fs, f.ID, inventory.FileAclSubjectUser, fs.user.ID, inventory.FileAclPermissionWrite, true)

	n := newTeamNav(t, fs)
	if !n.canWriteProjectID(testProjectID) {
		t.Fatalf("write ACL MUST enable write capability (else the feature is unusable)")
	}

	// 写集必须真的含写位
	if !teamNavigatorWriteCapability.Enabled(int(NavigatorCapabilityUploadFile)) {
		t.Fatalf("write capability set MUST contain UploadFile")
	}
}

// TestTeamCapabilityNoAclIsWritable 无 ACL 时必须【放行】（写位开）。
//
// 【这条守的是我犯过的错误方向】
// 我曾让"无 ACL"返回不可写，理由写成"回落给原有的所有者判定，它必然拒绝"。
// 该理由【前提错误】：团队写路径的所有者判定已被 withTeamOwnerBypassIfTeam
// 旁路（manage.go:25、upload.go:22,83,331,444），manage.go:74 的
// `!ok && owner != user` 恒为假。
//
// 而 acl.go 头部契约白纸黑字写着：
//
//	「ACL 客户端未注入、文件上没有任何 ACL、解析出错 —— 三种情况一律放行」
//
// 所以正确状态是「能力位与后端一致 = 都放行」，不是"取更保守的那个"。
// 默认（0 条 ACL）下判错，会把团队写入口对所有人关闭。
func TestTeamCapabilityNoAclIsWritable(t *testing.T) {
	_, fs, _, _ := newAclGateFixture(t, "team_cap_no_acl")

	n := newTeamNav(t, fs)
	if !n.canWriteProjectID(testProjectID) {
		t.Fatalf("no ACL rows MUST be treated as writable (acl.go contract: 一律放行)")
	}
}

// TestTeamCapabilityHasNoModifyProps 团队能力集【不含】ModifyProps。
//
// 【Peer 2026-10-04 要求的断言】这条同时锁住两件事：
//
//	(a) 能力位动态化【没有】"顺手"打开 ModifyProps；
//	(b) 若将来有人打开了它，会立刻撞上 props.go:26 那道
//	    【不可被 ctx 旁路】的 owner 门 —— 那是设计上的第二道防线，
//	    但没有断言就没人知道它在不在。
//
// 为什么这两件事必须一起锁：
//
//	§0.5 的逐位表里，ModifyProps 是唯一"门不可被 ctx 旁路"的位。
//	团队文件的 owner 恒为系统账号、写者恒非 owner，且团队路径上的所有者
//	旁路（withTeamOwnerBypass）只装在 manage/upload/lock 等路径，
//	【不覆盖 props.go:26】。
//	=> 一旦放开该位，所有团队成员改文件属性都会被 ErrOwnerOnly 拒绝，
//	   且 aclGuard（props.go:31）因 :26 先返回而不可达。
//
// 本用例是纯能力集断言（不依赖 HTTP）：PatchProps 在本版本没有对外路由，
// 只能从内部调用，故用能力集 + 门的位置来覆盖，而不是造一个 HTTP 请求。
func TestTeamCapabilityHasNoModifyProps(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "team_cap_no_modifyprops")
	mustSetAcl(t, ctx, fs, f.ID, inventory.FileAclSubjectUser, fs.user.ID, inventory.FileAclPermissionWrite, true)

	n := newTeamNav(t, fs)
	if !n.canWriteProjectID(testProjectID) {
		t.Fatalf("fixture 前提不成立：本用例需要主体可写，才能说明'即使可写也不含 ModifyProps'")
	}

	if teamNavigatorWriteCapability.Enabled(int(NavigatorCapabilityModifyProps)) {
		t.Fatalf("team capability MUST NOT contain ModifyProps: " +
			"props.go:26 的 owner 门不可被 ctx 旁路，团队写者恒非 owner，" +
			"放开该位会让所有成员改属性都被 ErrOwnerOnly 拒绝")
	}
}

// TestTeamCapabilityWriteSetContainsReadSet 写集必须【包含】全部读位。
//
// 若写位开放时读位反而关闭，可写成员会连目录都列不出来 ——
// 这是"能力集拆成两份"引入的新风险，必须显式断言。
//
// 【同时守死锁回归】本用例在无事务上下文中反复调用，
// 若将来有人在 canWriteProjectID 里重新引入"事务内查库"，
// 上层集成测试会超时，而这里仍是绿的 —— 故另有集成验证兜底。
func TestTeamCapabilityWriteSetContainsReadSet(t *testing.T) {
	ctx, fs, _, f := newAclGateFixture(t, "team_cap_write_superset")
	mustSetAcl(t, ctx, fs, f.ID, inventory.FileAclSubjectUser, fs.user.ID, inventory.FileAclPermissionWrite, true)

	n := newTeamNav(t, fs)
	if !n.canWriteProjectID(testProjectID) {
		t.Fatalf("fixture 前提不成立：应为可写")
	}

	for _, bit := range []NavigatorCapability{
		NavigatorCapabilityListChildren,
		NavigatorCapabilityDownloadFile,
		NavigatorCapabilityInfo,
		NavigatorCapabilityEnterFolder,
		NavigatorCapabilityGenerateThumb,
	} {
		if !teamNavigatorWriteCapability.Enabled(int(bit)) {
			t.Fatalf("write capability set must be a SUPERSET of read set, missing %v", bit)
		}
	}
}

// TestTeamCapabilityZeroProjectIDIsNotWritable 无项目（团队根）不给写位。
//
// 团队根不对应任何 files 行，没有 ACL 载体。
// 此处返回不可写是【刻意的】—— 与"无 ACL 放行"不矛盾：
// 前者是"没有判定对象"，后者是"有对象且无规则"。
func TestTeamCapabilityZeroProjectIDIsNotWritable(t *testing.T) {
	_, fs, _, _ := newAclGateFixture(t, "team_cap_zero_pid")

	n := newTeamNav(t, fs)
	if n.canWriteForProject() {
		t.Fatalf("团队根（projectID=0）不应给写位：它没有 ACL 载体")
	}
}

// TestTeamCapabilityGlobalAdminAlwaysWritable 全局管理员豁免（与 acl.go 口径一致）。
func TestTeamCapabilityGlobalAdminAlwaysWritable(t *testing.T) {
	_, fs, _, _ := newAclGateFixture(t, "team_cap_admin")

	// 构造一个 Admin 组用户
	adminUser := *fs.user
	g := *fs.user.Edges.Group
	// GroupPermissionIsAdmin = bit 0
	g.Permissions = boolsetOf(0)
	adminUser.Edges.Group = &g

	n := &teamNavigator{user: &adminUser, aclClient: fs.aclClient, l: fs.l}
	if !n.canWriteProjectID(testProjectID) {
		t.Fatalf("global admin MUST always be writable (acl.go 的全局管理员豁免)")
	}
}

// TestTeamCapabilityNilAclClientIsWritable ACL 客户端未注入 -> 放行。
//
// 与 acl.go:61 的回落一致。ACL 是附加能力，
// 绝不允许它把原有的文件系统操作弄挂。
func TestTeamCapabilityNilAclClientIsWritable(t *testing.T) {
	_, fs, _, _ := newAclGateFixture(t, "team_cap_nil_acl")

	n := &teamNavigator{user: fs.user, aclClient: nil, l: fs.l}
	if !n.canWriteProjectID(testProjectID) {
		t.Fatalf("nil aclClient MUST be treated as writable (acl.go contract: 未注入则放行)")
	}
}

// TestTeamCapabilityNoQueryDuringTargetSet setCapabilityTarget 必须【零查库】。
//
// 【这条是死锁的回归防线】
// 实测：若 setCapabilityTarget 内发起查询，团队上传会卡 16s 后回滚
// （getNavigator 在事务内被调用 + SQLite 单连接）。
//
// 判据：传入一个 fileClient 为 nil 的 navigator —— 若函数体里真的查了库，
// 会因 nil 解引用而 panic；不查库则安全返回。
func TestTeamCapabilityNoQueryDuringTargetSet(t *testing.T) {
	n := &teamNavigator{user: nil, aclClient: nil, l: nil, fileClient: nil}

	// 不应 panic —— 说明没有触碰 fileClient
	n.setCapabilityTarget(nil, mustParseURI(t, "cloudreve://team/2"))
	if n.capabilityProjectID != 2 {
		t.Fatalf("期望记录 projectID=2，实际 %d", n.capabilityProjectID)
	}

	// 团队根 -> 0
	n.setCapabilityTarget(nil, mustParseURI(t, "cloudreve://team"))
	if n.capabilityProjectID != 0 {
		t.Fatalf("团队根应为 0，实际 %d", n.capabilityProjectID)
	}
}

// TestTeamCapabilityInsideTransactionDoesNotDeadlock
// ★ Peer 2026-10-04 要求的回归测试：在【事务内】调 Capabilities() 不得死锁。
//
// 【为什么必须有这条】
// Peer 的裁决：Capabilities() 就在 getNavigator【内部】被调用（dbfs.go:802），
// 所以"查库是否在事务内"取决于【谁调用 getNavigator】，
// 与我"把查询写在哪一行"无关。
//
// 我此前用 context.Background() 做查询，理由是"不在任何 WithTx 之内"——
// 那句话未经查证，且与我自己上一条消息自相矛盾。
// TestTeamCapabilityNoQueryDuringTargetSet 只护住了 setCapabilityTarget，
// 护不住"canWriteProjectID 被挪回事务内"这一复发路径。
//
// 【判据】夹具的 DB 与生产一致设了 SetMaxOpenConns(1)。
// 事务内若另开连接查库，database/sql 会永久等待那条唯一连接。
// 所以本用例在带超时的 goroutine 里跑：超时即失败。
func TestTeamCapabilityInsideTransactionDoesNotDeadlock(t *testing.T) {
	ctx, fs, n := newTeamNavFixture(t, "team_cap_tx_no_deadlock")
	mustSetAcl(t, ctx, fs, lastFixtureProjRoot.ID,
		inventory.FileAclSubjectUser, fs.user.ID, inventory.FileAclPermissionWrite, true)

	done := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Capabilities 在事务内 panic: %v", r)
				done <- false
			}
		}()

		// 必须用 inventory.WithTx 开事务 —— 只有它会把 TxCtx 写进 ctx，
		// 而 InheritTx 正是靠这个值继承事务。
		// （手工 tx 而 ctx 里没有 TxCtx 的话，InheritTx 返回 nil，
		//   于是照常另开连接 —— 那样测的是别的场景，不是生产形态。）
		_, tx, txCtx, err := inventory.WithTx(ctx, fs.fileClient)
		if err != nil {
			t.Errorf("开启事务失败: %v", err)
			done <- false
			return
		}
		defer func() { _ = inventory.Rollback(tx) }()

		// 关键前提：此事务正持有那条唯一连接。
		// 若能力位判定另开连接查库 -> database/sql 永久等待 -> 本用例超时失败。
		n.setCapabilityTarget(txCtx, mustParseURI(t, "cloudreve://team/2"))
		caps := n.Capabilities(false).Capability
		done <- caps.Enabled(int(NavigatorCapabilityUploadFile))
	}()

	select {
	case ok := <-done:
		if !ok {
			t.Fatalf("事务内能力位判定失败：期望写位开放（write ACL）")
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("★ 死锁：事务持有唯一连接时调用 Capabilities() 超过 15s 未返回。\n" +
			"  原因几乎必然是 canWriteProjectID 里的查库没有继承事务 ——\n" +
			"  必须用 inventory.InheritTx(ctx, t.aclClient) 绑定当前事务\n" +
			"  （与 acl.go:106-109 同一模式），而不是用 context.Background()。")
	}
}
