package teamtrash

import (
	"context"
	"strconv"

	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/file"
	"github.com/cloudreve/Cloudreve/v4/ent/metadata"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs/dbfs"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
)

// restorePlan 是"一个待恢复文件 + 它的落点"。
type restorePlan struct {
	file  *ent.File // 待恢复的行（owner=系统账号, file_children=0）
	dest  *ent.File // 恢复到的目标目录行
	name  string    // 恢复后的原名（= restore_uri 的末段）
}

// Restore 把回收站里的文件恢复到原位置。
//
// 【执行语义 —— 与个人空间 Restore 逐条对齐】
//
// 个人空间恢复 = DBFS.Restore -> MoveOrCopy -> moveFiles（manage.go:1055-1095），
// 它做三件事：SetParent（挂回原目录）、Rename（改回原名）、
// RemoveMetadata（清掉 sys:restore_uri / sys:expected_collect_time）。
// 本方法做【完全相同的三步】，且同样放在一个事务里：
//   · 全部成功才提交（Peer 要求：不要"前两个恢复了、第三个失败"的半完成状态）
//   · 任一步失败整体回滚
//
// 【有意不做/降级的部分（透明记录，不是遗漏）】
//
//  1. 不走 fs 锁（acquireByPath）。个人恢复会锁源与目标。
//     这里只锁不上的代价：并发恢复同一条时，第二次会因
//     Rename 撞唯一约束而整体回滚并报"同名已存在" —— 结果仍然是
//     一致的（不会出现半个恢复），只是错误文案偏保守。
//     SQLite 单写者 + 事务串行化使窗口极小；要复刻锁语义需要
//     复活一整套 LockByPath 路径解析，收益不成比例。
//  2. 不发 fs 事件（emitFileMoved）。个人恢复会在 websocket 上推
//     "文件移动"事件。本接口的验收是"响应后重新拉列表"，前端
//     恢复成功即 refetch，不依赖推送。要加事件需要 DBFS 实例
//     （事件挂在 dbfs 上），会在 service 层造出第二个 DBFS ——
//     权衡后不引入；若前端将来需要实时推送，在 dbfs 侧加钩子更合适。
//  3. 存储配额不动。move 不改变 owner 与 entity 归属，
//     storage diff 恒为空（与 moveFiles 对非 copy 分支一致，
//     它的 storageDiff 在 move 分支从未被赋值）。
//
// 【恢复权限（先全部校验，再执行）】
//
//   - 项目门：requireWriteOnProject（= 能删的判定，见 service.go）
//   - 目标目录门：ResolveFilePermission(dest) —— 恢复是向目标目录
//     【写入】，与 MoveOrCopy 对 destination 的 aclGuard 同位置同语义
//   - 目标目录必须真实存在：不存在 -> 明确报错，不静默丢到根
//   - 同名冲突：预检 + 约束错误双保险（预检给友好文案，
//     约束兜底防 TOCTOU）
func (s *Service) Restore(ctx context.Context, u *ent.User, projectID int, fileHashIDs []string) error {
	// ── 门 1：项目权限 ──────────────────────────────────────
	if err := s.requireWriteOnProject(ctx, u, projectID); err != nil {
		return err
	}
	if len(fileHashIDs) == 0 {
		return serializer.NewError(serializer.CodeParamErr, "No file specified", nil)
	}

	sysUserID, _, err := inventory.SystemTeamIDs(ctx, s.client)
	if err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to load system account", err)
	}
	if sysUserID == 0 {
		return ErrProjectNotFound
	}

	// ── 解码 hashid -> raw id（文件是 Cloudreve 实体，用 hashid，
	//    与 pathID 注释里"Cloudreve 的实体仍沿用 hashid"一致）──────
	fileIDs := make([]int, 0, len(fileHashIDs))
	seen := map[int]bool{}
	for _, h := range fileHashIDs {
		id, err := s.hasher.Decode(h, hashid.FileID)
		if err != nil || id <= 0 {
			return serializer.NewError(serializer.CodeParamErr, "invalid file id", err)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		fileIDs = append(fileIDs, id)
	}

	// ── 校验全部（不执行）—— 全过才进事务 ──────────────────
	plans, err := s.planRestore(ctx, u, projectID, sysUserID, fileIDs)
	if err != nil {
		return err
	}

	// ── 执行：一个事务内完成全部三步 ────────────────────────
	return s.executeRestore(ctx, plans)
}

