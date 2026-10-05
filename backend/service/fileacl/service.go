package fileacl

import (
	"context"
	"strconv"
	"strings"

	"github.com/cloudreve/Cloudreve/v4/application/constants"
	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/ent"
	entfile "github.com/cloudreve/Cloudreve/v4/ent/file"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	itypes "github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs/dbfs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/manager"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// ============================================================
// 文件 ACL 服务层（契约补遗二 §4）
//
//   GET    /file/permission?uri=…   查看：需要【读】权限
//   PUT    /file/permission         设置：需要对该文件有【管理权限】
//   DELETE /file/permission         删除：同 PUT
//
// 三条接口一律以 Cloudreve 的 uri 定位文件，对内一律换算成整型 file id。
//
// ★ 权限模型分两支（2026-10-05，乙方案）：
//
//	个人/分享（原有）：门1=Share 能力位，门2=owner 或全局管理员
//	                  —— 原逻辑【逐字保留】，只在非 team 分支执行
//	团队 team（新分支）：解析用 Download 能力位（读集有，人人可解析），
//	                  授权完全交给【角色门】：项目 owner /
//	                  team_members.role ∈ {owner, admin} / 全局管理员
//
// ★ 为什么团队不复用 Share 能力位门（Peer 评审通过的方案要点）：
//   Share 是【能力层】概念（这个文件系统支不支持这动作，与谁请求无关），
//   而"谁能改权限"是【主体层】概念。把权限要求表达成能力位缺失，
//   正是改造前的病根：所有人都撞 "action 18 not supported"，
//   连错误信息都驴唇不对马嘴。团队分支直接不设 Share 门，而不是
//   给团队写集补 Share 位 —— 补位会驱动前端渲染出"创建分享"按钮，
//   而创建分享要求 owner==自己、团队文件 owner 是系统账号，点了必失败，
//   等于重新造出"按钮在、点了才拒"的割裂。
//
// ★ owner 不可锁死：团队侧写入前做【写前模拟】（见 guardOwnerLockout），
//   拒绝任何"会让项目 owner 失去可写"的变更；判据是生效结果（走真实
//   解析链，含组行含继承），不是变更内容本身。
//
// ★ 保持可丢弃（Peer 补充 3）：本改动只落这一个文件，不碰数据库结构、
//   不碰任何能力位；若用户改选"先不做"，整体回退本文件即可。
// ============================================================

type (
	// ListPermissionService 查看某个文件上的 ACL（query: uri）
	ListPermissionService struct {
		Uri string `form:"uri" binding:"required"`
	}
	ListPermissionParamCtx struct{}

	// SetPermissionService 设置/覆盖一条 ACL（body）
	SetPermissionService struct {
		Uri         string `json:"uri" binding:"required"`
		SubjectType string `json:"subject_type" binding:"required"`
		// 主体 hashid：subject_type=user 时是用户 hashid，=group 时是用户组 hashid
		SubjectID  string `json:"subject_id" binding:"required"`
		Permission string `json:"permission" binding:"required"`
		// 缺省为 true（契约 §2 默认值）
		Inherit *bool `json:"inherit"`
	}
	SetPermissionParamCtx struct{}

	// DeletePermissionService 删除一条 ACL（body）
	DeletePermissionService struct {
		Uri         string `json:"uri" binding:"required"`
		SubjectType string `json:"subject_type" binding:"required"`
		SubjectID   string `json:"subject_id" binding:"required"`
	}
	DeletePermissionParamCtx struct{}
)

// resolvedTarget 已经解析完成的目标文件（只保留标量，避免依赖 fs.File 的生命周期）
type resolvedTarget struct {
	FileID  int
	OwnerID int
	Name    string
	User    *ent.User
	// ProjectID > 0 表示这是团队空间的目标（由 resolveTeamTarget 填充），
	// 用于把锁死模拟（Set/Delete）限定在团队分支；个人文件恒为 0。
	ProjectID int
}

// isGlobalAdmin 判断当前用户是否为 Cloudreve 全局管理员。
// 口径与团队模块 service/team 的 isGlobalAdmin 完全一致。
func isGlobalAdmin(ctx context.Context) bool {
	u := inventory.UserFromContext(ctx)
	if u == nil || u.Edges.Group == nil {
		return false
	}
	return u.Edges.Group.Permissions.Enabled(int(itypes.GroupPermissionIsAdmin))
}

// resolveTarget 把 uri 解析成内部文件，并按需做能力位校验。
//
// requireWrite = false（查看）：要求读能力（NavigatorCapabilityDownloadFile）。
// requireWrite = true （设置/删除）：要求「管理分享」同级能力位
// （NavigatorCapabilityShare）—— 该能力位只在用户自己的文件系统上开放，
// 因此天然挡住「给别人的文件设 ACL」；非管理员再补一道显式校验。
func resolveTarget(c *gin.Context, uri string, requireWrite bool) (*resolvedTarget, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	if user == nil {
		return nil, serializer.NewError(serializer.CodeCheckLogin, "Login required", nil)
	}

	parsed, err := fs.NewUriFromString(uri)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "unknown uri", err)
	}

	// ★ 团队空间走独立分支（乙方案，评审通过 2026-10-05）。
	//   else 之下是【原逻辑逐字保留】的个人/分享路径，不因本分支存在而改变。
	if parsed.FileSystem() == constants.FileSystemTeam {
		return resolveTeamTarget(c, parsed, requireWrite)
	}

	caps := []dbfs.NavigatorCapability{dbfs.NavigatorCapabilityDownloadFile}
	if requireWrite {
		caps = []dbfs.NavigatorCapability{dbfs.NavigatorCapabilityShare}
	}

	m := manager.NewFileManager(dep, user)

	file, err := m.Get(c, parsed, dbfs.WithRequiredCapabilities(caps...), dbfs.WithNotRoot())
	if err != nil {
		m.Recycle()
		return nil, err
	}
	if file == nil || file.IsNil() {
		m.Recycle()
		return nil, serializer.NewError(serializer.CodeFileNotFound, "File not found", nil)
	}

	// ★ 严格模式（Peer 评审 2026-10-05 追加，只加在【设/删权限】路径上）：
	//   getFileByPath 的既有约定是"解析不到就返回最近存在祖先"
	//   （上传/新建靠它落位 —— 那条路【不要动】）。
	//   但对"设权限"这是毒药：路径打错一个字，权限就静默落在父目录/项目根上，
	//   作用对象 ≠ 请求对象且无任何提示 —— 正是全项目在打的那类病。
	//   因此 requireWrite 时要求【解析结果与请求 uri 精确同一节点】，否则拒绝。
	if requireWrite && !sameTargetElements(parsed, file.Uri(true)) {
		m.Recycle()
		return nil, serializer.NewError(serializer.CodeFileNotFound,
			"The specified path does not exist", nil)
	}

	target := &resolvedTarget{
		FileID:  file.ID(),
		OwnerID: file.OwnerID(),
		Name:    file.Name(),
		User:    user,
	}
	m.Recycle()

	// 非管理员不得给别人的文件设 ACL（契约 §4 权限要求）
	if requireWrite && target.OwnerID != user.ID && !isGlobalAdmin(c) {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr,
			"Only the file owner or a global administrator can manage file ACL", nil)
	}

	return target, nil
}

