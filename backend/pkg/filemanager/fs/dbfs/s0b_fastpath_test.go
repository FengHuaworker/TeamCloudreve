package dbfs

// S0b 快路径与节点级能力的验收测试。
//
// === Peer 要求的三个用例 ===
//   A. 项目无任何 ACL          -> 断言：批量查询【未被调用】；capability 与 S0a 一致
//   B. 项目有【项目根】ACL      -> 断言：走批量路径；capability 与 S0a 一致（等价性）
//   C. ★ 项目【只有一条子级 ACL】（项目根无 ACL）
//                              -> 断言：走【批量】路径（不是快路径）
//                              -> 断言：子节点 capability == 只读集
//                              -> 断言：项目根 capability == 可写集
//
// C 是反例用例：没有它，"ACL 行数 <= 1" 那个错误判据测不出来。
//
// === Peer 要求的局部性断言 ===
//   ACL 在子级 -> 子节点变只读，但【项目根仍可写】
//   没有这一条，一个退化的实现（"项目里有子级 ACL 就全项目只读"）会假通过。

import (
	"context"
	"testing"
	"time"

	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/boolset"
	"github.com/cloudreve/Cloudreve/v4/pkg/conf"
)

// countingAclClient 包装一个 ACL 客户端，记录批量解析被调用的次数。
//
// 用途：让"快路径真的被绕开了"成为【可观测的断言】，而不是读代码声称。
type countingAclClient struct {
	inventory.FilePermissionClient
	batchCalls   int
	anyExistsHit int
}

func (c *countingAclClient) ResolveFilePermissionsBatch(ctx context.Context,
	fileIDs []int, userID, groupID int) (map[int]*inventory.ResolvedFilePermission, error) {
	c.batchCalls++
	return c.FilePermissionClient.ResolveFilePermissionsBatch(ctx, fileIDs, userID, groupID)
}

func (c *countingAclClient) AnyFilePermissionExists(ctx context.Context) (bool, error) {
	c.anyExistsHit++
	return c.FilePermissionClient.AnyFilePermissionExists(ctx)
}

// newS0bNav 造一个 teamNavigator，只带能力位判定需要的字段。
//
// 判定走 applyNodeCapabilities（它不依赖 fileClient / owner 解析，
// 只用 aclClient + user + l），因此这里不需要完整夹具。
func newS0bNav(t *testing.T, ctx context.Context, client *ent.Client, userID, groupID int) (*teamNavigator, *countingAclClient) {
	t.Helper()
	perms := &boolset.BooleanSet{} // 非管理员
	u := &ent.User{ID: userID}
	u.Edges.Group = &ent.Group{ID: groupID, Permissions: perms}

	inner := inventory.NewFilePermissionClient(client, conf.SQLiteDB, nil)
	counting := &countingAclClient{FilePermissionClient: inner}
	return &teamNavigator{
		user:      u,
		aclClient: counting,
		l:         nil,
	}, counting
}

// makeNode 造一个"节点 + 其数据库行"的配对（applyNodeCapabilities 需要两者对应）。
func makeNode(t *testing.T, ctx context.Context, c *ent.Client, ownerID int, name string, typ types.FileType) (*File, *ent.File) {
	t.Helper()
	m, err := c.File.Create().
		SetName(name).
		SetType(int(typ)).
		SetOwnerID(ownerID).
		Save(ctx)
	if err != nil {
		t.Fatalf("建 %s 失败: %v", name, err)
	}
	f := newFile(nil, m)
	return f, m
}

// capStr 把能力位渲染成可比较的字符串（用于"逐字节一致"的断言）。
func capStr(bs *boolset.BooleanSet) string {
	if bs == nil {
		return "<nil>"
	}
	s, err := bs.String()
	if err != nil {
		return "<err>"
	}
	return s
}

// TestS0bFastPath_A_NoAclSkipsBatch 用例 A。
func TestS0bFastPath_A_NoAclSkipsBatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, client, ownerID := newS0bFixture(t, "s0bA")

	nav, counting := newS0bNav(t, ctx, client, ownerID, 1)
	f, m := makeNode(t, ctx, client, ownerID, "a.bin", types.FileTypeFile)

	if err := nav.applyNodeCapabilities(ctx, []*File{f}, []*ent.File{m}, nil); err != nil {
		t.Fatalf("applyNodeCapabilities: %v", err)
	}

	if counting.batchCalls != 0 {
		t.Fatalf("无 ACL 时应走快路径，但批量解析被调用 %d 次", counting.batchCalls)
	}
	if counting.anyExistsHit == 0 {
		t.Fatal("快路径判据（AnyFilePermissionExists）应被调用过")
	}
	if got := capStr(f.CapabilitiesBs); got != capStr(teamNavigatorWriteCapability) {
		t.Fatalf("无 ACL 应回落可写集，实际 %q", got)
	}
}

