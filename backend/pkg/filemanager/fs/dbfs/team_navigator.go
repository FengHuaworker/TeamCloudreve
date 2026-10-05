package dbfs

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cloudreve/Cloudreve/v4/application/constants"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/file"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/boolset"
	"github.com/cloudreve/Cloudreve/v4/pkg/cache"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
	"github.com/cloudreve/Cloudreve/v4/pkg/setting"
)

// 团队空间文件系统（第 1 步：只读导航壳）。
//
// 结构（Peer 定，见 第1步-调研报告.md §6）：
//
//	cloudreve://team          ← 虚拟根，列出「我参与的项目」
//	  └─ <项目 ID>             ← 每个项目一个虚拟目录
//	       └─ （第 1 步恒为空）
//
// 【本步边界】第 1 步只做「导航结构」，不建立文件归属。
// 团队附件目前只引用上传者个人空间的文件（inventory/team.go:104
// 「只存 Cloudreve 文件引用，不复制文件」），没有任何文件归属于系统账号，
// 因此项目目录下没有内容 —— 这是设计边界，不是缺陷。
// 建立归属是第 2 步的工作（届时使用 inventory.SystemTeamAccount，当前零调用者）。
//
// 【只读如何做实】能力集见 navigator.go 的 teamNavigatorCapability，
// 不含 CreateFile / RenameFile / UploadFile / DeleteFile。
// 上传/新建/重命名/删除会在 dbfs.go:784-790 被 ErrNotSupportedAction 拒绝，
// 属服务端强制，不依赖前端隐藏按钮。
//
// 【路径段用项目 ID 而非名称】名称可重复、可修改，ID 才是稳定标识；
// 且团队模块对外暴露的一律是整数 project_id（service/team/types.go:74），
// 不引入 hashid 以保持口径一致。

// teamNavigatorCapability 团队空间的【只读】能力集。
//
// 实际能力位在 navigator.go 的 init() 中设置，此处仅声明。
// 用于：只读主体、或无法判定权限时（保守）。
var teamNavigatorCapability = &boolset.BooleanSet{}

// teamNavigatorWriteCapability 【只读位 + 写位】的完整能力集。
//
// 【2026-10-04 第 3 步】引入它的原因：
// 原先 teamNavigatorCapability 被同时当作「只读基线」和「唯一能力集」使用
// （navigator.go 的 init 把写位也塞进了同一份），
// 导致能力集无法按用户/文件区分 —— 前端因此对所有成员一律显示可写入口。
//
// 现在：读位在 init 里设进 teamNavigatorCapability，
// 写位设进 teamNavigatorWriteCapability；Capabilities() 按 ACL 判定二选一。
// 写集必须**包含**读集，否则可写成员反而列不出目录。
var teamNavigatorWriteCapability = &boolset.BooleanSet{}

// newTeamUri 构造团队空间根 URI：cloudreve://team
func newTeamUri() *fs.URI {
	res, _ := fs.NewUriFromString(constants.CloudreveScheme + "://" + string(constants.FileSystemTeam))
	return res
}

// newTeamProjectUri 构造团队空间下某项目的 URI：cloudreve://team/<项目 ID>
func newTeamProjectUri(projectID int) *fs.URI {
	res, _ := fs.NewUriFromString(fmt.Sprintf("%s://%s", constants.CloudreveScheme, constants.FileSystemTeam))
	return res.Join(strconv.Itoa(projectID))
}

// projectRootOf 已移除：RootUri() 必须回到 FS 根（cloudreve://team）而非项目根，
// 否则与已含项目段的 PathTrimmed() 拼出重复项目段。见 decorateRealFile 说明。

// ⚠️⚠️ 临时机制，待第 3 步 ACL 取代 —— 使用前务必读完本注释。
//
// withTeamOwnerBypass 在**团队文件系统的写操作**上标记跳过「仅所有者可写」校验。
//
// 【为什么需要】团队空间的文件归属于系统账号（OwnerModel/OwnerID = 系统账号 id），
// 而写操作者是被授权的团队成员，二者必然不等。dbfs 中形如
//
//	if ...; !ok && target.Owner().ID != f.user.ID { return nil, fs.ErrOwnerOnly }
//
// 的「所有者校验」若不跳过，则任何团队写入都会被 ErrOwnerOnly 拒绝 ——
// 即使能力位已开放。
//
// 【完整清单以检索为准，勿依赖本行数字】
//
//	rg -n "ByPassOwnerCheckCtxKey" --glob '*.go'
//
// 截至 2026-10-04：消费方命中 14 处 = 13 处所有者校验 + 1 处 ACL 跳过
// （后者已于第 3 步删除，见 acl.go 的说明）。生产设置方 12 处 = team 9 处 + system 3 处。
// ⚠️ 计数会随改动腐烂；上述命令的输出才是权威，本注释只记录"当时是多少"。
//
// 【它绕过了什么】「个人空间只能由所有者本人写」这条底线。
// 对团队空间而言该规则**本就不适用**（共享空间允许多成员写入），
// 因此语义上成立；但该 ctx 一旦设置，**上述所有所有者校验全部失效，范围偏大**。
//
// ✅ 【2026-10-04 第 3 步已处理】本 key 原先还会让 aclGuard 提前返回 nil
// （旧 acl.go:67），即**同时禁用 ACL 判定**。该短路已删除：
// 现在本 key 只豁免「所有者校验」，不再豁免「ACL 判定」——
// 两种语义已分开，团队空间的写权限由文件 ACL 决定。
//
// 【护栏】作用域必须收窄 —— 只在团队 URI 的操作路径上设置。
// 绝不可放到中间件、To() 或任何「请求级」位置：move/copy 等跨越两个文件系统的
// 操作中，若 ctx 携带本标记传播到个人空间一侧，将导致该侧的所有者校验被错误跳过。
// 见 team_navigator_test.go 中 TestTeamBypassDoesNotLeakToPersonalFs 的断言。
//
// 【第 3 步后续】ACL 规则（项目成员 -> write）落地后，
// 本旁路可由精确规则取代，即从「跳过判定」变为「改用更精确的判定」。
func withTeamOwnerBypass(ctx context.Context) context.Context {
	return WithBypassOwnerCheck(ctx)
}

// isTeamUri 判断给定 URI 是否属于团队文件系统。
//
// 用于在**写操作入口**收窄 withTeamOwnerBypass 的作用域：
// 只有目标路径确实落在 team 时才设置旁路，个人空间等其它文件系统不受影响。
func isTeamUri(path *fs.URI) bool {
	return path != nil && path.FileSystem() == constants.FileSystemTeam
}

// withTeamOwnerBypassIfTeam 仅当**所有**目标 URI 都落在团队文件系统时设置所有者旁路。
//
// 这是护栏 1 的落点：调用点在**具体写操作入口**（Create/Rename/Delete/
// PrepareUpload/CompleteUpload/CancelUploadSession/MoveOrCopy），
// 而非中间件或 To()，避免 ctx 外溢到同请求中的其它文件系统操作。
//
// ⚠️ 判定用「全部」而非「任一」，这是**安全必需**：
// MoveOrCopy 的 path 与 dst 可能分属不同文件系统。若用"任一为 team 即设置"，
// 则该 ctx 会传播到个人空间一侧的**全部所有者校验**，导致越权写入。
// 由 TestTeamBypassDoesNotLeakToPersonalFs 守护该语义。
//
// 调用方传入的 URI 已覆盖本次操作的全部参与方：
//   - 单目标操作（Create/Rename/Delete/Upload）传各自的目标 URI
//   - MoveOrCopy 传 path... 与 dst 的全部组合
//   - 无参数 -> 不设置（保守）
func withTeamOwnerBypassIfTeam(ctx context.Context, paths ...*fs.URI) context.Context {
	if len(paths) == 0 {
		return ctx
	}
	for _, p := range paths {
		if !isTeamUri(p) {
			return ctx
		}
	}
	return withTeamOwnerBypass(ctx)
}