// ============================ 团队分支（乙方案） ============================

// sameTargetElements 严格模式判据：请求 uri 与解析结果指向【同一路径节点】。
//
// 只比 Elements()（清洗后的路径段序列），刻意【不比】完整 String()/FileSystem：
//  1. 解析结果是沿着请求 uri 导航出来的，文件系统名由构造保证一致，无需重复断言；
//  2. userinfo（分享 uri 的 <id>:<pw>@ 前缀）、尾斜杠等差异不该造成误拒
//     —— path.Clean 已在 Path() 里做过，Elements 天然归一。
// 祖先回落场景（少最后一段）必然段数不等 -> 一票否决，这正是要拦的毒。
func sameTargetElements(requested, resolved *fs.URI) bool {
	if requested == nil || resolved == nil {
		return false
	}
	re, qe := resolved.Elements(), requested.Elements()
	if len(re) != len(qe) {
		return false
	}
	for i := range re {
		if re[i] != qe[i] {
			return false
		}
	}
	return true
}

// resolveTeamTarget 团队空间的解析 + 授权。
//
// 与个人路径的关键差别：
//  1. 解析门只要 Download 能力位（团队读集包含 -> 所有能寻址的成员都能解析），
//     【不查 Share】—— Share 门是个人路径"只能管自己的文件"的替代表达，
//     团队路径用角色门表达同一件事，语义更准（见文件头注释）。
//  2. 归属证据取自【服务端构造的路径】（teamNavigator.decorateRealFile 填的
//     f.Path[pathIndexUser] = cloudreve://team/<pid>/...），
//     客户端传入的 uri 只用于寻址，其项目段不作为归属证据。
//  3. 可见性 = 成员级（owner/成员/全局管理员），与 checkAccess 同序；
//     写入 = 管理级（项目 owner / 角色 owner、admin / 全局管理员）。
//
// 非成员在可见性门返回与"项目不存在"相同的 404（防枚举，沿用回收站口径）。
func resolveTeamTarget(c *gin.Context, parsed *fs.URI, requireWrite bool) (*resolvedTarget, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	if user == nil {
		return nil, serializer.NewError(serializer.CodeCheckLogin, "Login required", nil)
	}

	// ① 项目 ID：客户端 uri 的路径段（寻址输入）。
	//    与 server 路径在 ⑤ 复核 —— 同一个 parsed uri 走出的路径段必然一致，
	//    复核是为了防"装饰层将来改了寻址语义"这类漂移。
	pid := 0
	if els := parsed.Elements(); len(els) > 0 {
		pid, _ = strconv.Atoi(els[0])
	}
	if pid <= 0 {
		return nil, serializer.NewError(serializer.CodeNotFound, "Project not found", nil)
	}
	proj, err := dep.TeamClient().GetProjectByID(c, pid)
	if err != nil || proj == nil {
		// 项目不存在或已软删（ent 软删 mixin 自动过滤）——与个人路径
		// "寻址不到"同码，不泄露"项目存在但你看不到"
		return nil, serializer.NewError(serializer.CodeNotFound, "Project not found", err)
	}

	// ② 可见性门（读）：全局管理员 ∥ 项目 owner ∥ 成员表有记录。
	//    ★ 必须先于 m.Get：非成员对"存在的文件"和"不存在的文件"
	//      必须得到【同一个 404】，否则可以用错误码差异枚举项目内文件。
	role, isMember := teamMemberRole(c, dep, pid, user.ID)
	isGlobal := isGlobalAdmin(c)
	if !isGlobal && proj.OwnerID != user.ID && !isMember {
		return nil, serializer.NewError(serializer.CodeNotFound, "Project not found", nil)
	}

	// ③ 写入门（乙口径）：项目 owner ∥ 角色 ∈ {owner, admin} ∥ 全局管理员
	//
	// ★ 角色用 inventory.TeamRole* 常量（角色矩阵的权威注册表：
	//   inventory/team.go:26-29），不是记忆里的字符串。
	//   生产库现存取值 {owner, member}（实测），常量覆盖 schema 允许的
	//   {owner, admin, member, viewer} —— viewer/member 在此被自然拒绝。
	if requireWrite && !isGlobal && proj.OwnerID != user.ID &&
		role != inventory.TeamRoleOwner && role != inventory.TeamRoleAdmin {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr,
			"只有项目所有者或管理员可以管理团队文件的权限", nil)
	}

	// ④a 项目根（单段 cloudreve://team/<pid>）特殊解析 —— 落【真实根行】。
	//
	// ★ Peer 生产实测的 bug（v18，比严格模式更毒的一条）：
	//   To() 对单段路径返回【虚拟项目节点】（buildProjectFile，展示项目名用，
	//   无 DB 行）—— 于是权限静默写成 file_id=0 的悬挂行：
	//   【看起来成功、永不生效、每次调用多积一条，且不报错】。
	//   严格模式拦不住它：判据是"路径段相等"，而虚拟节点的路径段
	//   确实就是 cloudreve://team/<pid> —— 它比的是路径，没比
	//   "节点有没有真实 DB 行"。
	//   => 权限/查看一律改落在【真实根行】：name=str(pid)、owner=系统账号、
	//      无父目录的 folder 行（ensureProjectRoot 维护的那一条，与
	//      teamtrash.findProjectRoot 同判据）。
	//   连带收益：403 态（根 read ACL -> 成员回收站 403、根能力位只读）
	//   由此变成【真用户路径可达】，不再是 SQL 种子的防御性代码。
	//
	// 根行尚不存在（项目从未被浏览/列过目录）-> 明确报错让用户先打开项目，
	// 【绝不写 file_id=0】。
	// 本分支路径即目标（单段 = 根路径），严格模式判据天然满足，无需再比。
	if len(parsed.Elements()) == 1 {
		sysUID, _, err := inventory.SystemTeamIDs(c, dep.DBClient())
		if err != nil || sysUID == 0 {
			return nil, serializer.NewError(serializer.CodeNotFound, "Project not found", err)
		}
		realRoot, err := dep.DBClient().File.Query().Where(
			entfile.OwnerIDEQ(sysUID),
			entfile.NameEQ(strconv.Itoa(pid)),
			entfile.TypeEQ(int(itypes.FileTypeFolder)),
			entfile.FileChildrenIsNil(),
		).First(c)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, serializer.NewError(serializer.CodeFileNotFound,
					"项目根目录尚未生成，请先打开该项目一次后再设置权限", nil)
			}
			return nil, serializer.NewError(serializer.CodeDBError,
				"Failed to load project root", err)
		}
		return &resolvedTarget{
			FileID:    realRoot.ID,
			OwnerID:   realRoot.OwnerID,
			Name:      realRoot.Name,
			User:      user,
			ProjectID: pid,
		}, nil
	}

	// ④ 解析：Download（读集有，人人可解析）。解析 ≠ 授权。
	//    授权已在 ②③ 完成，这里的错误只反映"路径本身不存在"。
	m := manager.NewFileManager(dep, user)
	file, err := m.Get(c, parsed,
		dbfs.WithRequiredCapabilities(dbfs.NavigatorCapabilityDownloadFile),
		dbfs.WithNotRoot())
	if err != nil {
		m.Recycle()
		return nil, err
	}
	if file == nil || file.IsNil() {
		m.Recycle()
		return nil, serializer.NewError(serializer.CodeFileNotFound, "File not found", nil)
	}

	// ⑤ 归属复核：服务端构造的路径（decorateRealFile 填的
	//    f.Path[pathIndexUser]）第一段必须与 ① 的 pid 一致。
	serverPid := 0
	if u := file.Uri(true); u != nil {
		if els := u.Elements(); len(els) > 0 {
			serverPid, _ = strconv.Atoi(els[0])
		}
	}
	if serverPid != pid {
		m.Recycle()
		return nil, serializer.NewError(serializer.CodeNotFound, "Project not found", nil)
	}

	// ★ 严格模式（与个人分支同一判据、同一理由 —— 见 sameTargetElements 注释）：
	//   To() 对不存在的末段返回"最近存在祖先"（team_navigator.go:415，
	//   dbfs 为上传/新建保留的约定），若不拦，对不存在路径设权限会
	//   静默落在父目录/项目根上。只拦 requireWrite（查看仍允许祖先回落，
	//   上传/新建的依赖完全不受影响）。
	if requireWrite && !sameTargetElements(parsed, file.Uri(true)) {
		m.Recycle()
		return nil, serializer.NewError(serializer.CodeFileNotFound,
			"The specified path does not exist", nil)
	}
	target := &resolvedTarget{
		FileID:    file.ID(),
		OwnerID:   file.OwnerID(),
		Name:      file.Name(),
		User:      user,
		ProjectID: pid,
	}
	m.Recycle()

	return target, nil
}

