package dbfs

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
)

// ============================================================
// 文件 ACL 判定接入点
//
// 契约：《契约补遗二-文件ACL.md》§5「生效逻辑」。
//
// 判定顺序（必须严格保持）：
//  1. 全局管理员 → 不受 ACL 限制；
//  2. 命中 ACL   → 以 ACL 为准（read / write）；
//  3. 未命中     → 【完全回落到 Cloudreve 原有判定】。
//
// ⚠️ 本文件里的每一次调用都必须是「无 ACL 时零副作用」：
// ACL 客户端未注入、文件上没有任何 ACL、解析出错 —— 三种情况一律放行，
// 由 Cloudreve 原有的判定逻辑继续接管。
// 这样「没有 ACL 的文件，行为与打补丁前一模一样」这条硬要求才成立。
// ============================================================

// ACL 访问需求
const (
	// AclRequireRead 读取类操作（列目录、读内容、下载、缩略图、遍历）
	AclRequireRead = iota
	// AclRequireWrite 写入类操作（新建、上传/覆盖、重命名、移动、删除、改元数据）
	AclRequireWrite
)

// ErrAclDenied ACL 只读了却执行写操作时的错误
var ErrAclDenied = serializer.NewError(serializer.CodeNoPermissionErr,
	"Access denied by file ACL: the file is read-only for you", nil)

// IsAclDenied 判断错误是否由文件 ACL 的只读限制产生。
//
// serializer.AppError 是值类型，且 WithError 会返回一个全新的值，
// 因此 errors.Is(err, ErrAclDenied) 永远不成立；这里按 Msg 判别。
func IsAclDenied(err error) bool {
	if err == nil {
		return false
	}
	var appErr serializer.AppError
	if !errors.As(err, &appErr) {
		return false
	}
	return appErr.Msg == ErrAclDenied.Msg
}

// aclGuard 对目标文件做一次 ACL 判定。
//
// 返回 nil 表示「ACL 没有意见」，调用方继续原有的判定流程。
// 只有明确命中一条 read 记录、而本次操作需要写权限时才返回错误。
func (f *DBFS) aclGuard(ctx context.Context, target *File, need int) error {
	if f.aclClient == nil || target == nil || target.Model == nil || f.user == nil {
		return nil
	}

	// 【2026-10-04 第 3 步】此处原有一段旁路短路：
	//
	//     if _, ok := ctx.Value(ByPassOwnerCheckCtxKey{}).(bool); ok { return nil }
	//
	// 它使得 aclGuard 在【所有设置过所有者旁路的路径】上从不生效 ——
	// 而团队写路径正是全部设置旁路的（生产 12 处设置点中 9 处在 team），
	// 于是 ACL 在团队空间等于装饰，实测已复现（只读主体写只读目录 code=0）。
	// 复核命令（勿依赖上面的数字，以输出为准）：
	//     rg -n "WithBypassOwnerCheck\(|withTeamOwnerBypass" --glob '*.go'
	//
	// 删除它的理由：该 key 的语义是「跳过所有者校验」，不是「跳过 ACL 判定」。
	// 一个布尔量承担两种语义，正是问题的根源。团队空间需要前者、绝不需要后者。
	//
	// 删除后的覆盖情况（已逐一追调用链确认，见 s0a-two-lists.md）：
	//   · team 写路径        -> ACL 正常判定（本步的目标）
	//   · 附件读取           -> 只走 AclRequireRead，而 ACL 只在写时拒绝，不受影响
	//   · 缩略图生成/清理     -> 已在下层收窄（upload.go thumbOnly / inventory 层），
	//                          不经由本判定，故无需在此豁免
	//
	// ⚠️ 注意：全局管理员豁免在下方（见「1. 全局管理员不受 ACL 限制」），
	//    那是【设计意图】，与本处删除的「内部任务旁路」不是一回事，不要一并删掉。

	// 1. 全局管理员不受 ACL 限制（与团队模块 isGlobalAdmin 的口径一致）
	if f.user.Edges.Group != nil &&
		f.user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		return nil
	}

	groupID := 0
	if f.user.Edges.Group != nil {
		groupID = f.user.Edges.Group.ID
	}

	// ⚠️ 必须参与当前已存在的事务。
	//
	// dbfs 的操作会被套在别人的事务里调用（例：PrepareUpload 先
	// inventory.WithTx 开事务，再用同一个 ctx 调 f.Create），而
	// SQLite 的连接池上限是 1（inventory/client.go: SetMaxOpenConns(1)）。
	// 此时若 ACL 查询另开一条连接，database/sql 会永久等待那条唯一的连接，
	// 整个请求连同数据库一起挂死（已实测复现：PUT /file/content 卡在
	// CommitWithStorageDiff 之前不再返回，其他请求也全部排队超时）。
	aclClient := f.aclClient
	if bound, tx := inventory.InheritTx(ctx, f.aclClient); tx != nil {
		aclClient = bound
	}

	resolved, err := aclClient.ResolveFilePermission(ctx, target.ID(), f.user.ID, groupID)
	if err != nil {
		// 解析失败（例如迁移未跑、表不存在）时放行：
		// ACL 是附加能力，绝不允许它把原有的文件系统操作弄挂。
		f.l.Warning("Failed to resolve file ACL for file %d: %s", target.ID(), err)
		return nil
	}

	// 3. 未命中 → 完全回落到 Cloudreve 原有判定
	if resolved == nil {
		return nil
	}

	// 2. 命中 ACL → 以 ACL 为准
	if need == AclRequireWrite && resolved.Permission != inventory.FileAclPermissionWrite {
		source := "自身"
		if !resolved.Direct {
			source = fmt.Sprintf("上级目录 %q(id=%d)", resolved.SourceName, resolved.SourceFileID)
		}
		return ErrAclDenied.WithError(fmt.Errorf(
			"file %d is read-only (acl from %s)", target.ID(), source))
	}

	return nil
}
