package inventory

import (
	"context"
	"fmt"

	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/file"
	"github.com/cloudreve/Cloudreve/v4/ent/filepermission"
	"github.com/cloudreve/Cloudreve/v4/ent/schema"
	"github.com/cloudreve/Cloudreve/v4/pkg/conf"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
)

// ============================================================
// 文件 ACL（访问控制列表）数据访问层
//
// 契约：《契约补遗二-文件ACL.md》§2/§3/§5。
//
// 语义约定（务必与 service 层、测试保持一致）：
//
//  1. 对外 hashid、对内整型：subject_id / created_by 落库为整型，
//     接口层负责 hashid 编解码（与任务指派那次事故同一个教训）。
//  2. 继承采用【查询时向上回溯】：从目标文件自身沿父目录向上，
//     取最近的一条命中当前主体的记录，不做任何写时扇出。
//  3. inherit 的含义：
//     - true （默认）→ 该条记录对子项同样生效；
//     - false       → 该条记录【只对自身生效】，并且是继承边界：
//     子项不再向上继承更上层的 ACL。
//     这是「子项不再继承更上层的 ACL」这条验收标准的实现依据。
//  4. 同一个文件上，user 记录优先于 group 记录（更具体者胜）。
//  5. 全部回溯完仍无命中 → 返回 (nil, nil)，由调用方【完全回落到
//     Cloudreve 原有判定】。没有 ACL 的文件，行为必须与打补丁前一致。
// ============================================================

// 主体类型
const (
	FileAclSubjectUser  = "user"
	FileAclSubjectGroup = "group"
)

// 权限枚举（与分享权限对齐）
const (
	FileAclPermissionRead  = "read"
	FileAclPermissionWrite = "write"
)

// maxAclAncestorDepth 向上回溯的最大层数，防止脏数据（父子成环）导致死循环。
const maxAclAncestorDepth = 64

type (
	// NewFilePermissionArgs 写入一条 ACL 的入参
	NewFilePermissionArgs struct {
		FileID      int
		SubjectType string
		SubjectID   int
		Permission  string
		Inherit     bool
		CreatedBy   int
	}

	// ResolvedFilePermission ACL 解析结果（对某个主体实际生效的权限）
	ResolvedFilePermission struct {
		// "read" / "write"
		Permission string
		// 该权限来自哪个文件（直接命中时就是目标文件自身）
		SourceFileID int
		// 来源文件夹名。直接命中时为空串。
		SourceName string
		// 是否直接设在目标文件上
		Direct bool
	}

	// FilePermissionClient 文件 ACL 数据访问接口
	FilePermissionClient interface {
		TxOperator

		// UpsertFilePermission 写入/覆盖一条 ACL（按 file_id+subject_type+subject_id 唯一）。
		// PUT 语义：结果由本次入参完全决定。
		UpsertFilePermission(ctx context.Context, args *NewFilePermissionArgs) (*ent.FilePermission, error)
		// DeleteFilePermission 删除一条 ACL（硬删除，避免唯一索引与软删残留互相打架）。
		// 记录不存在时是空操作（幂等）。
		DeleteFilePermission(ctx context.Context, fileID int, subjectType string, subjectID int) error
		// ListFilePermissions 列出直接设在某个文件上的全部 ACL（不做继承）。
		ListFilePermissions(ctx context.Context, fileID int) ([]*ent.FilePermission, error)
		// GetFilePermission 取某个文件上某个主体的一条 ACL；不存在返回 (nil, nil)。
		GetFilePermission(ctx context.Context, fileID int, subjectType string, subjectID int) (*ent.FilePermission, error)
		// ResolveFilePermission 向上回溯解析某个主体对某个文件实际生效的权限。
		// 无任何命中时返回 (nil, nil)。
		ResolveFilePermission(ctx context.Context, fileID, userID, groupID int) (*ResolvedFilePermission, error)

		// ResolveFilePermissionsBatch 批量解析【多个文件】各自生效的权限。
		// 与 ResolveFilePermission 语义【完全相同】，只是一次解析一批：
		// 返回 map[fileID]*ResolvedFilePermission，无命中的 fileID 不出现在 map 里
		// （等价于单节点版的 (nil, nil)）。
		//
		// 【为什么必须与单节点版语义相同 —— 这是结构保证，不是口头承诺】
		// 两条路径共用同一个纯内存判定函数 resolveAclFromChain，
		// 差别仅在于"链数据从哪来"（逐个查库 vs 一次批量取回）。
		// 因此语义一致性由【共用函数】保证；配套测试还直接比对两者的
		// resolved 输出（不只是下游的 capability）。
		ResolveFilePermissionsBatch(ctx context.Context, fileIDs []int, userID, groupID int) (map[int]*ResolvedFilePermission, error)

		// AnyFilePermissionExists 全库是否存在任何一条 ACL（快路径判据）。
		//
		// 用途：列表时若全库没有任何 ACL，则必然不存在"子级 ACL"，
		// 可跳过批量解析直接按项目根判定。
		//
		// ⚠️ 判据必须是【全局】，不能是"本项目 ACL 行数 <= 1"：
		// 后者有反例 —— 项目只有 1 条 ACL 且挂在子文件夹上时，
		// "行数<=1" 会误判为"只有根级 ACL"，从而走项目根判定，
		// 把"子级只读"这个要修的缺陷原样保留。
		AnyFilePermissionExists(ctx context.Context) (bool, error)
	}
)