// teamMemberRole 查成员角色；ok=false 表示非成员（查询失败也按非成员保守处理 ——
// 这里的下游是"拒绝"，把故障算成非成员只会让结果更保守，不会越权）。
func teamMemberRole(c *gin.Context, dep dependency.Dep, projectID, userID int) (role string, ok bool) {
	m, err := dep.TeamClient().GetMember(c, projectID, userID)
	if err != nil || m == nil {
		return "", false
	}
	return m.Role, true
}

// ============================ owner 锁死保护（团队分支写入） ============================

// ownerLockoutMsg 人话文案（Peer 补充 2：用户要能看懂为什么被拒，
// 而不是一个内部错误名）。
const ownerLockoutMsg = "该权限变更会把项目所有者锁在项目之外（所有者必须始终可写），已拒绝。请检查针对所有者本人或其所在用户组的限制。"

// guardOwnerLockout 在【单个事务内】执行团队 ACL 变更并做写前模拟：
//
//	变更前解析 owner 生效权限 -> 执行变更 -> 同事务内再解析 owner ->
//	  新状态锁定 且 变更前未锁定 -> 回滚 + ownerLockoutMsg
//	  否则提交
//
// ★ 判据是"生效结果"而不是"变更内容"：模拟走真实解析链，
//   所以 subject=owner 的直接行、subject=owner 所在【组】的组行、
//   以及"删掉 owner 的写行、使祖先的读行顶上来"这类删除型锁死，
//   都会在同一次模拟里现形（Peer 验收矩阵的"锁死-2"由此覆盖）。
//
// ★ 只拦【新造】锁死（postLocked && !preLocked）：
//   若状态已经坏（preLocked），继续放行其它变更 —— 否则坏数据会把
//   管理入口一起锁死，谁也无法修复。API 路径上 owner 今天不可能是
//   locked（Q1 取证：团队 ACL 此前无人能创建），所以 preLocked 分支
//   目前是纯防御。
//
// ★ 转让（Peer 补充：列入本次）——核实结论：转让功能【不存在】。
//   UpdateProjectService 只接受 name/description/status（service/team/service.go:33），
//   inventory/team.go 的 SetOwnerID 仅出现在 CreateProject（team.go:278），
//   全仓没有"改项目 owner_id"的入口 => 场景不可达，今天无洞。
//   留给未来的钩子：将来若加转让端点，必须在转让路径复用本函数的模拟。
//   （写在这里而不是注释里划掉，因为这是 Peer 点名要求的交付项。）
func guardOwnerLockout(c *gin.Context, target *resolvedTarget,
	mutate func(ctx context.Context, bound inventory.FilePermissionClient) error) error {

	dep := dependency.FromContext(c)
	proj, err := dep.TeamClient().GetProjectByID(c, target.ProjectID)
	if err != nil || proj == nil {
		return serializer.NewError(serializer.CodeNotFound, "Project not found", err)
	}

	// owner 的组必须加载：组行（subject=group）也要被模拟覆盖
	octx := context.WithValue(c, inventory.LoadUserGroup{}, true)
	owner, err := dep.UserClient().GetByID(octx, proj.OwnerID)
	if err != nil || owner == nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to load project owner", err)
	}
	ownerGroup := 0
	if owner.Edges.Group != nil {
		ownerGroup = owner.Edges.Group.ID
	}

	client := dep.FilePermissionClient()
	pre, err := client.ResolveFilePermission(c, target.FileID, proj.OwnerID, ownerGroup)
	if err != nil {
		return serializer.NewError(serializer.CodeDBError,
			"Failed to resolve project owner permission", err)
	}
	preLocked := pre != nil && pre.Permission != inventory.FileAclPermissionWrite

	bound, tx, tctx, err := inventory.WithTx(c, client)
	if err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to start transaction", err)
	}
	if err := mutate(tctx, bound); err != nil {
		_ = inventory.Rollback(tx)
		return err
	}
	post, err := bound.ResolveFilePermission(tctx, target.FileID, proj.OwnerID, ownerGroup)
	if err != nil {
		_ = inventory.Rollback(tx)
		return serializer.NewError(serializer.CodeDBError,
			"Failed to simulate owner permission after change", err)
	}
	postLocked := post != nil && post.Permission != inventory.FileAclPermissionWrite
	if postLocked && !preLocked {
		_ = inventory.Rollback(tx)
		return serializer.NewError(serializer.CodeParamErr, ownerLockoutMsg, nil)
	}
	if err := inventory.Commit(tx); err != nil {
		return serializer.NewError(serializer.CodeDBError,
			"Failed to commit permission change", err)
	}
	return nil
}

