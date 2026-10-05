package explorer

// ★ S0b 缺失的断言：序列化层不得覆盖节点自己的能力位。
//
// === 为什么这个文件必须存在 ===
//
// S0b 的单元测试全绿 —— 批量路径与单节点路径逐字段一致、快路径正确、
// 局部性成立、反向验证也确实能失败。**但端到端 capability 一点没变。**
//
// 原因不在我改的代码里，而在【我没追到的一层】：
//
//   BuildListResponse:388  BuildFileResponse(ctx, u, f, hasher, res.Props.Capability)
//   BuildListResponse:401  BuildFileResponse(ctx, u, res.Parent, hasher, res.Props.Capability)
//   BuildFileResponse:414    if cap == nil { cap = f.Capabilities() }   <- 唯一出口
//   BuildFileResponse:427    Capability: cap
//
// 调用方【显式传入】导航器级（= 项目级）的 capability，
// 于是节点自己算出的 CapabilitiesBs 被覆盖 —— **算得再对也送不到调用方**。
//
// 我原来的验收只覆盖了【产生端】（我算的值对不对），
// 没有任何一条覆盖【送达端】（这个值有没有真的到达调用方）。
//
// 本文件补的就是送达端。它与产生端断言的关系：
//   产生端对 + 送达端坏  =>  端到端不变（S0b 实际发生的）
//   产生端对 + 送达端对  =>  端到端才变
// 只测产生端会给出【假绿】。这就是本次的教训。

import (
	"context"
	"testing"
	"time"

	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/boolset"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
)

// fakeNode 是一个只实现断言所需方法的 fs.File。
//
// 不引真实 DB：本断言要验的是【序列化层选哪个值】，
// 与文件怎么存的无关；用假对象反而让失败信息更干净。
type fakeNode struct {
	fs.File // 其余方法不需要，嵌 nil 接口即可
	id      int
	name    string
	typ     int
	capBs   *boolset.BooleanSet
}

func (f *fakeNode) ID() int                 { return f.id }
func (f *fakeNode) DisplayName() string     { return f.name }
func (f *fakeNode) Type() types.FileType    { return types.FileType(f.typ) }
func (f *fakeNode) Capabilities() *boolset.BooleanSet { return f.capBs }
func (f *fakeNode) Owner() *ent.User        { return nil }
func (f *fakeNode) CreatedAt() time.Time    { return time.Time{} }
func (f *fakeNode) UpdatedAt() time.Time    { return time.Time{} }
func (f *fakeNode) Size() int64             { return 0 }
func (f *fakeNode) Metadata() map[string]string { return nil }
func (f *fakeNode) ExtendedInfo() *fs.FileExtendedInfo {
	// 返回 nil 让 BuildExtendedInfo 在 response.go:438 提前返回 ——
	// 本断言只关心 Capability，不需要存储策略/直链/分享那一路。
	// 也因此不必依赖 dependency.FromContext（假对象不需要 DI 容器）。
	return nil
}
func (f *fakeNode) Entities() []fs.Entity { return nil }
func (f *fakeNode) Uri(bool) *fs.URI {
	// 必须返回内部非 nil 的 URI：BuildFileResponse:425 会调 Uri(false).String()，
	// 而 fs.URI.String() 内部解引用 u.U（uri.go:112）。
	// 返回零值会 panic —— 那样测到的就不是"能力位选错"，而是我的假对象造得不对。
	// 用官方构造器而不是直接拼结构体，避免字段名猜错。
	u, err := fs.NewUriFromString("cloudreve://my/" + f.name)
	if err != nil {
		panic(err)
	}
	return u
}
func (f *fakeNode) Shared() bool            { return false }
func (f *fakeNode) FolderSummary() *fs.FolderSummary { return nil }
func (f *fakeNode) PrimaryEntityID() int    { return 0 }
func (f *fakeNode) CapabilitiesBs() *boolset.BooleanSet { return f.capBs }

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

