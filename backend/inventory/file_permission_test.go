package inventory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/boolset"
	"github.com/cloudreve/Cloudreve/v4/pkg/conf"
	_ "modernc.org/sqlite"
)

// ============================================================
// 文件 ACL 数据层回归测试（契约补遗二 §2/§3/§7）
//
// 用 modernc.org/sqlite 纯 Go 驱动跑内存库，不依赖 CGO。
// 覆盖：基本读写、继承（父目录 → 子文件）、子项覆盖父项、
// inherit=false 作为继承边界、删除、以及「未命中即无意见」的回落语义。
// ============================================================

// newAclTestClient 建一个内存 sqlite ent 客户端并建表
func newAclTestClient(t *testing.T, name string) (*ent.Client, context.Context) {
	t.Helper()
	ctx := context.Background()

	// 注意：DSN 刻意不加 _fk=1 —— 本测试只关心 ACL 与文件父子链，
	// 不开外键约束就不必为了满足 files.owner_id 去造完整的用户/用户组记录。
	db, err := sql.Open("sqlite3", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", db)))
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("schema create: %v", err)
	}
	return client, ctx
}

// newAclTestFile 建一个文件/文件夹记录
func newAclTestFile(t *testing.T, ctx context.Context, client *ent.Client, name string,
	ownerID int, fileType types.FileType, parent *ent.File) *ent.File {
	t.Helper()

	c := client.File.Create().
		SetName(name).
		SetOwnerID(ownerID).
		SetType(int(fileType))
	if parent != nil {
		c = c.SetFileChildren(parent.ID)
	}

	f, err := c.Save(ctx)
	if err != nil {
		t.Fatalf("create file %q: %v", name, err)
	}
	return f
}