// decodeSubject 把请求里的 subject_id（hashid 字符串）解码成整型 ID，
// 并校验主体确实存在。**解码失败必须报错，不得静默忽略。**
func decodeSubject(c *gin.Context, subjectType, raw string) (int, error) {
	dep := dependency.FromContext(c)

	if !inventory.ValidFileAclSubject(subjectType) {
		return 0, serializer.NewError(serializer.CodeParamErr, "Invalid subject_type", nil)
	}

	var (
		id  int
		err error
	)
	switch subjectType {
	case inventory.FileAclSubjectUser:
		id, err = dep.HashIDEncoder().Decode(strings.TrimSpace(raw), hashid.UserID)
	case inventory.FileAclSubjectGroup:
		id, err = dep.HashIDEncoder().Decode(strings.TrimSpace(raw), hashid.GroupID)
	}
	if err != nil || id <= 0 {
		return 0, serializer.NewError(serializer.CodeParamErr, "Invalid subject_id", err)
	}

	// 主体必须存在，避免写入永远不可能命中的脏记录
	switch subjectType {
	case inventory.FileAclSubjectUser:
		if u, uerr := dep.UserClient().GetByID(c, int(id)); uerr != nil || u == nil {
			return 0, serializer.NewError(serializer.CodeUserNotFound, "User not found", uerr)
		}
	case inventory.FileAclSubjectGroup:
		if g, gerr := dep.GroupClient().GetByID(c, int(id)); gerr != nil || g == nil {
			return 0, serializer.NewError(serializer.CodeGroupNotFound, "Group not found", gerr)
		}
	}

	return int(id), nil
}