// planRestore 校验每一条并算出落点。任何一条不合法 -> 整体拒绝。
func (s *Service) planRestore(ctx context.Context, u *ent.User, projectID, sysUserID int,
	fileIDs []int) ([]*restorePlan, error) {

	rows, err := s.client.File.Query().
		Where(file.IDIn(fileIDs...)).
		All(ctx)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load files", err)
	}
	if len(rows) != len(fileIDs) {
		// 数量对不上 = 至少一个 ID 不存在
		return nil, ErrProjectNotFound
	}
	rowByID := map[int]*ent.File{}
	for _, r := range rows {
		rowByID[r.ID] = r
	}

	// metadata 批量取
	mds, err := s.client.Metadata.Query().Where(
		metadata.FileIDIn(fileIDs...),
	).All(ctx)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load metadata", err)
	}
	restoreUriByFile := map[int]string{}
	for _, m := range mds {
		if m.Name == dbfs.MetadataRestoreUri {
			restoreUriByFile[m.FileID] = m.Value
		}
	}

	// 目标目录按 dir path 缓存（批量恢复常落在同一目录）
	destByDir := map[string]*ent.File{}

	plans := make([]*restorePlan, 0, len(fileIDs))
	for _, fid := range fileIDs {
		row := rowByID[fid]

		// ★ 身份校验：必须是【系统账号】的、【已摘出树】的文件 ——
		//   防止把个人空间的文件 ID 混进批次。
		//   FileChildren 是普通 int：0 = 无父目录（不是 *int！）。
		if row.OwnerID != sysUserID || row.FileChildren != 0 {
			return nil, ErrProjectNotFound
		}

		// ★ 归属校验：restore_uri 必须属于本项目。
		//   这是"不串"在【写路径】上的守卫，与 List 的读路径守卫成对。
		uri, ok := restoreUriByFile[fid]
		if !ok {
			// 没有恢复标记 -> 恢复不了（与 DBFS.Restore 的
			// "cannot restore file without required metadata mark" 同语义）
			return nil, ErrOriginalDirGone
		}
		pid, ok := parseProjectID(uri)
		if !ok || pid != projectID {
			return nil, ErrProjectNotFound
		}

		// 落点 = restore_uri 的父目录
		src, err := fs.NewUriFromString(uri)
		if err != nil {
			return nil, ErrOriginalDirGone
		}
		dirUri := src.DirUri()
		dirKey := dirUri.String()

		dest, ok := destByDir[dirKey]
		if !ok {
			dest, err = s.resolveDestination(ctx, dirUri, projectID, sysUserID)
			if err != nil {
				return nil, err
			}
			destByDir[dirKey] = dest
		}

		name := OriginalNameFromUri(uri)

		// 门 2：目标目录的写 ACL（与 MoveOrCopy 对 destination 的 aclGuard 同位）
		writable, err := s.canWriteNode(ctx, u, dest.ID)
		if err != nil {
			return nil, ErrNotPermitted
		}
		if !writable {
			return nil, ErrNotPermitted
		}

		// 门 3：同名冲突预检（友好文案；执行时还有约束兜底）
		conflict, err := s.client.File.Query().Where(
			file.FileChildrenEQ(dest.ID),
			file.NameEQ(name),
		).Exist(ctx)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to check name conflict", err)
		}
		if conflict {
			return nil, ErrNameConflict
		}

		plans = append(plans, &restorePlan{file: row, dest: dest, name: name})
	}

	return plans, nil
}