// newAclTestUser 建一个用户组 + 一个用户。
//
// files.owner_id 是指向 users 的外键（modernc sqlite 默认开启外键约束），
// 所以造文件前必须先有真实用户行。
func newAclTestUser(t *testing.T, ctx context.Context, client *ent.Client, email string) (*ent.User, *ent.Group) {
	t.Helper()

	g, err := client.Group.Create().
		SetName("test-group-" + email).
		SetPermissions(&boolset.BooleanSet{}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	u, err := client.User.Create().
		SetEmail(email).
		SetNick("nick-" + email).
		SetGroup(g).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u, g
}

// TestFilePermissionSchema 建表验证：file_permissions 表与唯一索引确实建出来了
func TestFilePermissionSchema(t *testing.T) {
	client, ctx := newAclTestClient(t, "acl_schema_test")

	// 写入一行证明表可用
	if _, err := client.FilePermission.Create().
		SetFileID(1).SetSubjectType(FileAclSubjectUser).SetSubjectID(2).
		SetPermission(FileAclPermissionRead).SetInherit(true).SetCreatedBy(2).
		Save(ctx); err != nil {
		t.Fatalf("insert into file_permissions: %v", err)
	}

	// 唯一索引 (file_id, subject_type, subject_id) 必须存在：
	// 同一主体、同一文件重复插入裸行（绕过 Upsert）必须被拒绝。
	_, err := client.FilePermission.Create().
		SetFileID(1).SetSubjectType(FileAclSubjectUser).SetSubjectID(2).
		SetPermission(FileAclPermissionWrite).SetInherit(true).SetCreatedBy(2).
		Save(ctx)
	if err == nil {
		t.Fatalf("unique index (file_id, subject_type, subject_id) is missing: duplicate row accepted")
	}
	t.Logf("duplicate insert correctly rejected: %v", err)
}

// TestFilePermissionCRUD ACL 基本读写与删除
func TestFilePermissionCRUD(t *testing.T) {
	client, ctx := newAclTestClient(t, "acl_crud_test")
	c := NewFilePermissionClient(client, conf.SQLiteDB, nil)
	owner, _ := newAclTestUser(t, ctx, client, "crud@test.local")
	f := newAclTestFile(t, ctx, client, "a.txt", owner.ID, types.FileTypeFile, nil)

	// 初始为空
	rows, err := c.ListFilePermissions(ctx, f.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("expect empty acl list, got %d err=%v", len(rows), err)
	}

	// 写入
	row, err := c.UpsertFilePermission(ctx, &NewFilePermissionArgs{
		FileID: f.ID, SubjectType: FileAclSubjectUser, SubjectID: 7,
		Permission: FileAclPermissionRead, Inherit: true, CreatedBy: 1,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if row.Permission != FileAclPermissionRead || !row.Inherit || row.CreatedBy != 1 {
		t.Fatalf("unexpected row: %+v", row)
	}

	// 覆盖写（PUT 语义）：同一主体再写一次应更新而不是新增
	row2, err := c.UpsertFilePermission(ctx, &NewFilePermissionArgs{
		FileID: f.ID, SubjectType: FileAclSubjectUser, SubjectID: 7,
		Permission: FileAclPermissionWrite, Inherit: false, CreatedBy: 9,
	})
	if err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}
	if row2.ID != row.ID {
		t.Fatalf("upsert should overwrite the same row: id %d -> %d", row.ID, row2.ID)
	}
	if row2.Permission != FileAclPermissionWrite || row2.Inherit || row2.CreatedBy != 9 {
		t.Fatalf("overwrite did not take effect: %+v", row2)
	}
	// 行数必须仍是 1
	if rows, _ = c.ListFilePermissions(ctx, f.ID); len(rows) != 1 {
		t.Fatalf("expect 1 row after overwrite, got %d", len(rows))
	}

	// 单条读取
	got, err := c.GetFilePermission(ctx, f.ID, FileAclSubjectUser, 7)
	if err != nil || got == nil || got.ID != row.ID {
		t.Fatalf("get single: %+v err=%v", got, err)
	}
	// 不存在时返回 (nil, nil)，不是错误
	missing, err := c.GetFilePermission(ctx, f.ID, FileAclSubjectUser, 999)
	if err != nil || missing != nil {
		t.Fatalf("expect (nil,nil) for missing entry, got %+v err=%v", missing, err)
	}

	// 删除
	if err := c.DeleteFilePermission(ctx, f.ID, FileAclSubjectUser, 7); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rows, _ = c.ListFilePermissions(ctx, f.ID); len(rows) != 0 {
		t.Fatalf("expect empty after delete, got %d", len(rows))
	}
	// 幂等：再删一次不报错
	if err := c.DeleteFilePermission(ctx, f.ID, FileAclSubjectUser, 7); err != nil {
		t.Fatalf("delete should be idempotent: %v", err)
	}
	// 删除后可以重新写入（验证硬删除没有把唯一索引卡死）
	if _, err := c.UpsertFilePermission(ctx, &NewFilePermissionArgs{
		FileID: f.ID, SubjectType: FileAclSubjectUser, SubjectID: 7,
		Permission: FileAclPermissionRead, Inherit: true, CreatedBy: 1,
	}); err != nil {
		t.Fatalf("re-insert after delete: %v", err)
	}

	// 校验函数
	if !ValidFileAclSubject(FileAclSubjectUser) || !ValidFileAclSubject(FileAclSubjectGroup) {
		t.Fatal("ValidFileAclSubject rejected a legal value")
	}
	if ValidFileAclSubject("team") {
		t.Fatal("ValidFileAclSubject accepted an illegal value")
	}
	if !ValidFileAclPermission(FileAclPermissionRead) || !ValidFileAclPermission(FileAclPermissionWrite) {
		t.Fatal("ValidFileAclPermission rejected a legal value")
	}
	if ValidFileAclPermission("admin") {
		t.Fatal("ValidFileAclPermission accepted an illegal value")
	}
}

// TestFilePermissionInheritance 继承、覆盖与边界（契约 §3 + 验收 4/5/6/7）
//
// 目录结构：
//
//	/            (root)
//	└── D1       给 user 7 设 read, inherit=true
//	    └── D2   给 user 7 设 read, inherit=false
//	        ├── F1   无 ACL        → 不继承（被 D2 的边界挡住）
//	        └── F2   给 user 7 设 write → 直接命中
//	    └── F3   无 ACL            → 继承 D1 的 read
func TestFilePermissionInheritance(t *testing.T) {
	client, ctx := newAclTestClient(t, "acl_inherit_test")
	c := NewFilePermissionClient(client, conf.SQLiteDB, nil)

	owner, ownerGroup := newAclTestUser(t, ctx, client, "inherit@test.local")
	other, _ := newAclTestUser(t, ctx, client, "other@test.local")

	root := newAclTestFile(t, ctx, client, "root", owner.ID, types.FileTypeFolder, nil)
	d1 := newAclTestFile(t, ctx, client, "项目文档", owner.ID, types.FileTypeFolder, root)
	f3 := newAclTestFile(t, ctx, client, "F3.txt", owner.ID, types.FileTypeFile, d1)
	d2 := newAclTestFile(t, ctx, client, "D2", owner.ID, types.FileTypeFolder, d1)
	f1 := newAclTestFile(t, ctx, client, "F1.txt", owner.ID, types.FileTypeFile, d2)
	f2 := newAclTestFile(t, ctx, client, "F2.txt", owner.ID, types.FileTypeFile, d2)

	userID, groupID := owner.ID, ownerGroup.ID

	// --- 未设任何 ACL：必须「没有意见」，交回 Cloudreve 原有判定 ---
	for _, f := range []*ent.File{d1, d2, f1, f2, f3} {
		res, err := c.ResolveFilePermission(ctx, f.ID, userID, groupID)
		if err != nil {
			t.Fatalf("resolve %s: %v", f.Name, err)
		}
		if res != nil {
			t.Fatalf("file %q has no ACL, expect nil decision, got %+v", f.Name, res)
		}
	}

	// --- 验收 4：给文件夹 D1 设 read + inherit=true ---
	if _, err := c.UpsertFilePermission(ctx, &NewFilePermissionArgs{
		FileID: d1.ID, SubjectType: FileAclSubjectUser, SubjectID: userID,
		Permission: FileAclPermissionRead, Inherit: true, CreatedBy: 1,
	}); err != nil {
		t.Fatalf("set D1 acl: %v", err)
	}

	// D1 自身：直接命中
	res, err := c.ResolveFilePermission(ctx, d1.ID, userID, groupID)
	if err != nil || res == nil {
		t.Fatalf("D1 should be a direct hit, got %+v err=%v", res, err)
	}
	if !res.Direct || res.Permission != FileAclPermissionRead || res.SourceFileID != d1.ID {
		t.Fatalf("D1 direct hit wrong: %+v", res)
	}

	// F3（D1 下的未设 ACL 文件）：继承 read，inherited_from = 项目文档
	res, err = c.ResolveFilePermission(ctx, f3.ID, userID, groupID)
	if err != nil || res == nil {
		t.Fatalf("F3 should inherit from D1, got %+v err=%v", res, err)
	}
	if res.Direct {
		t.Fatalf("F3 should be inherited, not direct: %+v", res)
	}
	if res.Permission != FileAclPermissionRead || res.SourceFileID != d1.ID || res.SourceName != "项目文档" {
		t.Fatalf("F3 inherited decision wrong: %+v", res)
	}

	// --- 验收 5：子项覆盖父项 —— F2 上设 write，D1 上有 read ---
	if _, err := c.UpsertFilePermission(ctx, &NewFilePermissionArgs{
		FileID: f2.ID, SubjectType: FileAclSubjectUser, SubjectID: userID,
		Permission: FileAclPermissionWrite, Inherit: true, CreatedBy: 1,
	}); err != nil {
		t.Fatalf("set F2 acl: %v", err)
	}
	res, err = c.ResolveFilePermission(ctx, f2.ID, userID, groupID)
	if err != nil || res == nil {
		t.Fatalf("F2 should hit its own acl, got %+v err=%v", res, err)
	}
	if !res.Direct || res.Permission != FileAclPermissionWrite || res.SourceFileID != f2.ID {
		t.Fatalf("child should override ancestor: %+v", res)
	}

	// --- 验收 6/7：inherit=false 是继承边界 ---
	// D2 上设 read + inherit=false
	if _, err := c.UpsertFilePermission(ctx, &NewFilePermissionArgs{
		FileID: d2.ID, SubjectType: FileAclSubjectUser, SubjectID: userID,
		Permission: FileAclPermissionRead, Inherit: false, CreatedBy: 1,
	}); err != nil {
		t.Fatalf("set D2 boundary acl: %v", err)
	}

	// D2 自身仍然生效
	res, err = c.ResolveFilePermission(ctx, d2.ID, userID, groupID)
	if err != nil || res == nil || !res.Direct || res.Permission != FileAclPermissionRead {
		t.Fatalf("D2 own acl should still apply: %+v err=%v", res, err)
	}

	// F1（D2 下、自身无 ACL）：不得继承 D1 的 read —— 被 D2 的边界挡住
	res, err = c.ResolveFilePermission(ctx, f1.ID, userID, groupID)
	if err != nil {
		t.Fatalf("resolve F1: %v", err)
	}
	if res != nil {
		t.Fatalf("F1 must NOT inherit across an inherit=false boundary, got %+v", res)
	}

	// F2 有自己的 write，仍然以自身为准（边界不影响直接命中）
	res, _ = c.ResolveFilePermission(ctx, f2.ID, userID, groupID)
	if res == nil || !res.Direct || res.Permission != FileAclPermissionWrite {
		t.Fatalf("F2 direct hit must survive the boundary: %+v", res)
	}

	// --- 主体匹配：别的用户不受影响 ---
	if res, _ := c.ResolveFilePermission(ctx, f3.ID, other.ID, groupID); res != nil {
		t.Fatalf("unrelated user must not match, got %+v", res)
	}

	// --- 用户组主体 ---
	if _, err := c.UpsertFilePermission(ctx, &NewFilePermissionArgs{
		FileID: root.ID, SubjectType: FileAclSubjectGroup, SubjectID: ownerGroup.ID,
		Permission: FileAclPermissionWrite, Inherit: true, CreatedBy: 1,
	}); err != nil {
		t.Fatalf("set group acl: %v", err)
	}
	// user 不属于该组 → 仍然只命中 D1 的 read
	res, _ = c.ResolveFilePermission(ctx, d1.ID, userID, 0)
	if res == nil || res.Permission != FileAclPermissionRead {
		t.Fatalf("group acl leaked to a non-member: %+v", res)
	}
	// user 属于该组 → F3 应当先命中 D1（更近），而不是 root 的组权限
	res, _ = c.ResolveFilePermission(ctx, f3.ID, userID, ownerGroup.ID)
	if res == nil || res.Permission != FileAclPermissionRead || res.SourceFileID != d1.ID {
		t.Fatalf("nearest match must win over group acl on root: %+v", res)
	}
	// root 自身：命中组权限
	res, _ = c.ResolveFilePermission(ctx, root.ID, userID, ownerGroup.ID)
	if res == nil || !res.Direct || res.Permission != FileAclPermissionWrite {
		t.Fatalf("group acl on root should hit: %+v", res)
	}

	// --- 同一文件上 user 记录优先于 group 记录 ---
	if _, err := c.UpsertFilePermission(ctx, &NewFilePermissionArgs{
		FileID: f3.ID, SubjectType: FileAclSubjectUser, SubjectID: userID,
		Permission: FileAclPermissionWrite, Inherit: true, CreatedBy: 1,
	}); err != nil {
		t.Fatalf("set F3 user acl: %v", err)
	}
	res, _ = c.ResolveFilePermission(ctx, f3.ID, userID, ownerGroup.ID)
	if res == nil || !res.Direct || res.Permission != FileAclPermissionWrite {
		t.Fatalf("user entry must win over group entry on the same file: %+v", res)
	}

	// --- 删除后回到未命中 ---
	if err := c.DeleteFilePermission(ctx, f3.ID, FileAclSubjectUser, userID); err != nil {
		t.Fatalf("delete F3 acl: %v", err)
	}
	res, _ = c.ResolveFilePermission(ctx, f3.ID, userID, groupID)
	if res == nil || res.Direct || res.SourceFileID != d1.ID {
		t.Fatalf("after delete F3 must fall back to D1 inheritance: %+v", res)
	}

	// --- 脏数据保护：父子成环时不得死循环（深度上限 64） ---
	loopA := newAclTestFile(t, ctx, client, "loopA", owner.ID, types.FileTypeFolder, nil)
	loopB := newAclTestFile(t, ctx, client, "loopB", owner.ID, types.FileTypeFolder, loopA)
	// 人为制造 loopA -> loopB 的反向指针
	if err := client.File.UpdateOneID(loopA.ID).SetFileChildren(loopB.ID).Exec(ctx); err != nil {
		t.Fatalf("make cycle: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := c.ResolveFilePermission(ctx, loopA.ID, userID, groupID); err != nil {
			t.Errorf("resolve cycle: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("ResolveFilePermission did not terminate on a parent cycle")
	}
}