// buildResponse 组装响应：直接记录（self）+ 当前请求者的生效权限（effective）
func buildResponse(c *gin.Context, target *resolvedTarget) (*FileAclResponse, error) {
	dep := dependency.FromContext(c)

	rows, err := dep.FilePermissionClient().ListFilePermissions(c, target.FileID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list file permissions", err)
	}

	resp := &FileAclResponse{
		Self:      make([]*AclEntry, 0, len(rows)),
		Effective: []*EffectiveAcl{},
	}

	for _, row := range rows {
		entry := &AclEntry{
			ID:          row.ID,
			FileID:      row.FileID,
			SubjectType: row.SubjectType,
			SubjectID:   row.SubjectID,
			Permission:  row.Permission,
			Inherit:     row.Inherit,
			CreatedBy:   row.CreatedBy,
			CreatedAt:   row.CreatedAt,
		}
		entry.Subject = buildSubjectBrief(c, dep, row.SubjectType, row.SubjectID)
		resp.Self = append(resp.Self, entry)
	}

	// effective：对【当前请求者】实际生效的权限（可能来自祖先目录）
	groupID := 0
	if target.User != nil && target.User.Edges.Group != nil {
		groupID = target.User.Edges.Group.ID
	}

	resolved, rerr := dep.FilePermissionClient().ResolveFilePermission(c, target.FileID, target.User.ID, groupID)
	if rerr != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to resolve file permission", rerr)
	}
	if resolved != nil {
		item := &EffectiveAcl{
			Permission:   resolved.Permission,
			SourceFileID: resolved.SourceFileID,
			Direct:       resolved.Direct,
		}
		if !resolved.Direct && resolved.SourceName != "" {
			name := resolved.SourceName
			item.InheritedFrom = &name
		}
		resp.Effective = append(resp.Effective, item)
	}

	return resp, nil
}