// TestS0bFastPath_C_SingleChildAclMustNotTakeFastPath ★ 反例用例。
//
// 这是"ACL 行数 <= 1 -> 走快路径"那个错误判据的边界：
// 项目只有【一条】ACL，但它挂在子文件夹上。
func TestS0bFastPath_C_SingleChildAclMustNotTakeFastPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, client, ownerID := newS0bFixture(t, "s0bC")
	const subject = 42

	// 造 projectRoot -> child
	projectRoot, err := client.File.Create().
		SetName("2"). // projectRootName(2)
		SetType(int(types.FileTypeFolder)).
		SetOwnerID(ownerID).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	child, err := client.File.Create().
		SetName("docs").
		SetType(int(types.FileTypeFolder)).
		SetFileChildren(projectRoot.ID).
		SetOwnerID(ownerID).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// ★ 只在这一条子级上设 ACL，项目根【不设】
	addAcl(t, ctx, client, child.ID, subject, "read", true)

	nav, counting := newS0bNav(t, ctx, client, subject, 1)

	// 同时把项目根与子节点交给批量赋值（模拟"列出该项目内容"）
	rootFile := newFile(nil, projectRoot)
	childFile := newFile(nil, child)

	if err := nav.applyNodeCapabilities(ctx,
		[]*File{rootFile, childFile},
		[]*ent.File{projectRoot, child}, nil); err != nil {
		t.Fatalf("applyNodeCapabilities: %v", err)
	}

	// 断言 1：必须走【批量】路径，不能被快路径误判
	if counting.batchCalls == 0 {
		t.Fatal("★ 有 1 条【子级】 ACL 时必须走批量路径，但它走了快路径 —— " +
			"这正是 'ACL 行数<=1' 那个错误判据的产物")
	}

	// 断言 2：子节点为只读
	if got, want := capStr(childFile.CapabilitiesBs), capStr(teamNavigatorCapability); got != want {
		t.Fatalf("子节点 docs 有 read ACL，应为只读集：got=%q want=%q", got, want)
	}

	// 断言 3：★ 局部性 —— 项目根无 ACL，仍应为可写集
	if got, want := capStr(rootFile.CapabilitiesBs), capStr(teamNavigatorWriteCapability); got != want {
		t.Fatalf("★ 局部性失败：项目根没有 ACL，应仍可写。got=%q want=%q\n"+
			"（若这里是只读集，说明实现退化成'有子级 ACL 就整个项目只读'）", got, want)
	}

	// 断言 4：同一项目内两个节点拿到了不同能力位 —— S0b 的验收核心
	if capStr(rootFile.CapabilitiesBs) == capStr(childFile.CapabilitiesBs) {
		t.Fatal("★ 同一项目内根与子节点能力位相同 —— 节点级判定未生效（仍是项目级）")
	}
}

// TestS0bFastPath_B_RootAclEquivalentToS0a 用例 B：项目根 ACL 的行为与 S0a 一致。
func TestS0bFastPath_B_RootAclEquivalentToS0a(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, client, ownerID := newS0bFixture(t, "s0bB")
	const subject = 42

	projectRoot, err := client.File.Create().
		SetName("3").
		SetType(int(types.FileTypeFolder)).
		SetOwnerID(ownerID).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	child, err := client.File.Create().
		SetName("docs").
		SetType(int(types.FileTypeFolder)).
		SetFileChildren(projectRoot.ID).
		SetOwnerID(ownerID).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 只设【项目根】read，inherit=true -> 应向下继承到 child
	addAcl(t, ctx, client, projectRoot.ID, subject, "read", true)

	nav, counting := newS0bNav(t, ctx, client, subject, 1)
	rootFile := newFile(nil, projectRoot)
	childFile := newFile(nil, child)

	if err := nav.applyNodeCapabilities(ctx,
		[]*File{rootFile, childFile},
		[]*ent.File{projectRoot, child}, nil); err != nil {
		t.Fatalf("applyNodeCapabilities: %v", err)
	}

	// 根 ACL inherit=true -> 二者都应只读（与 S0a 的项目级判定结果一致）
	if got := capStr(rootFile.CapabilitiesBs); got != capStr(teamNavigatorCapability) {
		t.Fatalf("项目根有 read ACL，应只读，实际 %q", got)
	}
	if got := capStr(childFile.CapabilitiesBs); got != capStr(teamNavigatorCapability) {
		t.Fatalf("根 ACL inherit=true 应继承到子节点，子节点应只读，实际 %q", got)
	}
	// 有 ACL -> 必然走批量路径
	if counting.batchCalls == 0 {
		t.Fatal("有 ACL 时应走批量路径")
	}
}

// TestS0bFastPath_BoundaryStopsInheritance 边界（inherit=false）下方的节点不受更上层影响。
func TestS0bFastPath_BoundaryStopsInheritance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, client, ownerID := newS0bFixture(t, "s0bBoundary")
	const subject = 42

	top, _ := client.File.Create().SetName("top").SetType(int(types.FileTypeFolder)).
		SetOwnerID(ownerID).Save(ctx)
	// boundary 的 ACL：write 但 inherit=false -> 它自己是 write，且截断上层
	boundary, _ := client.File.Create().SetName("proj").SetType(int(types.FileTypeFolder)).
		SetFileChildren(top.ID).SetOwnerID(ownerID).Save(ctx)
	inner, _ := client.File.Create().SetName("inner").SetType(int(types.FileTypeFolder)).
		SetFileChildren(boundary.ID).SetOwnerID(ownerID).Save(ctx)

	// 最上层 read(inherit=true) 本会继承下来，但 boundary 处 inherit=false 截断它
	addAcl(t, ctx, client, top.ID, subject, "read", true)
	addAcl(t, ctx, client, boundary.ID, subject, "write", false)

	nav, _ := newS0bNav(t, ctx, client, subject, 1)
	innerFile := newFile(nil, inner)
	if err := nav.applyNodeCapabilities(ctx, []*File{innerFile}, []*ent.File{inner}, nil); err != nil {
		t.Fatalf("applyNodeCapabilities: %v", err)
	}

	// inner 自身无 ACL；boundary 的 inherit=false 是边界 -> 不再向上取 top 的 read
	// => 无命中 -> 回落放行（可写）
	if got := capStr(innerFile.CapabilitiesBs); got != capStr(teamNavigatorWriteCapability) {
		t.Fatalf("inherit=false 应截断上层 read，inner 应回落可写，实际 %q", got)
	}
}