func nodeCap(readonly bool) *boolset.BooleanSet {
	// 只读集 gAaI 与可写集 w8aI 的差异点足够做本断言：
	// 用 DownloadFile 之外的一个位构造两个不同集合即可。
	write := &boolset.BooleanSet{}
	boolset.Set(7, true, write)  // DownloadFile
	boolset.Set(19, true, write) // Info
	boolset.Set(23, true, write) // EnterFolder

	ro := &boolset.BooleanSet{}
	boolset.Set(7, true, ro)
	boolset.Set(19, true, ro)
	if readonly {
		return ro
	}
	return write
}

func testHasher() hashid.Encoder {
	e, err := hashid.New("s0b-test-salt")
	if err != nil {
		panic(err)
	}
	return e
}

// TestS0bSerializerMustNotOverrideNodeCapability 核心断言。
//
// 场景：节点自己的能力位与调用方传入的 cap 【不同】。
// 断言：出口用的是【节点的】。
//
// ★ 在修复 response.go 之前，这条必然失败。
func TestS0bSerializerMustNotOverrideNodeCapability(t *testing.T) {
	ctx := context.Background()

	own := nodeCap(true)     // 节点自己：只读
	caller := nodeCap(false) // 调用方传入：可写

	f := &fakeNode{id: 1, name: "readonly-dir", typ: int(types.FileTypeFolder), capBs: own}
	u := &ent.User{ID: 2}
	u.Edges.Group = &ent.Group{ID: 1, Permissions: &boolset.BooleanSet{}}

	got := BuildFileResponse(ctx, u, f, testHasher(), caller)
	if got == nil {
		t.Fatal("BuildFileResponse 返回 nil")
	}

	gotCap := capStr(got.Capability)
	if gotCap != capStr(own) {
		t.Fatalf("★ 序列化层覆盖了节点能力位：\n"+
			"   节点自己的 capability  = %q\n"+
			"   调用方传入的 capability = %q\n"+
			"   响应实际输出的         = %q\n"+
			"   => 节点级判定【算对了但送不到调用方】，端到端完全不可见。",
			capStr(own), capStr(caller), gotCap)
	}
}

// TestS0bSerializerFallsBackWhenNodeHasNoCapability 回落断言。
//
// 节点自己没有能力位时必须回落到入参 —— 否则会弄坏 my/share/trash 等
// 【依赖入参】的 navigator。没有这一条，一个"干脆忽略入参"的修复会假通过。
func TestS0bSerializerFallsBackWhenNodeHasNoCapability(t *testing.T) {
	ctx := context.Background()
	caller := nodeCap(false)

	f := &fakeNode{id: 1, name: "plain", typ: int(types.FileTypeFolder), capBs: nil}
	u := &ent.User{ID: 2}
	u.Edges.Group = &ent.Group{ID: 1, Permissions: &boolset.BooleanSet{}}

	got := BuildFileResponse(ctx, u, f, testHasher(), caller)
	if got == nil {
		t.Fatal("BuildFileResponse 返回 nil")
	}
	if s := capStr(got.Capability); s != capStr(caller) {
		t.Fatalf("节点无能力位时应回落到入参 %q，实际 %q", capStr(caller), s)
	}
}

// TestS0bSerializerNilBothSides 两侧都没有时不能 panic（保持上游原行为）。
func TestS0bSerializerNilBothSides(t *testing.T) {
	ctx := context.Background()
	f := &fakeNode{id: 1, name: "bare", typ: int(types.FileTypeFile), capBs: nil}
	u := &ent.User{ID: 2}
	u.Edges.Group = &ent.Group{ID: 1, Permissions: &boolset.BooleanSet{}}

	got := BuildFileResponse(ctx, u, f, testHasher(), nil)
	if got == nil {
		t.Fatal("BuildFileResponse 返回 nil")
	}
	_ = got.Capability // 允许 nil
}