// buildSubjectBrief 构造主体简要信息：用户给 hashid+昵称，用户组给 hashid+组名
func buildSubjectBrief(ctx context.Context, dep dependency.Dep, subjectType string, subjectID int) *SubjectBrief {
	switch subjectType {
	case inventory.FileAclSubjectUser:
		u, err := dep.UserClient().GetByID(ctx, subjectID)
		if err != nil || u == nil {
			return &SubjectBrief{ID: "", Nickname: "#" + itoa(subjectID)}
		}
		return &SubjectBrief{
			ID:       hashid.EncodeUserID(dep.HashIDEncoder(), u.ID),
			Nickname: u.Nick,
		}
	case inventory.FileAclSubjectGroup:
		g, err := dep.GroupClient().GetByID(ctx, subjectID)
		if err != nil || g == nil {
			return &SubjectBrief{ID: "", Nickname: "#" + itoa(subjectID)}
		}
		return &SubjectBrief{
			ID:       hashid.EncodeGroupID(dep.HashIDEncoder(), g.ID),
			Nickname: g.Name,
		}
	}
	return nil
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

// ============================ 查看 ============================

// List 查看某个文件上的 ACL（需要读权限）
func (s *ListPermissionService) List(c *gin.Context) (*FileAclResponse, error) {
	target, err := resolveTarget(c, s.Uri, false)
	if err != nil {
		return nil, err
	}

	return buildResponse(c, target)
}

// ============================ 设置 ============================

// Set 设置/覆盖一条 ACL，返回刷新后的 ACL 列表（需要管理权限）
func (s *SetPermissionService) Set(c *gin.Context) (*FileAclResponse, error) {
	target, err := resolveTarget(c, s.Uri, true)
	if err != nil {
		return nil, err
	}

	if !inventory.ValidFileAclPermission(s.Permission) {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid permission, expect read or write", nil)
	}

	subjectID, err := decodeSubject(c, s.SubjectType, s.SubjectID)
	if err != nil {
		return nil, err
	}

	// inherit 缺省为 true（契约 §2 默认值）
	inherit := true
	if s.Inherit != nil {
		inherit = *s.Inherit
	}

	dep := dependency.FromContext(c)
	args := &inventory.NewFilePermissionArgs{
		FileID:      target.FileID,
		SubjectType: s.SubjectType,
		SubjectID:   subjectID,
		Permission:  s.Permission,
		Inherit:     inherit,
		CreatedBy:   target.User.ID,
	}

	if target.ProjectID > 0 {
		// 团队目标：事务内变更 + 写前模拟（owner 不可被锁死）
		if err := guardOwnerLockout(c, target, func(tctx context.Context,
			bound inventory.FilePermissionClient) error {
			if _, err := bound.UpsertFilePermission(tctx, args); err != nil {
				return serializer.NewError(serializer.CodeDBError,
					"Failed to save file permission", err)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	} else {
		// 个人/分享目标：原逻辑逐字保留
		if _, err := dep.FilePermissionClient().UpsertFilePermission(c, args); err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to save file permission", err)
		}
	}

	return buildResponse(c, target)
}

// ============================ 删除 ============================

// Delete 删除一条 ACL（需要管理权限；幂等）
func (s *DeletePermissionService) Delete(c *gin.Context) error {
	target, err := resolveTarget(c, s.Uri, true)
	if err != nil {
		return err
	}

	subjectID, err := decodeSubject(c, s.SubjectType, s.SubjectID)
	if err != nil {
		return err
	}

	dep := dependency.FromContext(c)
	if target.ProjectID > 0 {
		// 团队目标：删除同样可能造成锁死 ——
		// 例：owner 的近端行是 write、祖先行是 read，删掉近端行后
		//     祖先的 read 顶上来 -> owner 反而变只读。
		//     所以 DELETE 与 PUT 走同一个模拟（Peer 补充 1：
		//     删除动作在角色门内外的两种结果都要有断言）。
		if err := guardOwnerLockout(c, target, func(tctx context.Context,
			bound inventory.FilePermissionClient) error {
			if err := bound.DeleteFilePermission(tctx, target.FileID,
				s.SubjectType, subjectID); err != nil {
				return serializer.NewError(serializer.CodeDBError,
					"Failed to delete file permission", err)
			}
			return nil
		}); err != nil {
			return err
		}
	} else {
		// 个人/分享目标：原逻辑逐字保留
		if err := dep.FilePermissionClient().
			DeleteFilePermission(c, target.FileID, s.SubjectType, subjectID); err != nil {
			return serializer.NewError(serializer.CodeDBError, "Failed to delete file permission", err)
		}
	}

	return nil
}
