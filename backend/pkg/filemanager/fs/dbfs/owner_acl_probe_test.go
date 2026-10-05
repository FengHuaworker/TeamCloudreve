package dbfs

import (
	"testing"

	"github.com/cloudreve/Cloudreve/v4/inventory"
)

// TestOwnerResolvesReadAclOnOwnFile —— 回答 Peer 的 ③ 质疑（决定性判定）。
//
// Peer 读代码的判断：aclGuard 只有【全局管理员】豁免（acl.go:88-91），
// 【没有】owner 豁免。所以按代码，owner 在自己文件上遇到 read ACL 也会被拒。
//
// 若成立，我上一轮那条 "owner 不受 ACL 约束 -> owner rename code=0"
// 就是【空断言】—— 当时那个文件上根本没有 ACL。
//
// 本用例直接在单测里判定，不经过 HTTP，排除 uri 解析与路径归属的干扰。
func TestOwnerResolvesReadAclOnOwnFile(t *testing.T) {
	ctx, fs, user, entFile := newAclGateFixture(t, "owner_acl_probe")
	target := &File{Model: entFile}

	// ACL 主体 = 文件所有者【自己】，permission=read
	mustSetAcl(t, ctx, fs, entFile.ID,
		inventory.FileAclSubjectUser, user.ID, inventory.FileAclPermissionRead, true)

	resolved, err := fs.aclClient.ResolveFilePermission(ctx, entFile.ID, user.ID, 0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved == nil {
		t.Fatalf("owner 自己的 read ACL 没被解析到 -> 主体匹配有问题")
	}
	t.Logf("resolved = {perm=%s direct=%v}", resolved.Permission, resolved.Direct)

	// 关键判定：aclGuard 对 owner 的写操作应【拒绝】（代码里无 owner 豁免）
	err = fs.aclGuard(ctx, target, AclRequireWrite)
	t.Logf("aclGuard(owner, Write) = %v", err)

	switch {
	case err == nil:
		t.Errorf("aclGuard 未拒绝 owner 的写 —— 与 acl.go（无 owner 豁免）矛盾，需要解释")
	case !IsAclDenied(err):
		t.Errorf("拒绝原因不是 ACL：%v", err)
	default:
		t.Logf("=> aclGuard 【无 owner 豁免】，Peer 读代码的判断正确")
	}
}

// TestOwnerWriteAllowedWhenNoAcl —— 对照：无 ACL 时 owner 写应放行。
//
// 这一条才是上一轮我实际测到的东西（当时误写成了"owner 不受 ACL 约束"）。
func TestOwnerWriteAllowedWhenNoAcl(t *testing.T) {
	ctx, fs, _, entFile := newAclGateFixture(t, "owner_no_acl")
	target := &File{Model: entFile}

	resolved, err := fs.aclClient.ResolveFilePermission(ctx, entFile.ID, fs.user.ID, 0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved != nil {
		t.Fatalf("无 ACL 时应返回 nil，实际 %+v", *resolved)
	}

	if err := fs.aclGuard(ctx, target, AclRequireWrite); err != nil {
		t.Errorf("无 ACL 时 owner 写被拒：%v", err)
	}
}