// NewTeamNavigator 创建团队空间 navigator。
// NewTeamNavigator 构造团队空间 navigator。
//
// aclClient 用于计算能力位（见 canWriteNode）。可为 nil —— 此时写位一律关闭，
// 但读位不受影响（只读成员仍可浏览）。
func NewTeamNavigator(u *ent.User, fileClient inventory.FileClient,
	teamClient inventory.TeamClient, userClient inventory.UserClient,
	l logging.Logger, config *setting.DBFS, hasher hashid.Encoder,
	aclClient inventory.FilePermissionClient) Navigator {
	n := &teamNavigator{
		user:       u,
		l:          l,
		fileClient: fileClient,
		teamClient: teamClient,
		userClient: userClient,
		config:     config,
		hasher:     hasher,
		aclClient:  aclClient,
	}
	n.baseNavigator = newBaseNavigator(fileClient, defaultFilter, u, hasher, config)
	return n
}

type teamNavigator struct {
	l          logging.Logger
	user       *ent.User
	fileClient inventory.FileClient
	teamClient inventory.TeamClient
	userClient inventory.UserClient
	config     *setting.DBFS
	hasher     hashid.Encoder

	// aclClient 用于计算能力位（canWriteNode）。由 DBFS 注入。
	aclClient inventory.FilePermissionClient

	root *File
	*baseNavigator

	// node 是本次导航的目标节点（由 To() 设置），用于计算能力位。
	//
	// 【2026-10-04 第 3 步】Capabilities() 需要知道「对哪个文件」计算写权限，
	// 而 Capabilities(isSearching) 的签名不接受文件参数（fs 接口约定），
	// 因此由 To() 在解析出目标节点后写入本字段。
	node *File

	// ⚠️⚠️ S0a 的能力位按【项目根】判定，不是按每个节点判定 —— 这是 S0a 的
	//       【已知边界】，不是缺陷，但它意味着下列限制，务必读全：
	//
	//	ACL 写在项目根 -> 能力位正确 -> 前端隐藏写入口 ✅ -> 与 aclGuard 一致
	//	ACL 写在子级   -> 能力位仍说"可写"（因为它看的是项目根）
	//	                  -> 前端【显示】写入口 ❌ -> 点了以后 aclGuard 拒绝 403
	//
	//     实测证据（2026-10-04，隔离实例，同一只读主体只改 ACL 所在层级）：
	//	  ACL 在项目根(file 94)  -> props.capability='gAaI' 只读集 -> 403 Not supported action
	//	  ACL 在文件(file 112)   -> props.capability='w8aI' 可写集 -> 403 read-only
	//
	//     根因：能力位的判定对象是【项目根】（findProjectRoot 查到的 files 行），
	//	   而 aclGuard 的判定对象是【本次操作的目标文件】。两者判定对象不同，
	//	   所以当 ACL 只写在子级、项目根没有 ACL 时，两者必然不一致。
	//
	//     【S0b 会解决这一条】：把能力位细化到节点级，让两者判定对象重新一致。
	//     S0b 的方案与验收另行给出，【不塞进 S0a】。
	//
	//     S0b 落地前，团队空间的 ACL 【应当】只写在项目根上；
	//     写在子级不会报错，但前端不会反映它（会出现"按钮在、点了报错"）。
	//
	// capabilityProjectID 是本次能力位判定所属的项目 id（0 = 团队根/无效）。
	//
	// 【为什么不用 node 承载 —— 实测死锁】
	// getNavigator 会在【事务内】被调用，此时任何查库都会在 SQLite 单连接上
	// 永久等待（实测团队上传 16s 超时，个人空间正常）。
	// 故 setCapabilityTarget 只记录 id（纯内存），
	// 由 canWriteNode 在真正需要时查询。
	capabilityProjectID int

	// capabilityCtx 是本次能力位判定的请求 ctx。
	//
	// 【为什么必须保存它，而不是在查询处用 context.Background()】
	// Peer 2026-10-04 裁决：Capabilities() 就在 getNavigator【内部】被调用
	// （dbfs.go:802），所以"查库是否在事务内"取决于【谁调用 getNavigator】，
	// 与我"把查询写在哪一行"无关。
	//
	// 用 context.Background() 时，ctx 里永远没有事务 —— 于是【永远无法继承事务】，
	// 无论写在哪一行都不安全。正确形态是保存请求 ctx，
	// 再用 inventory.InheritTx 绑定当前事务（与 acl.go:106-109 同一模式）。
	// 这样"在不在事务内"不再重要，也就不会复发。
	capabilityCtx context.Context

	// ownerResolved / owner 缓存团队空间的配额归属者（系统账号）。
	// 见 resolveOwner 的说明：第 2 步起配额记在系统账号上（共享池）。
	ownerResolved bool
	owner         *ent.User
}

// resolveOwner 返回团队空间文件的「归属者」—— 即配额记账对象。
//
// 【为什么是系统账号而不是请求者】
// 上传路径 dbfs/upload.go:167-168 用 `owner := ancestor.Owner()` 取配额持有者，
// 而 fs/dbfs/file.go:145 的 Owner() 会沿 Parent 链向上找第一个非 nil 的
// OwnerModel。因此把团队节点的 OwnerModel 设为系统账号后：
//
//	配额归属（who pays）     = users.storage → 系统账号 id=18（团队共享池）
//	实际上传者（who did it） = entities.created_by → 真实用户
//
// 两个维度分离：团队空间是一个共享容量池（组 max_storage 决定总额），
// 同时每个文件的实际上传者仍被完整记录，审计信息不丢失。
//
// 【为什么不用请求者】若沿用请求者，配额会记到上传者的个人空间上：
// （1）个人 1GB 成为团队空间的实际上限，且「A 传满不影响 B」，
//
//	团队配额形同虚设；（2）第 0 步创建的系统账号完全不参与，
//	其 storage 恒为 0，失去存在意义。
//
// 【失败处理】系统账号未初始化（旧库/迁移未跑）或加载失败时，
// 返回错误而非静默回退到请求者 —— 静默回退会导致配额记到个人头上，
// 而这正是本函数要避免的、且无法事后回滚的错误。
func (t *teamNavigator) resolveOwner(ctx context.Context) (*ent.User, error) {
	return t.resolveOwnerWith(ctx, t.userClient)
}

// resolveOwnerWith 用【指定】的 userClient 解析团队空间所有者。
//
// 存在的唯一理由：让调用方能传入 tx-bound 的 client。
// 直接用 t.userClient 在事务内会另开连接 -> SQLite 单连接死锁（已实测）。
func (t *teamNavigator) resolveOwnerWith(ctx context.Context, uc inventory.UserClient) (*ent.User, error) {
	if t.ownerResolved {
		return t.owner, nil
	}

	// 必须显式要求加载 group 边：dbfs.Capacity 用 u.Edges.GroupOrErr()
	// 读取组的 max_storage，未预加载会直接报错。
	// 与 my_navigator.go:89 / share_navigator.go:116 的既有写法一致。
	loadCtx := context.WithValue(ctx, inventory.LoadUserGroup{}, true)

	sysUser, err := inventory.SystemTeamAccount(loadCtx, uc.GetClient())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve team space owner: %w", err)
	}

	// SystemTeamAccount 只按 ID 查库，不预加载 group 边，这里补一次。
	sysUser, err = uc.GetByID(loadCtx, sysUser.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load team space owner: %w", err)
	}

	t.owner, t.ownerResolved = sysUser, true
	return t.owner, nil
}

func (t *teamNavigator) Recycle() {
	if t.root != nil {
		t.root.Recycle()
		t.root = nil
	}
}

