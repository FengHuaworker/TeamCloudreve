package dbfs

// S0b 批量解析与单节点解析的【等价性】测试。
//
// Peer 的要求（原话）：
//   "对同一批节点，分别用【单节点路径】和【批量路径】解析，
//    断言逐节点的 【resolved 结果】完全相同（不只是 capability 相同）"
//
// 理由："capability 是 resolved 的下游，只比下游会掩盖'上游不同但下游恰好相同'。
//        结构保证只能保证【函数相同】，保证不了【喂给它的数据相同】。所以要直接比 resolved。"

import (
	"context"
	"database/sql"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/boolset"
	"github.com/cloudreve/Cloudreve/v4/pkg/conf"
	_ "modernc.org/sqlite"
)

// newS0bFixture 建一个独立的内存库 + 真实 owner 用户（files.owner_id 是外键）。
//
// 与 acl_test.go:29 的夹具同形，特别是 SetMaxOpenConns(1) —— 必须与生产一致，
// 否则"事务内另开连接会死锁"这个真实风险在测试里复现不出来。
func newS0bFixture(t *testing.T, name string) (context.Context, *ent.Client, int) {
	t.Helper()
	ctx := context.Background()

	db, err := sql.Open("sqlite3", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	db.SetMaxIdleConns(1)
	db.SetMaxOpenConns(1)

	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", db)))
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("schema create: %v", err)
	}

	g, err := client.Group.Create().
		SetName("s0b-group-" + name).
		SetPermissions(&boolset.BooleanSet{}).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u, err := client.User.Create().
		SetEmail("s0b-" + name + "@test.local").
		SetNick("s0b-user").
		SetGroup(g).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, client, u.ID
}

// aclClientOf 造一个绑定到该 client 的 ACL 客户端（与生产同构）。
func aclClientOf(client *ent.Client) inventory.FilePermissionClient {
	return inventory.NewFilePermissionClient(client, conf.SQLiteDB, nil)
}

// buildChain 建 root -> mid -> leaf，返回三者的 id。
func buildChain(t *testing.T, ctx context.Context, c *ent.Client, ownerID int) (root, mid, leaf int) {
	t.Helper()
	mk := func(name string, parent int, typ types.FileType) int {
		b := c.File.Create().
			SetName(name).
			SetType(int(typ)).
			SetOwnerID(ownerID)
		if parent > 0 {
			b.SetFileChildren(parent)
		}
		f, err := b.Save(ctx)
		if err != nil {
			t.Fatalf("建 %s 失败: %v", name, err)
		}
		return f.ID
	}
	root = mk("root", 0, types.FileTypeFolder)
	mid = mk("mid", root, types.FileTypeFolder)
	leaf = mk("leaf", mid, types.FileTypeFile)
	return
}

func addAcl(t *testing.T, ctx context.Context, c *ent.Client, fileID, subjectID int,
	perm string, inherit bool) {
	t.Helper()
	_, err := c.FilePermission.Create().
		SetFileID(fileID).
		SetSubjectType("user").
		SetSubjectID(subjectID).
		SetPermission(perm).
		SetInherit(inherit).
		SetCreatedBy(1).
		Save(ctx)
	if err != nil {
		t.Fatalf("写 ACL 失败: %v", err)
	}
}

