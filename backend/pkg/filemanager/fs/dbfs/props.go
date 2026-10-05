package dbfs

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/samber/lo"
)

func (f *DBFS) PatchProps(ctx context.Context, uri *fs.URI, props *types.FileProps, delete bool) error {
	navigator, err := f.getNavigator(ctx, uri, NavigatorCapabilityModifyProps, NavigatorCapabilityLockFile)
	if err != nil {
		return err
	}

	target, err := f.getFileByPath(ctx, navigator, uri)
	if err != nil {
		return fmt.Errorf("failed to get target file: %w", err)
	}

	if target.OwnerID() != f.user.ID && !f.user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		return fs.ErrOwnerOnly.WithError(fmt.Errorf("only file owner can modify file props"))
	}

	// 文件 ACL 判定（写入）：改文件属性属于写操作
	if err := f.aclGuard(ctx, target, AclRequireWrite); err != nil {
		return err
	}

	// Lock target
	lr := &LockByPath{target.Uri(true), target, target.Type(), ""}
	ls, err := f.acquireByPath(ctx, -1, f.user, true, fs.LockApp(fs.ApplicationUpdateMetadata), lr)
	defer func() { _ = f.Release(ctx, ls) }()
	if err != nil {
		return err
	}

	currentProps := target.Model.Props
	if currentProps == nil {
		currentProps = &types.FileProps{}
	}

	if props.View != nil {
		if delete {
			currentProps.View = nil
		} else {
			currentProps.View = props.View
		}
	}

	if _, err := f.fileClient.UpdateProps(ctx, target.Model, currentProps); err != nil {
		return serializer.NewError(serializer.CodeDBError, "failed to update file props", err)
	}

	return nil
}

func (f *DBFS) PatchMetadata(ctx context.Context, path []*fs.URI, metas ...fs.MetadataPatch) error {
	ae := serializer.NewAggregateError()
	targets := make([]*File, 0, len(path))
	for _, p := range path {
		navigator, err := f.getNavigator(ctx, p, NavigatorCapabilityUpdateMetadata, NavigatorCapabilityLockFile)
		if err != nil {
			ae.Add(p.String(), err)
			continue
		}

		target, err := f.getFileByPath(ctx, navigator, p)
		if err != nil {
			ae.Add(p.String(), fmt.Errorf("failed to get target file: %w", err))
			continue
		}

		// Require Update permission
		if _, ok := ctx.Value(ByPassOwnerCheckCtxKey{}).(bool); !ok && target.OwnerID() != f.user.ID {
			return fs.ErrOwnerOnly.WithError(fmt.Errorf("permission denied"))
		}

		// 文件 ACL 判定（写入）：改元数据属于写操作
		if err := f.aclGuard(ctx, target, AclRequireWrite); err != nil {
			ae.Add(p.String(), err)
			continue
		}

		if target.IsRootFolder() {
			ae.Add(p.String(), fs.ErrNotSupportedAction.WithError(fmt.Errorf("cannot move root folder")))
			continue
		}

		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return ae.Aggregate()
	}

	// Lock all targets
	lockTargets := lo.Map(targets, func(value *File, key int) *LockByPath {
		return &LockByPath{value.Uri(true), value, value.Type(), ""}
	})
	ls, err := f.acquireByPath(ctx, -1, f.user, true, fs.LockApp(fs.ApplicationUpdateMetadata), lockTargets...)
	defer func() { _ = f.Release(ctx, ls) }()
	if err != nil {
		return err
	}

	metadataMap := make(map[string]string)
	privateMap := make(map[string]bool)
	deleted := make([]string, 0)
	updateModifiedAt := false
	for _, meta := range metas {
		if meta.Remove {
			deleted = append(deleted, meta.Key)
			continue
		}
		metadataMap[meta.Key] = meta.Value
		if meta.Private {
			privateMap[meta.Key] = meta.Private
		}
		if meta.UpdateModifiedAt {
			updateModifiedAt = true
		}
	}

	fc, tx, ctx, err := inventory.WithTx(ctx, f.fileClient)
	if err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to start transaction", err)
	}

	for _, target := range targets {
		if err := fc.UpsertMetadata(ctx, target.Model, metadataMap, privateMap); err != nil {
			_ = inventory.Rollback(tx)
			return fmt.Errorf("failed to upsert metadata: %w", err)
		}

		if len(deleted) > 0 {
			if err := fc.RemoveMetadata(ctx, target.Model, deleted...); err != nil {
				_ = inventory.Rollback(tx)
				return fmt.Errorf("failed to remove metadata: %w", err)
			}
		}

		if updateModifiedAt {
			if err := fc.UpdateModifiedAt(ctx, target.Model, time.Now()); err != nil {
				_ = inventory.Rollback(tx)
				return fmt.Errorf("failed to update file modified at: %w", err)
			}
		}
	}

	if err := inventory.Commit(tx); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to commit metadata change", err)
	}

	return ae.Aggregate()
}