// PersistState / RestoreState：团队空间无跨请求可变状态，
// 与 sharedWithMeNavigator 一致留空。
func (n *teamNavigator) PersistState(kv cache.Driver, key string) {}

func (n *teamNavigator) RestoreState(s State) error { return nil }

// To 把 URI 解析为 *File。
//
//	cloudreve://team          → 虚拟根
//	cloudreve://team/<projID> → 某项目的虚拟目录节点
//	更深的路径                 → ErrPathNotExist（第 1 步项目下无子层级）
func (t *teamNavigator) To(ctx context.Context, path *fs.URI) (*File, error) {
	if inventory.IsAnonymousUser(t.user) {
		return nil, ErrLoginRequired
	}

	elements := path.Elements()

	// 根：cloudreve://team
	if len(elements) == 0 {
		if t.root == nil {
			owner, err := t.resolveOwner(ctx)
			if err != nil {
				return nil, err
			}
			t.root = t.buildRoot(owner)
		}
		return t.root, nil
	}

	// 第一段必须是合法项目 ID
	projectID, err := strconv.Atoi(elements[0])
	if err != nil {
		return nil, fs.ErrPathNotExist.WithError(fmt.Errorf("invalid project id %q", elements[0]))
	}

	project, err := t.teamClient.GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, fs.ErrPathNotExist.WithError(err)
	}

	// 可见性：非成员不得进入。
	// 即使猜到项目 ID 也访问不到，与「我参与的项目」列表口径一致。
	if !t.visible(ctx, project) {
		return nil, fs.ErrPathNotExist.WithError(fmt.Errorf("project %d not visible to user %d", projectID, t.user.ID))
	}

	root, err := t.To(ctx, newTeamUri())
	if err != nil {
		return nil, err
	}

	owner, err := t.resolveOwner(ctx)
	if err != nil {
		return nil, err
	}

	// 只有项目一层 -> 返回虚拟项目节点（用于列表展示项目名）。
	if len(elements) == 1 {
		return t.buildProjectFile(root, project, owner), nil
	}

	// 项目下有更深路径 -> 解析到项目的**真实根目录行**，再逐层定位。
	//
	// 方案 A：ensureProjectRoot 返回真实目录行，之后走既有的按名字逐层查找，
	// 因此任意深度都可用，且整条 dbfs 链路直接复用。
	folder, err := t.ensureProjectRoot(ctx, projectID, owner)
	if err != nil {
		return nil, err
	}

	// 逐层解析，同时构造【父子链】与对外 URI（cloudreve://team/<pid>/<seg>...）。
	//
	// ⚠️ 关键：路径的最后一段**允许不存在**，此时返回"最近的存在祖先"。
	// 这是 dbfs 的既有约定 —— getFileByPath 的注释就写着
	// "Get most recent ancestor or target file"（upload.go:95），
	// 上传/新建时目标文件本就尚不存在，调用方靠 ancestor + 剩余路径段来落位。
	// 若这里对不存在的末段直接报 ErrPathNotExist，上传将无法新建文件。
	//
	// 父子链的意义：Uri(true)/RootUri() 依赖 Parent 上溯，锁定路径基于它计算。
	//
	// ⚠️ 关键契约（由 RootUri() 的消费方决定，见 manage.go:85 / upload.go:144）：
	//     lockedPath := ancestor.RootUri().JoinRaw(path.PathTrimmed())
	// PathTrimmed() 返回的是**从 FS 根算起的完整路径**（如 "2/<proj>/a.bin"），
	// 因此 RootUri() 必须回到 **FS 根**（cloudreve://team），而**不是**项目根。
	// 若回到项目根（cloudreve://team/2），就会拼出
	//     cloudreve://team/2/2/<proj>/a.bin        ← 项目段重复
	// 表现为删除/移动时 Lock conflict 里的畸形锁路径。
	//
	// 同时 UserRoot() 依赖 IsUserRoot 标记，**必须**在链上有且仅有一个节点置位，
	// 否则 UserRoot() 返回 nil，RootUri() 里 .Uri(true) 直接空指针 panic
	// （实测：file.go:182 <- file.go:248 <- manage.go:85）。
	baseUri := newTeamProjectUri(projectID)
	fsRoot := t.teamFsRoot(owner)
	cur := t.decorateRealFile(ctx, fsRoot, folder, baseUri, owner)

	uri := baseUri
	for i, seg := range elements[1:] {
		child, err := t.fileClient.GetChildFile(ctx, cur.Model, owner.ID, seg, true)
		if err != nil {
			if !ent.IsNotFound(err) {
				return nil, fs.ErrPathNotExist.WithError(err)
			}

			// 末段不存在 -> 返回已解析到的祖先（cur），由调用方在其下创建。
			// 中间段不存在 -> 说明路径本身非法，交回调用方按既有逻辑处理
			// （manage.go 的 noChainedCreation 会据此拒绝或逐级创建）。
			if i == len(elements[1:])-1 {
				return cur, nil
			}
			return nil, fs.ErrPathNotExist.WithError(err)
		}
		uri = uri.Join(seg)
		cur = t.decorateRealFile(ctx, cur, child, uri, owner)
	}

	// 【2026-10-04 第 3 步】记录本次导航的目标节点，供 Capabilities() 计算写位。
	// 放在这里是因为 To() 是解析目标节点的唯一出口；
	// 后续 decorateRealFile / Children 都会经 t.Capabilities() 取能力位。
	t.node = cur

	return cur, nil
}

// teamFsRoot 构造团队文件系统的**根节点**（合成节点，不绑定 files 行）。
//
// 它是整条父子链上**唯一** IsUserRoot=true 的节点，因此：
//   - UserRoot() 沿 Parent 上溯能终止于此（否则返回 nil，RootUri() 空指针）
//   - RootUri() 回到 cloudreve://team，与 PathTrimmed() 的「从 FS 根起算」配套
//
// 注意：Path[user] 也置为同一 FS 根，使 Uri(false) 在根节点上语义自洽。
func (t *teamNavigator) teamFsRoot(owner *ent.User) *File {
	f := newFile(nil, &ent.File{Name: inventory.RootFolderName, Type: int(types.FileTypeFolder)})
	f.Path[pathIndexRoot], f.Path[pathIndexUser] = newTeamUri(), newTeamUri()
	f.OwnerModel = owner
	f.IsUserRoot = true
	f.CapabilitiesBs = t.Capabilities(false).Capability
	return f
}

// decorateRealFile 把真实 files 行包装为对外节点，挂在 root 之下（root 可为 nil）。
//
// pathIndexUser 由调用方给出（对外 URI，cloudreve://team/<pid>/...），
// pathIndexRoot 交给 newFile 逐级传播 —— 链条顶端是 teamFsRoot
// （唯一 IsUserRoot，Path[root] = cloudreve://team），
// 因此后代 Path[root] 自然形如 cloudreve://team/<pid>/<...>，与
// PathTrimmed() 的「从 FS 根起算」一致，RootUri() 也回到 FS 根。
//
// ⚠️ 不要在这里把 Path[root] 固定成项目根：那样 RootUri() 会返回
// cloudreve://team/<pid>，与已含项目段的 PathTrimmed() 拼出重复项目段，
// 产生畸形锁路径（cloudreve://team/2/<proj>/2/<proj>/a.bin）。
//
// ⚠️ 也不要在这里设 IsUserRoot：只有 teamFsRoot 是根。
// 若每个节点都置位，RootUri() 会返回节点自身路径，同样导致重复拼接。
//
// ============================================================
// ★ 2026-10-05：本函数是 parent 能力位的来源 —— 之前这里设的是导航器常量
//
// 背景：`DBFS.List` 返回的 `res.Parent` 就是【传进来的 parent】，
// 而 parent 是 `To()` 通过本函数一层层 decoration 出来的。
// 也就是说：
//   · 被列出的【子项】经 applyNodeCapabilities 逐节点判定 ✅
//   · 而【当前目录自己】(= parent) 从没经过那条路 ❌
// 于是 parent.capability 一直是导航器常量。
//
// 为什么这个洞很关键（Peer 2026-10-05 指出）：
//   前端【当前目录的操作按钮】读的正是 parent.capability ——
//   useActionDisplayOpt.ts:119-122:
//       const parentCap = new Boolset(parent.capability);
//       display.showCreateFolder = parentCap.enabled(create_file) && ...
//       display.showCreateFile   = display.showCreateFolder && ...
//       display.showUpload       = display.showCreateFile
//   => 只修 Files 数组、不修 parent，用户看到的仍然是
//      "按钮在、点了 403" —— 正是要修的那个病。
//
// 修法与 applyNodeCapabilities 同源：逐节点解析，不是一律取常量。
// ctx 由调用方传入；为 nil 时回落常量（保持旧行为，不引入新崩溃点）。
// ============================================================
func (t *teamNavigator) decorateRealFile(ctx context.Context, root *File, target *ent.File, uri *fs.URI, owner *ent.User) *File {
	f := newFile(root, target)
	f.Path[pathIndexUser] = uri
	f.OwnerModel = owner
	f.CapabilitiesBs = t.resolveNodeCapability(ctx, target)
	return f
}