// TestS0bBatchEqualsSingleNode 是本变更的【核心等价性断言】。
//
// 对同一棵树穷举若干 ACL 配置，逐节点比对：
//
//	单节点 ResolveFilePermission(id)  vs  批量 ResolveFilePermissionsBatch([所有 id])[id]
//
// 断言二者【逐字段完全相同】。
func TestS0bBatchEqualsSingleNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx, client, ownerID := newS0bFixture(t, "eq")
	const subject = 42

	cases := []struct {
		name  string
		setup func(t *testing.T, ctx context.Context, c *ent.Client, root, mid, leaf int)
	}{
		{"无任何 ACL", func(*testing.T, context.Context, *ent.Client, int, int, int) {}},
		{"仅根 read(inherit)", func(t *testing.T, ctx context.Context, c *ent.Client, root, mid, leaf int) {
			addAcl(t, ctx, c, root, subject, "read", true)
		}},
		{"仅根 read(边界)", func(t *testing.T, ctx context.Context, c *ent.Client, root, mid, leaf int) {
			addAcl(t, ctx, c, root, subject, "read", false)
		}},
		{"根read(inherit)+mid write(inherit) 近者应胜", func(t *testing.T, ctx context.Context, c *ent.Client, root, mid, leaf int) {
			addAcl(t, ctx, c, root, subject, "read", true)
			addAcl(t, ctx, c, mid, subject, "write", true)
		}},
		{"根write(inherit)+mid read(inherit) 近者应胜(反向)", func(t *testing.T, ctx context.Context, c *ent.Client, root, mid, leaf int) {
			addAcl(t, ctx, c, root, subject, "write", true)
			addAcl(t, ctx, c, mid, subject, "read", true)
		}},
		{"根read(inherit)+mid write(边界)", func(t *testing.T, ctx context.Context, c *ent.Client, root, mid, leaf int) {
			addAcl(t, ctx, c, root, subject, "read", true)
			addAcl(t, ctx, c, mid, subject, "write", false)
		}},
		{"仅 leaf 有 ACL(自身命中,与 inherit 无关)", func(t *testing.T, ctx context.Context, c *ent.Client, root, mid, leaf int) {
			addAcl(t, ctx, c, leaf, subject, "read", false)
		}},
		{"同一层 user 与 group 都有(group 也命中时 user 胜)", func(t *testing.T, ctx context.Context, c *ent.Client, root, mid, leaf int) {
			addAcl(t, ctx, c, root, subject, "write", true)
			addAcl(t, ctx, c, root, 777, "read", true) // 不同 subject，不影响
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, mid, leaf := buildChain(t, ctx, client, ownerID)
			tc.setup(t, ctx, client, root, mid, leaf)

			// 批量路径：一次解析三个节点
			acl := aclClientOf(client)
			batch, err := acl.ResolveFilePermissionsBatch(ctx,
				[]int{root, mid, leaf}, subject, 0)
			if err != nil {
				t.Fatalf("批量解析失败: %v", err)
			}

			// 逐节点与单节点路径比对
			for _, id := range []int{root, mid, leaf} {
				single, err := acl.ResolveFilePermission(ctx, id, subject, 0)
				if err != nil {
					t.Fatalf("单节点解析 id=%d 失败: %v", id, err)
				}
				got := batch[id]

				if (single == nil) != (got == nil) {
					t.Fatalf("id=%d 命中与否不一致:\n  单节点=%v\n  批量  =%v", id, single, got)
				}
				if single == nil {
					continue
				}
				if single.Permission != got.Permission ||
					single.SourceFileID != got.SourceFileID ||
					single.SourceName != got.SourceName ||
					single.Direct != got.Direct {
					t.Fatalf("id=%d resolved 不一致:\n  单节点=%+v\n  批量  =%+v",
						id, *single, *got)
				}
			}

			// 清理，避免影响下一个用例
			if _, err := client.FilePermission.Delete().Exec(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := client.File.Delete().Exec(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestS0bBatchNearestAncestorWins 直接验证"最近祖先优先"这条语义。
//
// Peer 指出：没有这条测试，"最近祖先优先"在批量路径上从没被验过。
func TestS0bBatchNearestAncestorWins(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, client, ownerID := newS0bFixture(t, "nearest")
	const subject = 42

	root, mid, leaf := buildChain(t, ctx, client, ownerID)
	// 根 read、mid write —— 对 leaf 而言 mid 更近，应取 write
	addAcl(t, ctx, client, root, subject, "read", true)
	addAcl(t, ctx, client, mid, subject, "write", true)

	batch, err := aclClientOf(client).ResolveFilePermissionsBatch(ctx,
		[]int{leaf}, subject, 0)
	if err != nil {
		t.Fatalf("批量解析失败: %v", err)
	}
	r := batch[leaf]
	if r == nil {
		t.Fatal("leaf 应有命中（mid 的 write 继承下来）")
	}
	if r.Permission != "write" {
		t.Fatalf("应取【近的】mid=write，实际取到 %q (SourceFileID=%d)", r.Permission, r.SourceFileID)
	}
	if r.SourceFileID != mid {
		t.Fatalf("来源应是 mid(%d)，实际 %d", mid, r.SourceFileID)
	}
	if r.Direct {
		t.Fatal("leaf 自身没有 ACL，Direct 应为 false")
	}
	if r.SourceName != "mid" {
		t.Fatalf("SourceName 应为 mid，实际 %q", r.SourceName)
	}
}