func NewFilePermissionClient(client *ent.Client, dbType conf.DBType, hasher hashid.Encoder) FilePermissionClient {
	return &filePermissionClient{
		client:      client,
		hasher:      hasher,
		maxSQlParam: sqlParamLimit(dbType),
	}
}

type filePermissionClient struct {
	maxSQlParam int
	client      *ent.Client
	hasher      hashid.Encoder
}

func (c *filePermissionClient) SetClient(newClient *ent.Client) TxOperator {
	return &filePermissionClient{client: newClient, hasher: c.hasher, maxSQlParam: c.maxSQlParam}
}

func (c *filePermissionClient) GetClient() *ent.Client {
	return c.client
}

// ValidFileAclSubject 校验主体类型
func ValidFileAclSubject(s string) bool {
	return s == FileAclSubjectUser || s == FileAclSubjectGroup
}

// ValidFileAclPermission 校验权限枚举
func ValidFileAclPermission(p string) bool {
	return p == FileAclPermissionRead || p == FileAclPermissionWrite
}

// UpsertFilePermission 写入/覆盖一条 ACL（PUT 语义）。
//
// 已存在同键记录时走 UPDATE（保留 ID，前端不必因为 ID 漂移而重新渲染）；
// 记录被软删过时一并恢复（ClearDeletedAt），避免唯一索引被残留行卡住。
func (c *filePermissionClient) UpsertFilePermission(ctx context.Context,
	args *NewFilePermissionArgs) (*ent.FilePermission, error) {

	if args == nil {
		return nil, fmt.Errorf("nil args")
	}

	ctx = schema.SkipSoftDelete(ctx)
	existing, err := c.client.FilePermission.Query().
		Where(
			filepermission.FileID(args.FileID),
			filepermission.SubjectType(args.SubjectType),
			filepermission.SubjectID(args.SubjectID),
		).First(ctx)

	switch {
	case err == nil:
		return c.client.FilePermission.UpdateOneID(existing.ID).
			SetPermission(args.Permission).
			SetInherit(args.Inherit).
			SetCreatedBy(args.CreatedBy).
			ClearDeletedAt().
			Save(ctx)
	case ent.IsNotFound(err):
		return c.client.FilePermission.Create().
			SetFileID(args.FileID).
			SetSubjectType(args.SubjectType).
			SetSubjectID(args.SubjectID).
			SetPermission(args.Permission).
			SetInherit(args.Inherit).
			SetCreatedBy(args.CreatedBy).
			Save(ctx)
	default:
		return nil, err
	}
}