// resolveNodeCapability 解析【单个节点】的能力位（parent 走这条路）。
//
// 与 applyNodeCapabilities 的差别只是"一个节点 vs 一批节点"，
// 判定逻辑共用 nodeCapabilityOf + 同一套快路径判据，保证两条路一致。
//
// 失败一律回落常量（ACL 是附加能力，不允许它把文件系统操作弄挂）。
func (t *teamNavigator) resolveNodeCapability(ctx context.Context, target *ent.File) *boolset.BooleanSet {
	fallback := t.Capabilities(false).Capability
	if ctx == nil || target == nil || t.aclClient == nil || t.user == nil {
		return fallback
	}
	if t.isGlobalAdmin() {
		return teamNavigatorWriteCapability
	}

	hasAny, err := t.anyAclExists(ctx)
	if err != nil {
		t.logAclFallback("anyAclExists(parent)", err)
		return fallback
	}
	if !hasAny {
		// 快路径：全库无 ACL -> 必然不存在子级 ACL，直接放行
		return teamNavigatorWriteCapability
	}

	acl := t.aclClientForCtx(ctx)
	groupID := 0
	if t.user.Edges.Group != nil {
		groupID = t.user.Edges.Group.ID
	}

	resolved, err := acl.ResolveFilePermissionsBatch(ctx, []int{target.ID}, t.user.ID, groupID)
	if err != nil {
		t.logAclFallback("ResolveFilePermissionsBatch(parent)", err)
		return fallback
	}

	return t.nodeCapabilityOf(resolved[target.ID])
}

// visible 判断当前用户能否看到该项目。
//
// 口径与 service/team/types.go:409-432 的 checkAccess 对齐：
//
//  1. 项目 owner          → 可见
//  2. 项目成员表有记录    → 可见
//  3. Cloudreve 全局管理员 → 可见（可管理任意项目，保持一致）
func (t *teamNavigator) visible(ctx context.Context, project *ent.TeamProject) bool {
	if project.OwnerID == t.user.ID {
		return true
	}

	if member, err := t.teamClient.GetMember(ctx, project.ID, t.user.ID); err == nil && member != nil {
		return true
	}

	return t.isAdmin()
}

// isAdmin 判断是否为 Cloudreve 全局管理员。
// 与 service/team/types.go:25 的 isGlobalAdmin 同口径：查用户组 IsAdmin 位。
func (t *teamNavigator) isAdmin() bool {
	if t.user.Edges.Group == nil {
		return false
	}

	return t.user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin))
}

// buildRoot 构造团队空间虚拟根。
//
// 与 sharedWithMeNavigator.To 一致（sharewithme_navigator.go:70-85）：
// 构造不存在的虚拟节点，Path 指向 cloudreve://team。
// 团队空间没有对应的 ent.File 记录，故 Model 用合成对象承载展示字段。
//
// owner 为配额归属者（团队共享池的系统账号），**不是**请求者 t.user ——
// 见 resolveOwner 的说明。
func (t *teamNavigator) buildRoot(owner *ent.User) *File {
	rootPath := newTeamUri()
	f := newFile(nil, &ent.File{
		Type: int(types.FileTypeFolder),
		Name: "",
		// OwnerID 必须与 OwnerModel 一致：
		//   OwnerModel  -> 供 Owner() 读语义（所有者校验 / 配额归属）
		//   Model.OwnerID -> 供落库写语义（manage.go:99、file.go:802 均读它写 files.owner_id）
		// 两者分歧会导致「写被拒」或「写出 owner_id=0 的非法行（外键失败）」。
		OwnerID: owner.ID,
	})
	f.Path[pathIndexRoot], f.Path[pathIndexUser] = rootPath, rootPath
	f.OwnerModel = owner
	f.IsUserRoot = false
	f.CapabilitiesBs = t.Capabilities(false).Capability
	return f
}

// projectRootName 返回项目根目录行的 name。
//
// 【为什么用项目 ID 而非 project.Name】
// 路径段已经是项目 ID（cloudreve://team/<pid>）。若目录名用 project.Name，
// 就会出现「路径段是 ID、目录名是名字」的双重标识，改名时还要同步改目录名 ——
// 多一条会失效的链路。用 ID 做 name，路径稳定、改名不影响任何东西。
//
// 【展示名不受影响】cloudreve://team 那一层仍由虚拟节点提供项目名
// （读 team_projects.name）。存储层用 ID、展示层用名字。
func projectRootName(projectID int) string {
	return strconv.Itoa(projectID)
}