// resolveDestination 沿项目根向下走目录段，解析恢复落点。
//
// dirUri 形如 cloudreve://team/<pid> 或 cloudreve://team/<pid>/subA。
// Elements() = ["<pid>", ...子目录]（URI 的 host 段是文件系统名，
// 不进 path —— 见 PathTrimmed = Path 去掉前导 "/"）。
func (s *Service) resolveDestination(ctx context.Context, dirUri *fs.URI,
	projectID, sysUserID int) (*ent.File, error) {

	elements := dirUri.Elements()

	// 第一段必须是本项目 ID（parseProjectID 已在上游验过，此处双保险）
	if len(elements) == 0 || elements[0] != strconv.Itoa(projectID) {
		return nil, ErrOriginalDirGone
	}

	root, err := s.findProjectRoot(ctx, projectID, sysUserID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to locate project root", err)
	}
	if root == nil {
		// 项目根行都不在了 —— 目录自然也不在
		return nil, ErrOriginalDirGone
	}

	current := root
	for _, seg := range elements[1:] {
		child, err := s.client.File.Query().Where(
			file.FileChildrenEQ(current.ID),
			file.NameEQ(seg),
		).Only(ctx)
		if err != nil {
			// 目录不存在（或不唯一）-> 明确报错，不静默回退到根
			return nil, ErrOriginalDirGone
		}
		if child.Type != int(types.FileTypeFolder) {
			return nil, ErrOriginalDirGone
		}
		current = child
	}

	return current, nil
}

// canWriteNode 判定主体对节点的写权限 —— 与 aclGuard 回落语义一致
// （失败/未命中 -> 放行；命中 -> 必须 write）。
func (s *Service) canWriteNode(ctx context.Context, u *ent.User, nodeID int) (bool, error) {
	if u.Edges.Group != nil &&
		u.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		return true, nil
	}

	groupID := 0
	if u.Edges.Group != nil {
		groupID = u.Edges.Group.ID
	}

	acl := s.acl
	if bound, tx := inventory.InheritTx(ctx, s.acl); tx != nil {
		acl = bound
	}

	resolved, err := acl.ResolveFilePermission(ctx, nodeID, u.ID, groupID)
	if err != nil {
		return true, nil // 与 acl.go:113 一致：解析失败放行
	}
	if resolved == nil {
		return true, nil
	}
	return resolved.Permission == inventory.FileAclPermissionWrite, nil
}

// executeRestore 在单个事务内执行三步。
func (s *Service) executeRestore(ctx context.Context, plans []*restorePlan) error {
	fc, tx, txCtx, err := inventory.WithTx(ctx, s.fileClient)
	if err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to start transaction", err)
	}

	fail := func(e error) error {
		_ = inventory.Rollback(tx)
		return e
	}

	// 步 1：按目标目录分组 SetParent（保持 UUID 名，先挂树不改名 ——
	// 与 moveFiles 的顺序一致：先 SetParent 后 Rename）
	byDest := map[int][]*ent.File{}
	destByID := map[int]*ent.File{}
	for _, p := range plans {
		byDest[p.dest.ID] = append(byDest[p.dest.ID], p.file)
		destByID[p.dest.ID] = p.dest
	}
	for destID, models := range byDest {
		if err := fc.SetParent(txCtx, models, destByID[destID]); err != nil {
			if ent.IsConstraintError(err) {
				return fail(ErrNameConflict)
			}
			return fail(serializer.NewError(serializer.CodeDBError, "Failed to restore file location", err))
		}
	}

	// 步 2+3：逐个改回原名、清掉回收站标记
	for _, p := range plans {
		if _, err := fc.Rename(txCtx, p.file, p.name); err != nil {
			if ent.IsConstraintError(err) {
				return fail(ErrNameConflict)
			}
			if ent.IsNotFound(err) {
				// 校验后被并发硬删 —— 整批回滚
				return fail(ErrProjectNotFound)
			}
			return fail(serializer.NewError(serializer.CodeDBError, "Failed to restore file name", err))
		}

		if err := fc.RemoveMetadata(txCtx, p.file,
			dbfs.MetadataRestoreUri, dbfs.MetadataExpectedCollectTime); err != nil {
			return fail(serializer.NewError(serializer.CodeDBError,
				"Failed to remove trash bin metadata", err))
		}
	}

	// 存储 diff 恒为空（move 不改变 owner/entity 归属，见方法注释 3）
	if err := inventory.CommitWithStorageDiff(txCtx, tx, s.logger, s.userClient); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to commit restore change", err)
	}

	return nil
}