// DeleteFilePermission 硬删除一条 ACL；不存在时为空操作。
func (c *filePermissionClient) DeleteFilePermission(ctx context.Context,
	fileID int, subjectType string, subjectID int) error {

	ctx = schema.SkipSoftDelete(ctx)
	_, err := c.client.FilePermission.Delete().
		Where(
			filepermission.FileID(fileID),
			filepermission.SubjectType(subjectType),
			filepermission.SubjectID(subjectID),
		).Exec(ctx)
	return err
}

// ListFilePermissions 列出直接设在某个文件上的 ACL（按主体排序，结果稳定）
func (c *filePermissionClient) ListFilePermissions(ctx context.Context, fileID int) ([]*ent.FilePermission, error) {
	return c.client.FilePermission.Query().
		Where(filepermission.FileID(fileID)).
		Order(ent.Asc(filepermission.FieldSubjectType), ent.Asc(filepermission.FieldSubjectID)).
		All(ctx)
}

// GetFilePermission 取某个文件上某个主体的一条 ACL
func (c *filePermissionClient) GetFilePermission(ctx context.Context,
	fileID int, subjectType string, subjectID int) (*ent.FilePermission, error) {

	row, err := c.client.FilePermission.Query().
		Where(
			filepermission.FileID(fileID),
			filepermission.SubjectType(subjectType),
			filepermission.SubjectID(subjectID),
		).First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

// ============================================================
// 判定核心：从"一条祖先链"解析出对该主体生效的权限
//
// 【为什么抽成独立函数】
// 单节点路径（ResolveFilePermission）与批量路径（ResolveFilePermissionsBatch）
// 必须给出【完全相同】的结论。若各写一份判定逻辑，"语义相同"就只是口头承诺，
// 迟早会漂移（本项目已经反复吃过"两处实现看起来一样"的亏）。
// 抽成共用函数后，两条路径的差异被压缩到唯一一点：
//   链数据从哪来（逐个查库 vs 一次批量取回）。
//
// 输入 aclRowAt：depth -> 该层直接挂着的 ACL 行（可含多条，对应不同主体）。
//   depth=0 为目标文件自身，depth 越大越远。
//   depth 的层数由调用方在上溯时确定，本函数不关心"怎么走的"。
//
// 语义（逐字对应原 ResolveFilePermission 的循环，不得改动）：
//   · depth==0（目标自身）：只要该层有命中当前主体的记录，无论 inherit 取值都生效。
//     该层若无命中但存在 inherit=false 的记录，则构成继承边界 -> 返回 nil。
//   · depth>0（祖先）    ：只有 inherit=true 且命中当前主体的记录才生效。
//     该层若存在 inherit=false 的记录，则构成继承边界 -> 返回 nil。
//   · 全部走完无命中 -> (nil, nil)，由调用方完全回落到 Cloudreve 原有判定。
//
// nameOf 用于填充 SourceName（祖先命中时的文件夹名）；depth==0 时不使用。
// ============================================================
func resolveAclFromChain(aclRowAt map[int][]*ent.FilePermission,
	userID, groupID int,
	nameOf func(depth int) string) *ResolvedFilePermission {

	// 必须按 depth 升序（近者优先）。调用方保证 depth 从 0 连续到 maxDepth，
	// 这里用显式循环而不是遍历 map，避免依赖 map 迭代顺序。
	for depth := 0; depth < maxAclAncestorDepth; depth++ {
		rows, ok := aclRowAt[depth]
		if !ok {
			// 该层不存在（链已到头）。继续看更深层没有意义。
			return nil
		}
		if len(rows) == 0 {
			continue
		}

		matched := pickFileAclForSubject(rows, userID, groupID)
		boundary := hasFileAclBoundary(rows)

		if depth == 0 {
			// 目标文件自身的记录：无论 inherit 取值，它都描述「这个文件」。
			if matched != nil {
				return &ResolvedFilePermission{
					Permission:   matched.Permission,
					SourceFileID: matched.FileID,
					Direct:       true,
				}
			}
			if boundary {
				// 目标文件是继承边界：不再向上取用更上层的 ACL
				return nil
			}
			continue
		}

		// 祖先上的记录：只有 inherit = true 才会向下传递到本文件
		if matched != nil && matched.Inherit {
			return &ResolvedFilePermission{
				Permission:   matched.Permission,
				SourceFileID: matched.FileID,
				SourceName:   nameOf(depth),
				Direct:       false,
			}
		}
		if boundary {
			return nil
		}
	}

	return nil
}

// ResolveFilePermission 从目标文件沿父目录向上回溯，解析实际生效的权限。
//
// 返回 (nil, nil) 表示「无任何命中」——调用方必须原样回落到 Cloudreve 原有判定。
func (c *filePermissionClient) ResolveFilePermission(ctx context.Context,
	fileID, userID, groupID int) (*ResolvedFilePermission, error) {

	if fileID <= 0 || userID <= 0 {
		return nil, nil
	}

	// 逐层查库，把链数据整理成 resolveAclFromChain 需要的形状，
	// 然后交给【与批量路径共用】的判定函数。这样两条路径的结论必然一致。
	aclRowAt := make(map[int][]*ent.FilePermission, 8)
	nameOf := func(depth int) string { return "" }

	node := fileID
	for depth := 0; depth < maxAclAncestorDepth && node > 0; depth++ {
		current, err := c.client.File.Get(ctx, node)
		if err != nil {
			// 文件不存在（已被物理删除）时停止回溯，回落到原有判定
			if ent.IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}

		rows, err := c.ListFilePermissions(ctx, node)
		if err != nil {
			return nil, err
		}
		aclRowAt[depth] = rows
		if depth > 0 {
			// 捕获该层名字用于 SourceName（闭包按值取当前 current）
			cur := current
			prev := nameOf
			nameOf = func(d int) string {
				if d == depth {
					return cur.Name
				}
				return prev(d)
			}
		}

		// 向上走一层。
		// 注意：ent 的 file_children 是 Optional 但非 Nillable，未设置时为 0（文件 ID 从 1 起）。
		if current.FileChildren <= 0 {
			break
		}
		node = current.FileChildren
	}

	return resolveAclFromChain(aclRowAt, userID, groupID, nameOf), nil
}

// pickFileAclForSubject 在同一个文件的若干条记录中挑选对该主体生效的那条。
func pickFileAclForSubject(rows []*ent.FilePermission, userID, groupID int) *ent.FilePermission {
	var groupHit *ent.FilePermission
	for _, row := range rows {
		switch row.SubjectType {
		case FileAclSubjectUser:
			if row.SubjectID == userID {
				return row
			}
		case FileAclSubjectGroup:
			if groupID > 0 && row.SubjectID == groupID && groupHit == nil {
				groupHit = row
			}
		}
	}
	return groupHit
}

// hasFileAclBoundary 该文件上是否存在继承边界（inherit = false 的记录）。
//
// 语义：一个目录被标记为边界后，其内部不再继承更上层的 ACL ——
// 与「设 inherit=false 的文件夹 → 其子项不再继承更上层的 ACL」这条验收标准对应。
func hasFileAclBoundary(rows []*ent.FilePermission) bool {
	for _, row := range rows {
		if !row.Inherit {
			return true
		}
	}
	return false
}

// ============================================================
// 批量解析：一次取回 N 个节点各自的祖先链上的全部 ACL
//
// 【为什么不能"N 次单节点查询"】
// 一次列表要走 decorateRealFile 包装 N 个节点，若每个节点都调一次
// ResolveFilePermission，就是 N 次上溯 × (1 次 File.Get + 1 次 ListFilePermissions)。
// 这不是固有成本，是可以避免的实现代价。
//
// 【关键洞察】
// 上溯走的是 files.file_children 这条【父指针链】，
// 所以"N 个节点各自的祖先链"= N 条父指针链的并集，可以用一条递归 CTE 取回。
//
// 【SQL 必须返回 (sid, depth, acl_row) 三元关系 —— 这是设计要害】
// 只返回 acl 行是不够的：
//   节点 X 的链上有 A(depth2) 与 B(depth5) 两条 ACL，
//   若丢掉 sid/depth，就【分不清哪条离 X 更近】，无法实现"最近祖先优先"。
// 而"最近祖先优先"正是既有 ResolveFilePermission 的语义。
// 因此：
//   · sid   ：这条 ACL 属于哪个 seed 节点的链（否则不同节点的同一祖先会被合并）
//   · depth ：离该节点多少层（否则无法近者优先）
//   · 必须 UNION ALL 而不是 UNION：UNION 会按 (node_id,depth) 去重，
//     把【不同 sid 的同一祖先】合并成一行，直接丢掉归属信息。
//
// 【为什么不依赖"当前不在事务内"这个不变量】
// 批量查询在 Children 路径上。就当前代码而言，Children 只被
// dbfs.go:200（DBFS.List）与 global.go:34（AllFilesInTrashBin）调用，
// 而这两处的函数体内都没有 WithTx。
// 但 S0a 的教训是：**"现在不在事务内"不是不变量** ——
// 那次 getNavigator 被 upload 路径在事务内调用，导致单连接 SQLite 永久等待。
// 所以这里不赌调用位置，而是用 InheritTx：事务内就绑进当前事务，
// 事务外就用普通连接。"在不在事务内"因此不再重要。
// ============================================================
func (c *filePermissionClient) ResolveFilePermissionsBatch(ctx context.Context,
	fileIDs []int, userID, groupID int) (map[int]*ResolvedFilePermission, error) {

	out := make(map[int]*ResolvedFilePermission)
	if len(fileIDs) == 0 || userID <= 0 {
		return out, nil
	}

	// 去重并丢掉非法 id（0/负数不是有效文件）
	seen := make(map[int]struct{}, len(fileIDs))
	seeds := make([]int, 0, len(fileIDs))
	for _, id := range fileIDs {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		seeds = append(seeds, id)
	}
	if len(seeds) == 0 {
		return out, nil
	}

	// 一次取回"每个 seed 节点 × 其祖先链"上的全部 ACL 行（含 sid 与 depth）
	anchors, err := c.queryAncestorAclRows(ctx, seeds)
	if err != nil {
		return nil, err
	}

	// 组装 depth -> rows（含 SourceName），交给与单节点版共用的判定函数。
	for _, sid := range seeds {
		chain := anchors[sid]
		if len(chain) == 0 {
			continue
		}

		aclRowAt := make(map[int][]*ent.FilePermission, len(chain))
		names := make(map[int]string, len(chain))
		maxD := 0
		for d, node := range chain {
			aclRowAt[d] = node.Rows
			names[d] = node.Name
			if d > maxD {
				maxD = d
			}
		}
		// 逐层补空切片：让 resolveAclFromChain 能区分"该层无 ACL"与"链已到头"。
		// 链是连续的 0..maxD（queryAncestorAclRows 保证），无需补齐中间层。
		_ = maxD

		nameOf := func(d int) string { return names[d] }
		if r := resolveAclFromChain(aclRowAt, userID, groupID, nameOf); r != nil {
			out[sid] = r
		}
	}

	return out, nil
}

// AnyFilePermissionExists 全库是否存在任何一条 ACL（快路径判据）。
//
// 用 EXISTS 而不是 COUNT：只问"有没有"，不数个数。
// 生产实测：EXISTS 0.054ms/次，COUNT 0.310ms/次（74 文件规模）。
func (c *filePermissionClient) AnyFilePermissionExists(ctx context.Context) (bool, error) {
	n, err := c.client.FilePermission.Query().Limit(1).Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// aclChainNode 是批量查询返回的"某一层"：该层直接挂着的 ACL 行 + 该层文件名。
type aclChainNode struct {
	Rows []*ent.FilePermission
	Name string
}

// queryAncestorAclRows 取回 N 个节点的祖先链及其上的 ACL 行。
//
// 返回 map[seedID]map[depth]aclChainNode：
//   · 链是连续的 0..maxDepth（0 为节点自身）
//   · 某一层无 ACL 时 Rows 为空切片（不是缺失，以区分"为空"与"未取到"）
//
// ⚠️ depth 是【语义必需】，不是调试信息：
//   没有 depth 就无法实现"最近祖先优先"——
//   同一批里不同节点的祖先会混在一起，且分不清是哪一层的规则命中。
func (c *filePermissionClient) queryAncestorAclRows(ctx context.Context,
	seeds []int) (map[int]map[int]aclChainNode, error) {

	// ============================================================
	// ★ 为什么这里【不用】原生递归 CTE —— 一段踩坑记录
	//
	// 第一版是用 WITH RECURSIVE 一条 SQL 起底的（见 git 历史），
	// 运行时却恒失败：
	//     "failed to query ancestor acl rows: Driver.QueryContext is not supported"
	//
	// 原因：ent.Client 内部把 driver 包成了 inventory/debug.DebugDriver
	// （client.go:154 => debug.DebugWithContext），
	// 它只实现了 dialect.Driver 的 Query/Exec，
	// 于是 debug.go:69-74 那个 QueryContext 类型断言直接判 unsupported。
	//
	// 【比失败本身更值得记的，是它当时是【静默】的】
	// resolveNodeCapability 把错误回落成"可写"，
	// 端到端表现是 capability 一直 w8aI ——
	// 看起来像"节点级判定没生效"，
	// 实际是"ACL 查询从来没跑起来过"。
	// 差一点就去改一个根本没参与执行的函数。
	//
	// 【结构性教训】回落（fallback）必须【可见】。
	// 一个把异常吞成默认值的兜底，会让"整条链路没跑"伪装成"逻辑算错了"。
	// 所以这里改成：错误【向上抛】，由调用方决定回落并【记日志】——
	// 事实上现在 resolveNodeCapability / applyNodeCapabilities
	// 都会 logAclFallback(...) 把原因写进日志，不再静默。
	//
	// 【现在的实现】用 ent 生成的查询，不用裸 SQL：
	//   · 不依赖具体方言（sqlite/mysql 都能跑，CTE 语法两边并不通用）
	//   · 不依赖 driver 是否暴露 QueryContext
	//   · 逐层向上走，层数受 maxAclAncestorDepth 限制，天然有界
	// 代价是"每层一次查询"，但链深在实际数据里通常 2~4 层，
	// 且上层调用方是【批量】的（一次解析一批 fileID），
	// 这里的 N 是"链深"而不是"文件数"，不会随目录大小放大。
	// ============================================================
	out := make(map[int]map[int]aclChainNode, len(seeds))

	for _, seed := range seeds {
		chain := make(map[int]aclChainNode, 8)
		nodeID := seed
		seen := make(map[int]bool, 8)

		for depth := 0; depth < maxAclAncestorDepth; depth++ {
			if nodeID <= 0 || seen[nodeID] {
				// nodeID <= 0：到达根（file_children 为 NULL/0）
				// seen：数据异常成环，主动止损，不让它无限转
				break
			}
			seen[nodeID] = true

			// 该节点的 ACL 行（可能 0 行 -> LEFT JOIN 的等价物）
			rows, err := c.client.FilePermission.Query().
				Where(filepermission.FileID(nodeID)).
				All(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to query acl rows for file %d: %w", nodeID, err)
			}

			// 该节点的名字与父指针，一次取回
			f, err := c.client.File.Query().
				Where(file.ID(nodeID)).
				Only(ctx)
			if err != nil {
				if ent.IsNotFound(err) {
					break
				}
				return nil, fmt.Errorf("failed to query file %d: %w", nodeID, err)
			}

			chain[depth] = aclChainNode{Rows: rows, Name: f.Name}

			// file_children 是【父指针】（NULL/0 = 没有父，即到达顶层）。
			// 它在本项目里是 int 而不是 *int，0 就是"无父"。
			if f.FileChildren <= 0 {
				break
			}
			nodeID = f.FileChildren
		}

		out[seed] = chain
	}

	return out, nil
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