// ensureProjectRoot 返回项目对应的**真实根目录行**（方案 A）。
//
// 【为什么需要真实目录】团队根与项目目录原本是纯虚拟节点，数据库里没有对应行，
// 因此 cloudreve://team/<pid>/xxx 无法解析 —— To() 只能处理"根"和"根+一层项目ID"，
// 再深一层就没有 files 行可查（实测 POST /api/v4/file/create 触发 nil pointer panic）。
//
// 方案 A 给每个项目建一个真实目录行，之后整条 dbfs 链路
// （Create/Upload/Delete/Children/配额/所有者校验）全部直接复用，
// 不需要为团队空间写任何特殊的文件操作。
//
// ⚠️【幂等必须自己做，不能依赖 CreateFolder 的 OnConflict】
// inventory.CreateFolder 内部是
//
//	OnConflict(file_children, name).Ignore()
//
// （inventory/file.go:1075），但唯一索引 file_file_children_name 建在
// (file_children, name) 上，而**根目录的 file_children 是 NULL**。
// SQLite 中 NULL 彼此不相等，唯一索引对全 NULL 行不生效 ——
// 因此该 OnConflict **对顶层目录完全无效**，重复调用会建出重复行（实测：2 行 '2'）。
//
// 所以这里必须先按 (owner_id, file_children IS NULL, name) 显式查询。
// 用 CreateFolder(root=folderRoot) 也不行：那会把项目目录嵌到别的目录下，
// 与"每个项目一个顶层目录"的模型不符。
func (t *teamNavigator) ensureProjectRoot(ctx context.Context, projectID int, owner *ent.User) (*ent.File, error) {
	name := projectRootName(projectID)

	// 1. 显式查存在性（NULL-safe：用 IsNull 而非 = NULL）
	existing, err := t.fileClient.GetClient().File.
		Query().
		Where(
			file.OwnerID(owner.ID),
			file.Name(name),
			file.TypeEQ(int(types.FileTypeFolder)),
			file.FileChildrenIsNil(),
		).
		First(ctx)
	if err == nil {
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("failed to look up team project root %q: %w", name, err)
	}

	// 2. 创建。Owner 用系统账号，使其下所有文件继承 owner_id=系统账号。
	folder, err := t.fileClient.CreateFolder(ctx, nil, &inventory.CreateFolderParameters{
		Owner: owner.ID,
		Name:  name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create team project root %q: %w", name, err)
	}

	// 3. 并发兜底：另一端可能同时创建了。回查并按 id 取最小的一行，
	//    保证两端收敛到同一行（多余的重复行由下面的清理逻辑处理）。
	all, err := t.fileClient.GetClient().File.
		Query().
		Where(
			file.OwnerID(owner.ID),
			file.Name(name),
			file.TypeEQ(int(types.FileTypeFolder)),
			file.FileChildrenIsNil(),
		).
		Order(ent.Asc(file.FieldID)).
		All(ctx)
	if err == nil && len(all) > 0 {
		return all[0], nil
	}

	return folder, nil
}

// listRealFolder 列出真实目录节点 parent 的子项，并把对外 URI 重写到 base 之下。
//
// base 形如 cloudreve://team/<pid>/...；每个子项的 URI 为 base.Join(子项名)，
// 因此前端拿到的是团队空间的路径，而不是数据库里的用户空间路径。
// 子项挂在 parent 之下，Uri(true) 沿 Parent 链推导（见 teamSubtreeRoot 说明）。
func (t *teamNavigator) listRealFolder(ctx context.Context, parent *File, base *fs.URI,
	owner *ent.User, args *ListArgs) (*ListResult, error) {
	res, err := t.fileClient.GetChildFiles(ctx, &inventory.ListFileParameters{
		PaginationArgs: args.Page,
		MixedType:      true,
	}, owner.ID, parent.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to list team folder children: %w", err)
	}

	files := make([]*File, 0, len(res.Files))
	for _, m := range res.Files {
		files = append(files, t.decorateRealFile(ctx, parent, m, base.Join(m.Name), owner))
	}

	// S0b：把本页每个节点的能力位按【该节点自身】生效的 ACL 逐个设置。
	// 一次批量解析（递归 CTE），不是逐节点查询。
	// 失败时回落放行 —— ACL 是附加能力，绝不允许它把列表弄挂。
	if err := t.applyNodeCapabilities(ctx, files, res.Files, owner); err != nil {
		t.logAclFallback("applyNodeCapabilities", err)
	}

	return &ListResult{Files: files, Pagination: res.PaginationResults}, nil
}

// buildProjectFile 构造项目虚拟目录节点。owner 同 buildRoot。
func (t *teamNavigator) buildProjectFile(parent *File, project *ent.TeamProject, owner *ent.User) *File {
	f := newFile(nil, &ent.File{
		Type: int(types.FileTypeFolder),
		Name: project.Name,
		// 见 buildRoot 的说明：OwnerID 与 OwnerModel 必须一致。
		OwnerID: owner.ID,
	})
	uri := newTeamProjectUri(project.ID)
	f.Path[pathIndexRoot], f.Path[pathIndexUser] = uri, uri
	f.Parent = parent
	f.OwnerModel = owner
	f.CapabilitiesBs = t.Capabilities(false).Capability
	return f
}

// Children 列出父节点下的内容。
//
//	根       → 我参与的项目（动态）
//	项目节点 → 该项目真实根目录下的内容（方案 A）
//	深层节点 → 该真实目录下的内容
//
// 【空状态】目录为空时返回空切片而非 nil —— 前端据此渲染「暂无文件」，
// 而不是报错或空白。
func (t *teamNavigator) Children(ctx context.Context, parent *File, args *ListArgs) (*ListResult, error) {
	if inventory.IsAnonymousUser(t.user) {
		return nil, ErrLoginRequired
	}

	// 项目节点 / 深层真实节点：列出其真实目录下的内容。
	//
	// 判定顺序很重要 —— 必须先判"项目节点"，再判"是否绑定真实行"：
	//   · cloudreve://team          -> 虚拟根（Model.ID==0，Model==合成对象）
	//   · cloudreve://team/<pid>    -> 虚拟项目节点（Model.ID==0），
	//                                  但它的内容在**真实项目根目录行**下
	//   · cloudreve://team/<pid>/.. -> 真实节点（Model.ID>0）
	// 若只按 Model.ID>0 分流，项目节点会落进"根节点"分支，
	// 于是列出项目列表而非该项目的文件（实测 L2 失败：返回了项目自己）。
	if parent != nil && t.isProjectNode(parent) {
		projectID, err := strconv.Atoi(parent.Path[pathIndexUser].Elements()[0])
		if err != nil {
			return nil, fs.ErrPathNotExist.WithError(err)
		}

		owner, err := t.resolveOwner(ctx)
		if err != nil {
			return nil, err
		}

		// 取（或建）该项目对应的真实根目录行，再列其子项。
		folder, err := t.ensureProjectRoot(ctx, projectID, owner)
		if err != nil {
			return nil, err
		}

		// 挂到 FS 根之下：newFile 由此把 Path[root] 传播为
		// cloudreve://team/<pid>，与 To() 的链条形态一致。
		baseUri := parent.Path[pathIndexUser]
		realRoot := t.decorateRealFile(ctx, t.teamFsRoot(owner), folder, baseUri, owner)

		return t.listRealFolder(ctx, realRoot, baseUri, owner, args)
	}

	// 深层真实节点：直接列其子项。
	// parent 已带完整父子链（由 To() 构造），故 Uri 推导天然正确。
	if parent != nil && parent.Model != nil && parent.Model.ID > 0 {
		owner, err := t.resolveOwner(ctx)
		if err != nil {
			return nil, err
		}

		return t.listRealFolder(ctx, parent, parent.Path[pathIndexUser], owner, args)
	}

	// 根节点：列出我参与的项目
	projects, err := t.teamClient.ListProjects(ctx, t.user.ID, false)
	if err != nil {
		return nil, err
	}

	root, err := t.To(ctx, newTeamUri())
	if err != nil {
		return nil, err
	}

	owner, err := t.resolveOwner(ctx)
	if err != nil {
		return nil, err
	}

	files := make([]*File, 0, len(projects))
	for _, p := range projects {
		files = append(files, t.buildProjectFile(root, p, owner))
	}

	return &ListResult{Files: files}, nil
}

// isProjectNode 判断给定 File 是否为「项目目录」节点（而非团队根）。
//
// ⚠️ 必须要求**恰好一层**路径段（cloudreve://team/<pid>）。
// 若只判断 len(elements) > 0，则 cloudreve://team/2/team-folder-1 这类
// 深层真实节点也会被误判成项目节点，于是 Children 会去列项目根的内容 ——
// 表现为「进入任何子目录都看到同一批文件」（实测 L3/L4 均返回 3 条）。
func (t *teamNavigator) isProjectNode(f *File) bool {
	if f == nil || f.Path[pathIndexUser] == nil {
		return false
	}

	uri := f.Path[pathIndexUser]
	return uri.FileSystem() == constants.FileSystemTeam && len(uri.Elements()) == 1
}

// Capabilities 返回本导航器的能力集。
//
// 【2026-10-04 第 3 步】从「静态常量」改为「按 (用户, 文件) 的 ACL 计算结果」。
//
// 为什么必须动态化：ACL 生效后，后端 aclGuard 会按文件 ACL 拒绝只读主体的写操作，
// 但若这里仍返回同一份静态能力集，前端拿到的 capability 依旧说"可写"，
// 于是按钮照常显示、点了报错。**前端只看得见 capability，看不到 aclGuard。**
//
// 规则（对应 §0.5 的逐位表）：
//
//	· 读类位（ListChildren/EnterFolder/Info/DownloadFile/GenerateThumb）
//	  一律为真 —— 只读成员必须仍能进入目录、否则他连看都看不到。
//	· 写类位（CreateFile/UploadFile/RenameFile/DeleteFile/LockFile）
//	  仅在【当前用户对当前文件具备写权限】时为真。
//
// ⚠️ 只读位恒真 + 写位动态，是本函数的**安全边界**：
//
//	若把读位也交给 ACL，只读成员会因 ListChildren=false 而完全无法访问。
func (t *teamNavigator) Capabilities(isSearching bool) *fs.NavigatorProps {
	// config 为 nil 时（构造不完整/测试夹具）不 panic：分页上限退化为 0
	// 会让调用方按默认处理，优于空指针崩溃。
	maxPageSize := 0
	if t.config != nil {
		maxPageSize = t.config.MaxPageSize
	}

	res := &fs.NavigatorProps{
		Capability:            teamNavigatorCapability,
		OrderDirectionOptions: fullOrderDirectionOption,
		OrderByOptions:        fullOrderByOption,
		MaxPageSize:           maxPageSize,
	}

	// 团队根节点没有对应项目，无法做 ACL 判定；保持只读位可用、写位关闭。
	// （根节点本身不可写，写操作都发生在项目及其子项上。）
	//
	// 【2026-10-04】判定对象优先用 setCapabilityTarget 记下的 project id；
	// 若为空则回退到 To() 解析出的 node（兼容直接调用 Capabilities 的场景）。
	if t.user != nil {
		if t.canWriteForProject() {
			res.Capability = teamNavigatorWriteCapability
		}
	}

	if isSearching {
		res.OrderByOptions = searchLimitedOrderByOption
	}

	return res
}

// setCapabilityTarget 在能力位检查之前，把本次操作的目标路径告知 navigator。
//
// 【为什么需要】
// dbfs.go:790 的能力位检查发生在 getNavigator 【入口】，
// 此时尚未 To()，navigator 不知道目标是谁；而 ACL 判定必须知道目标。
// 没有这一步，团队能力位会永远落在只读集上（实测：写入口对所有人关闭）。
//
// 【判定对象取「项目根」而不是路径自身】
// ACL 规则的载体是项目（cloudreve://team/<pid>），成员关系也是按项目记的
// （team_members.project_id）。路径自身可能是不存在的文件（新建），
// 也可能是深层子目录，但它们的可写性都由所属项目的规则决定。
// 取项目根可命中项目级规则并经由 inherit 生效，且对"尚不存在的目标"也成立。
//
// path 形如 cloudreve://team/<pid>[/...]；取第一段作为项目 id。
// setCapabilityTarget 记录「本次能力位判定针对哪个项目」。
//
// 【为什么必须零查库 —— 2026-10-04 实测死锁】
//
// 本函数由 getNavigator 调用，而 getNavigator 在 f.mu 持有期间执行
// （dbfs.go:749 Lock / 750 defer Unlock）。
// 更致命的是：上传路径上 getNavigator 会被【事务内】再次调用
// （upload.go:90 在 line 233 的 WithTx 之后仍有调用链），
// 此时若在此处发起 SELECT：
//
//	Tx(...): started
//	Tx(...).Exec: UPDATE users SET storage=...   ← ReserveStorage
//	driver.Query: SELECT files ...               ← 本函数的查库，卡住
//	（16 秒后）Tx(...): rollbacked               ← 请求超时回滚
//
// SQLite 是单连接：事务未结束时再发起查询会永久等待。
// **实测复现**：团队空间上传 16s 超时（个人空间 0.3s 正常），
// 关掉本函数的查库调用后立即恢复。
//
// 【修法】只记录 project id（纯内存），把「查 files 行」推迟到真正需要时，
// 且查询发生在事务之外。canWriteNode 因此改为直接对【项目根】判定：
// 项目根不存在时（新项目尚未建根）视为「无 ACL」-> 放行。
func (t *teamNavigator) setCapabilityTarget(ctx context.Context, path *fs.URI) {
	// 保存请求 ctx：canWriteProjectID 需要它来继承事务（见字段注释）。
	// 本函数自身仍然【零查库】。
	t.capabilityCtx = ctx

	if path == nil || path.FileSystem() != constants.FileSystemTeam {
		t.capabilityProjectID = 0
		return
	}

	elements := path.Elements()
	if len(elements) == 0 {
		// 团队根（cloudreve://team）：无项目，不可写
		t.capabilityProjectID = 0
		return
	}

	pid, err := strconv.Atoi(elements[0])
	if err != nil {
		t.capabilityProjectID = 0
		return
	}
	t.capabilityProjectID = pid
}

// findProjectRoot 只读地查找项目根文件行，不存在时返回 (nil, nil)。
//
// 与 ensureProjectRoot 的区别：本函数【绝不写库】。
// 能力位计算可能发生在只读请求上，不能有副作用。
func (t *teamNavigator) findProjectRoot(ctx context.Context, projectID int, owner *ent.User) (*ent.File, error) {
	return t.findProjectRootWith(ctx, t.fileClient, projectID, owner)
}

// findProjectRootWith 用【指定】的 fileClient 查项目根。
//
// 存在的唯一理由：让调用方能传入 tx-bound 的 client。
// 直接用 t.fileClient 在事务内会另开连接 -> SQLite 单连接死锁（已实测）。
func (t *teamNavigator) findProjectRootWith(ctx context.Context, fc inventory.FileClient,
	projectID int, owner *ent.User) (*ent.File, error) {
	existing, err := fc.GetClient().File.
		Query().
		Where(
			file.OwnerID(owner.ID),
			file.Name(projectRootName(projectID)),
			file.TypeEQ(int(types.FileTypeFolder)),
			file.FileChildrenIsNil(),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return existing, nil
}

// canWriteNode 判断当前用户对给定节点是否具备写权限。
//
// 【为什么与 aclGuard 分离而不是复用它】
// aclGuard 是**请求级**判定：它读 ctx（含事务继承）并在拒绝时返回错误。
// 本函数是**能力位计算**：只回答「这个用户对这个文件能不能写」这一个布尔问题，
// 用于填充 capability，不产生错误、不参与事务。
//
// ⚠️⚠️ 判定口径必须与 aclGuard 【完全一致】，包括三处回落方向。
// 这一条曾经写错过，记在这里以免重犯：
//
//	错误写法：aclClient==nil / err!=nil / resolved==nil 一律 return false
//	错误理由：「aclGuard 在无 ACL 时放行、由调用方【原有的所有者判定】接管；
//	           团队文件 owner 恒为系统账号、写者恒非 owner，
//	           故原有的所有者判定必然是拒绝，所以这里取不可写才一致。」
//
//	上述理由的【前提是错的】：团队写路径上的所有者判定【已被旁路】——
//	那正是第 2 步让团队写入成为可能的原因。
//	manage.go:25 / upload.go:22,83,331,444 都在路径上调用
//	withTeamOwnerBypassIfTeam，于是 manage.go:74 的条件
//	    `!ok && ancestor.Owner().ID != f.user.ID`
//	因 ok=true 而为假，【不返回 ErrOwnerOnly】。
//	=> 无 ACL 时，后端「原有判定」的结果是【允许】，故本函数必须同样返回 true。
//
//	A/B 实测也直接印证：S0a 之前的构建（A 侧）在团队空间 0 条 ACL 时
//	create/rename/delete 全部 code=0 —— 那就是「回落值」的真身。
//
//	写错时的实际后果：不是"更保守"，而是**在默认情况下（0 条 ACL）把
//	团队写入口对所有人关闭**，并让 L1/L2 的"逐字节一致"无法验证
//	（B 侧根本没走到写路径）。"保守"若导致系统性判错，那不是保守。
//
// 三处回落与 acl.go 的对应关系（acl.go:23-26 是契约原文）：
//
//	aclClient == nil  -> acl.go:61   return nil（放行）  -> 这里 return true
//	err != nil        -> acl.go:112-117 return nil（放行）-> 这里 return true
//	resolved == nil   -> acl.go:120-122 return nil（放行）-> 这里 return true
//	命中 read         -> acl.go:125-132 返回 ErrAclDenied -> 这里 return false
//	命中 write        -> acl.go:134  return nil          -> 这里 return true
//
// 权限边界说明：非成员进不了团队空间靠的是 visible()/navigator 投影，不是 ACL。
// 因此「无 ACL 即可写」只对【能进项目的人】成立 —— 与第 2 步的既定状态一致。
// canWriteForProject 计算「当前主体能否写本次操作所属的项目」。
//
// 判定对象是**项目根文件行**（ACL 规则的载体），而不是路径自身：
// 路径可能是不存在的文件（新建/上传），此时没有 files 行可查；
// 而 ACL 挂在项目根上，经 inherit 对所有子项生效。
//
// 【与 setCapabilityTarget 的分工 —— 为避开实测死锁】
// ============================================================
// S0b：把能力位从【项目级】细化到【节点级】
//
// 【为什么要改】
// 用户需求是「给共享文件（夹）设置权限」。S0a 之后能力位只按【项目根】判定，
// 于是：
//   ACL 在项目根 -> capability = 只读集 gAaI -> 前端隐藏写入口 ✅
//   ACL 在子级   -> capability = 可写集 w8aI -> 前端【显示】写入口 ❌
//                                          -> 点了 aclGuard 403（体验割裂）
// 根因：四处赋值都取 t.Capabilities(false).Capability，
//       而它的判据是 t.capabilityProjectID（整个请求一个项目 id），与节点无关。
//
// 【改法】逐节点解析：capability(node) = f(该节点自身生效的 ACL)
// 批量取回由 inventory.ResolveFilePermissionsBatch 完成（一条 CTE，
// 不是 N 次查询），判定逻辑与单节点路径共用同一个纯内存函数。
// ============================================================

// nodeCapabilityOf 返回某个真实文件节点应当携带的能力位。
//
// resolved 为 nil（无任何 ACL 命中）-> 放行，返回可写集。
// 口径与 aclGuard 完全一致：aclGuard 在 resolved==nil 时也放行。
//
// ★ 同项目内不同节点可以拿到不同能力位 —— 这正是 S0b 的验收核心。
func (t *teamNavigator) nodeCapabilityOf(resolved *inventory.ResolvedFilePermission) *boolset.BooleanSet {
	if resolved == nil {
		// 无 ACL 命中 -> 回落放行（与 aclGuard 一致）
		return teamNavigatorWriteCapability
	}
	if resolved.Permission == inventory.FileAclPermissionWrite {
		return teamNavigatorWriteCapability
	}
	// read（或任何非 write 的取值）-> 只读集
	return teamNavigatorCapability
}

// applyNodeCapabilities 为一批真实文件节点【逐个】设置能力位。
//
// files 与 models 一一对应（models 是数据库行，files 是其对外节点）。
// 本函数负责：快路径判断 -> 批量解析 -> 逐节点赋值。
//
// 【快路径】全局没有任何 ACL 时直接返回，不做批量查询。
// 注意判据是【全局 EXISTS】而不是"本项目 ACL 行数 <= 1" ——
// 后者有反例：项目只有 1 条 ACL 且挂在子文件夹上时，"行数<=1"会误判为
// "只有根级 ACL"从而走项目根判定，把 S0b 要修的缺陷原样保留。
// 全局 EXISTS 无此问题：为 0 则必然不存在任何子级 ACL。
func (t *teamNavigator) applyNodeCapabilities(ctx context.Context,
	files []*File, models []*ent.File, owner *ent.User) error {

	if len(files) == 0 || t.aclClient == nil || t.user == nil {
		return nil
	}
	// 全局管理员不受 ACL 限制（与 acl.go / canWriteProjectID 口径一致）
	if t.isGlobalAdmin() {
		for _, f := range files {
			f.CapabilitiesBs = teamNavigatorWriteCapability
		}
		return nil
	}

	// ---- 快路径 ----
	hasAny, err := t.anyAclExists(ctx)
	if err != nil {
		// ACL 是附加能力，绝不允许它把原有的文件系统操作弄挂。
		// 判定失败时回落到"可写集"，与 aclGuard 的回落方向一致。
		t.logAclFallback("anyAclExists", err)
		return nil
	}
	if !hasAny {
		for _, f := range files {
			f.CapabilitiesBs = teamNavigatorWriteCapability
		}
		return nil
	}

	// ---- 批量路径 ----
	ids := make([]int, 0, len(models))
	for _, m := range models {
		if m != nil {
			ids = append(ids, m.ID)
		}
	}

	acl := t.aclClientForCtx(ctx)
	groupID := 0
	if t.user.Edges.Group != nil {
		groupID = t.user.Edges.Group.ID
	}

	resolved, err := acl.ResolveFilePermissionsBatch(ctx, ids, t.user.ID, groupID)
	if err != nil {
		t.logAclFallback("ResolveFilePermissionsBatch", err)
		return nil
	}

	for i, f := range files {
		if i >= len(models) || models[i] == nil {
			continue
		}
		f.CapabilitiesBs = t.nodeCapabilityOf(resolved[models[i].ID])
	}
	return nil
}

// aclClientForCtx 返回参与当前事务（若在事务内）的 ACL 客户端。
//
// 与 canWriteProjectID 同一模式：不依赖"在不在事务内"这个不变量 ——
// 事务内就绑进当前事务，事务外就用普通连接。
//
// 注意 inventory.InheritTx 的返回：第一个值【已经】是绑定好的客户端
// （内部做了 c.SetClient(txClient)），tx 为 nil 时表示"不在事务内"。
func (t *teamNavigator) aclClientForCtx(ctx context.Context) inventory.FilePermissionClient {
	if ctx == nil {
		return t.aclClient
	}
	bound, tx := inventory.InheritTx(ctx, t.aclClient)
	if tx == nil {
		return t.aclClient
	}
	return bound
}

// anyAclExists 快路径判据：全库是否存在任何一条 ACL。
// 只问"有没有"，不数个数（EXISTS 比 COUNT 快，且语义已足够）。
func (t *teamNavigator) anyAclExists(ctx context.Context) (bool, error) {
	acl := t.aclClientForCtx(ctx)
	return acl.AnyFilePermissionExists(ctx)
}

func (t *teamNavigator) logAclFallback(where string, err error) {
	if t.l != nil {
		t.l.Warning("team capability: %s failed, fallback to writable: %s", where, err)
	}
}

func (t *teamNavigator) isGlobalAdmin() bool {
	return t.user != nil && t.user.Edges.Group != nil &&
		t.user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin))
}