// SetThumbDisabled 标记/取消标记「该文件的缩略图不可用」。
//
// 【2026-10-04 第 3 步】为什么需要这个窄入口，而不是让调用方用 PatchMetadata：
//
//	缩略图标记是【服务端派生状态】，在只读文件上也必须可写
//	（thumbnail.go:197 的注释：「Generating thumb can be triggered by users with
//	read-only permission」）。但 PatchMetadata 是用户语义的「改元数据」入口，
//	其 key/value 由调用方任意控制，必须受文件 ACL 约束
//	（见 PatchMetadata 内的 aclGuard(AclRequireWrite)）。
//
//	若为了放行缩略图而给 PatchMetadata 开旁路，等于让只读用户可改写任意元数据 ——
//	方向错误。故改为在此提供【只写一个固定 key】的入口，天然不可滥用。
//
// 实现刻意不经由 PatchMetadata：
//   - 不做 ACL 判定（本操作非用户语义的写）
//   - 不做所有者校验（同上）
//   - 不获取文件锁（与 manage.go:248 清除该标记的既有做法保持一致，
//     那里直接使用 inventory 客户端，同样不加锁）
//
// 调用方只有 manager/thumbnail.go 的 disableThumb。
// SetThumbDisabled 写入/清除「缩略图已禁用」标记。
//
// 【为什么这里叫"窄"，以及"窄"靠什么保证 —— Peer 2026-10-04 问】
//
//	问题：什么保证了本方法只能写 ThumbDisabledKey，不能写别的 key？
//	      是结构上（签名里没有 key 参数），还是运行上（需要测试）？
//
//	答：【结构上】。签名是 SetThumbDisabled(ctx, uri, disabled bool)，
//	  没有 key 参数；ThumbDisabledKey 在下面两个分支里【硬编码】。
//	  调用方无法传入别的 key。
//
//	这比加测试【更强】，理由是输入面而非覆盖面：
//	  · 测试只能覆盖【已知的】坏输入，未知的绕过方式测不到；
//	  · 签名直接把"传入别的 key"这个【可能性】消灭了 ——
//	    不存在这样一个输入，也就不存在需要覆盖的用例。
//	  一条测试证明"某个坏输入会失败"，一个签名证明"没有坏输入"。
//
//	【Peer 2026-10-04 评语，一并记下】
//	  这是本步里唯一一处"结构上不可能"而非"测过没问题"的保证。
//	  它是这个项目里少见的、不需要守卫的性质。
//
//	⚠️ 修改本函数时请一并复核这句话：一旦给签名加上 key 参数，
//	  本方法就【不再是】窄的，"ACL 收窄"的依据随之失效。
//
// 【为什么不检查写能力位/ACL】
//	本方法是服务端内部状态写入（派生数据），不是用户发起的内容写入。
//	故刻意不要求任何写能力位，否则只读成员触发缩略图生成时会失败。
//	（同理见 upload.go 里 thumbOnly 白名单的说明。）
func (f *DBFS) SetThumbDisabled(ctx context.Context, uri *fs.URI, disabled bool) error {
	if uri == nil {
		return fs.ErrPathNotExist.WithError(fmt.Errorf("nil uri for thumb disabled mark"))
	}

	// 用目标 URI 自身的 navigator 解析。缩略图目标必然是可读文件，
	// 其 navigator 至少具备 ListChildren/Info 级别的读取能力。
	// ⚠️ 刻意不要求任何【写】能力位：本方法是服务端内部状态写入，
	// 不应因调用者当前 navigator 的写能力位而失败
	//（例如只读成员触发缩略图生成，其 navigator 无 UploadFile 位）。
	navigator, err := f.getNavigator(ctx, uri, NavigatorCapabilityListChildren)
	if err != nil {
		return err
	}

	target, err := f.getFileByPath(ctx, navigator, uri)
	if err != nil {
		return fmt.Errorf("failed to resolve file for thumb disabled mark: %w", err)
	}

	fc, tx, ctx, err := inventory.WithTx(ctx, f.fileClient)
	if err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to start transaction", err)
	}

	if disabled {
		if err := fc.UpsertMetadata(ctx, target.Model, map[string]string{ThumbDisabledKey: ""}, nil); err != nil {
			_ = inventory.Rollback(tx)
			return fmt.Errorf("failed to upsert thumb disabled mark: %w", err)
		}
	} else {
		if err := fc.RemoveMetadata(ctx, target.Model, ThumbDisabledKey); err != nil {
			_ = inventory.Rollback(tx)
			return fmt.Errorf("failed to remove thumb disabled mark: %w", err)
		}
	}

	if err := inventory.Commit(tx); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to commit thumb disabled mark", err)
	}

	return nil
}