// setCapabilityTarget 只记 project id（零查库）；
// 查库集中在本函数，调用链是 getNavigator -> Capabilities()，
// **不在任何 WithTx 之内**（已核对：upload.go 的 WithTx 在 getNavigator 之后）。
func (t *teamNavigator) canWriteForProject() bool {
	if t.capabilityProjectID <= 0 {
		return false
	}
	return t.canWriteProjectID(t.capabilityProjectID)
}

// canWriteProjectID 对指定项目做写权限判定（查库发生在此处）。
func (t *teamNavigator) canWriteProjectID(projectID int) bool {
	if t.user == nil {
		return false
	}

	// 【Peer 2026-10-04 裁决 · 必读】
	//
	// 我之前在此用 context.Background()，并注释称「查库不在任何 WithTx 之内」。
	// 那句话是【错的】，而且与我自己上一条消息自相矛盾：
	//   · 我曾说「getNavigator 会在事务内被再次调用（upload.go:90）」
	//   · 又在此注释「不在任何 WithTx 之内」
	// 两条不可能同时为真。
	//
	// 更根本的问题：本函数由 Capabilities() 调用，而 Capabilities() 就在
	// getNavigator【内部】（dbfs.go:802）。所以「查库是否在事务内」取决于
	// getNavigator 被谁调用 —— 与我"把查询写在哪一行"毫无关系。
	// v4 实测 0.27s 只说明【这条路径】没触发，不说明结构上安全。
	//
	// 【正确形态：不依赖任何不变量】
	// 用请求 ctx + inventory.InheritTx（与 acl.go:106-109 同一模式）：
	// 事务内就绑进当前事务，事务外就用普通连接。
	// 这样"在不在事务内"不再重要，也就不会有"哪天有人在事务里调了一次"的复发。
	//
	// 顺带修掉 context.Background() 的另两个副作用：
	//   · 丢失请求取消/超时（客户端断开后查询照跑）
	//   · 丢失请求作用域的值（追踪/审计信息带不进来）
	ctx := t.capabilityCtx
	if ctx == nil {
		ctx = context.Background()
	}

	// 1. 全局管理员不受 ACL 限制（与 acl.go:94-98 口径一致）
	if t.user.Edges.Group != nil &&
		t.user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		return true
	}

	// 2. 回落一：ACL 客户端未注入 -> 放行（与 acl.go:61 一致）
	//    ACL 是附加能力，绝不允许它把原有的文件系统操作弄挂。
	if t.aclClient == nil {
		return true
	}

	// ★ 三处查库都必须【参与当前事务】，否则在事务内会死锁。
	//
	// 只有 ResolveFilePermission 原来走的是 InheritTx（那是照抄 acl.go），
	// 而 resolveOwner 与 findProjectRoot 走的是 fileClient/userClient ——
	// 它们【不会】自动继承事务，事务内另开连接 -> SQLite 单连接永久等待。
	//
	// 实测：TestTeamCapabilityInsideTransactionDoesNotDeadlock 在只改
	// ResolveFilePermission 一处时仍然 15s 超时。三处一起改才通。
	fileClient := t.fileClient
	if bound, tx := inventory.InheritTx(ctx, t.fileClient); tx != nil {
		fileClient = bound
	}
	userClient := t.userClient
	if bound, tx := inventory.InheritTx(ctx, t.userClient); tx != nil {
		userClient = bound
	}

	owner, err := t.resolveOwnerWith(ctx, userClient)
	if err != nil || owner == nil {
		return true
	}

	root, err := t.findProjectRootWith(ctx, fileClient, projectID, owner)
	// 3. 回落二：查库失败 -> 放行
	if err != nil {
		return true
	}
	// 4. 回落三：项目根行不存在（项目刚建、尚未落根）-> 无 ACL 可命中 -> 放行
	if root == nil {
		return true
	}

	groupID := 0
	if t.user.Edges.Group != nil {
		groupID = t.user.Edges.Group.ID
	}

	// 与 acl.go:106-109 相同的处理：参与当前事务，不另开连接。
	// SQLite 连接池上限为 1（inventory/client.go 的 SetMaxOpenConns(1)），
	// 事务内另开连接会永久等待，整个请求挂死。
	aclClient := t.aclClient
	if bound, tx := inventory.InheritTx(ctx, t.aclClient); tx != nil {
		aclClient = bound
	}

	resolved, err := aclClient.ResolveFilePermission(ctx, root.ID, t.user.ID, groupID)
	// 5. 回落四：解析失败 -> 放行
	if err != nil {
		return true
	}
	// 6. 回落五：未命中 ACL -> 放行
	//    注意此处【不能】返回 false —— 团队路径的所有者判定已被旁路，
	//    后端在无 ACL 时是允许写入的（见函数头部说明）。
	if resolved == nil {
		return true
	}

	// 7. 命中 ACL -> 以 ACL 为准
	return resolved.Permission == inventory.FileAclPermissionWrite
}

// Walk 递归遍历给定节点（含其所有后代）。
//
// 【为什么第 2 步必须实现它】
// 第 1 步此处是 `return errors.New("not implemented")` 的占位，当时是**正确**的：
// 只读文件系统不会删除任何东西，而 Walk 的唯一调用方就是删除路径。
//
// 但第 2 步开放了 NavigatorCapabilityDeleteFile，**删除必然会经过 Walk**：
//
//	manage.go:909  n.Walk(ctx, files, MaxInt, MaxInt, ...)   // 递归展开待删对象
//	manage.go:969  n.Walk(ctx, files, limit, MaxInt, ...)    // 同上（另一分支）
//
// 两处都是**无条件调用**，与目标是文件还是文件夹无关 ——
// 因此**删单个文件同样会失败**，不只是删文件夹（实测报错
// `failed to walk files: not implemented`）。
//
// 【原注释里的类比是失效的，别再照抄】
// 旧注释称「既有 sharewithme / trash navigator 同样如此」，但对照后发现：
//
//	· sharewithme（sharewithme_navigator.go:118）确实 stub 了 Walk，
//	  但它的能力集**不含 delete_file**（仅 ListChildren/DownloadFile/EnterFolder）
//	  -> 永远不会走到 Walk，stub 是安全的
//	· trash（trash_navigator.go:133）**实现了** Walk（委托 baseNavigator.walk），
//	  因为它要执行真正的永久删除
//	· 只有 team：**既 stub 了 Walk，又开了 delete_file** -> 声明与实现脱节
//
// 教训：**能力位不是一个原子承诺**。开一个能力位时，必须列出该动作实际依赖的
// 实现清单（方法 / 其它能力位 / 前端判定点）并逐条确认；能力位是接口，不是开关。
// 类比是否成立，取决于两边能力集是否相同 —— 第 2 步改了能力集，类比就失效了。
//
// 实现用 baseNavigator.walk（与 my / trash navigator 同一实现），
// 它按层展开并用 GetChildFiles 拉取子项，因此天然复用既有的分页与过滤逻辑。
//
// walk 内部用 `newFile(p, model)` 构造后代，Path[root] 由父节点逐级传播。
// 由于链条顶端是 teamFsRoot（Path[root] = cloudreve://team，唯一 IsUserRoot），
// 后代得到的 Path[root] 恰为 cloudreve://team/<pid>/<...>，
// 与 manage.go:85 的 `RootUri().JoinRaw(PathTrimmed())` 配套（后者从 FS 根起算），
// 因此**无需在这里做任何路径修正** —— 修正逻辑集中在 teamFsRoot / decorateRealFile。
func (t *teamNavigator) Walk(ctx context.Context, levelFiles []*File, limit, depth int, f WalkFunc) error {
	return t.baseNavigator.walk(ctx, levelFiles, limit, depth, f)
}

func (n *teamNavigator) FollowTx(ctx context.Context) (func(), error) {
	if _, ok := ctx.Value(inventory.TxCtx{}).(*inventory.Tx); !ok {
		return nil, fmt.Errorf("navigator: no inherited transaction found in context")
	}
	newFileClient, _, _, err := inventory.WithTx(ctx, n.fileClient)
	if err != nil {
		return nil, err
	}

	oldFileClient := n.fileClient
	revert := func() {
		n.fileClient = oldFileClient
		n.baseNavigator.fileClient = oldFileClient
	}

	n.fileClient = newFileClient
	n.baseNavigator.fileClient = newFileClient
	return revert, nil
}

func (n *teamNavigator) ExecuteHook(ctx context.Context, hookType fs.HookType, file *File) error {
	return nil
}

func (n *teamNavigator) GetView(ctx context.Context, file *File) *types.ExplorerView {
	if view, ok := n.user.Settings.FsViewMap[string(constants.FileSystemTeam)]; ok {
		return &view
	}
	return getDefaultView()
}
