// Package teamtrash 实现【项目级团队回收站】（用户口径乙，2026-10-05 定）。
//
// ════════════════════════════════════════════════════════════════════════
// 一、归属信息从哪来 —— 取证结论，不是猜的
// ════════════════════════════════════════════════════════════════════════
//
// 团队文件软删时，上游会把原路径写进 metadata：
//
//	[软删前] files.id=3 name='fA.txt' file_children=2
//	[软删后] files.id=3 name='459c21db-...' file_children=NULL
//	         metadata: sys:restore_uri = "cloudreve://team/1/subA/fA.txt"
//
// ★ 关键：sys:restore_uri 里【直接带着项目 id】，所以"按项目过滤"是
// 解析字符串前缀，而不是反查父链 —— 父链在软删那刻已经被摘掉了。
//
// 实测（forensics_restore_uri.py，7/7 PASS）：
//
//	del.txt id= 3 -> 'cloudreve://team/1/d/del.txt'   项目 1
//	del.txt id= 7 -> 'cloudreve://team/2/d/del.txt'   项目 2
//	del.txt id=11 -> 'cloudreve://team/3/d/del.txt'   项目 3
//	pmydel.txt   -> 'cloudreve://my/pmydel.txt'       个人空间
//	未删文件      -> None
//
// ★★ 因此本功能【不需要修改软删逻辑】—— 而这是最初最担心的风险点
//    （改软删会影响个人空间那条基线路径，个人空间是基线，不能动）。
//
// ════════════════════════════════════════════════════════════════════════
// 二、"不串"的根本保证不是 parent=NULL，而是 owner
// ════════════════════════════════════════════════════════════════════════
//
// 个人回收站的查询是：
//
//	childFileQuery: file.OwnerIDEQ(userID) AND Not(file.HasParent())
//
// 团队文件 owner 是【系统账号】，永远不满足 OwnerIDEQ(我)。
// 所以哪怕将来有人改了 parent 的形态，隔离性也不会因此破掉 ——
// 这是个比"当前实现细节"更强的不变量。
// 本文件的查询同样以 owner 为轴（而不是以 parent 为轴）来筛选。
//
// ════════════════════════════════════════════════════════════════════════
// 三、权限口径（Peer 拍板【甲】：与"能删"完全同一口径）
// ════════════════════════════════════════════════════════════════════════
//
//	可见 = 可恢复 = 能删（项目成员 + 项目根 ACL write 或无 ACL 回落）
//
// ★ "能删"的判定不是我新造的，就是 dbfs.canWriteProjectID 那一套：
//   全局管理员 -> 放行；项目根行不存在 -> 放行；
//   ACL 解析失败 -> 放行；未命中 ACL -> 放行；命中 -> 必须 write。
//   【每一处回落方向都与之严格一致】，否则会出现"能删但看不见回收站"
//   或"看不见但能删"的两套口径打架。
//
// 注意：canWriteProjectID 不看 team_members.role —— 所以本包也不看。
// 反向测试要造"看不到"的场景，必须【真的种一条 read ACL】在项目根上
// （这正是 S0b 的只读子目录场景），不能只靠 role=viewer。
//
// ════════════════════════════════════════════════════════════════════════
// 四、写这个文件时踩的坑（留痕 —— 差点交出编译不过的代码）
// ════════════════════════════════════════════════════════════════════════
//
// 第一版我【凭记忆】写了六个不存在的东西：
//
//	teammember.RoleOwner          <- Role 是 string，无此类常量
//	member.Permission             <- teammember 【根本没有】这个字段
//	proj.FileChildren             <- teamproject 【没有】这个字段
//	hashid.TeamProjectID          <- hashid 里没有这个实体类型
//	inventory.ResolveFilePermission <- 是 FilePermissionClient 的【接口方法】
//	f.FileChildren != nil         <- FileChildren 是普通 int，不是 *int
//	pkg/filemanager/dbfs          <- 少了 fs 目录层级
//
// 这是今天第五~十一次"凭记忆写 API 形状"。而这次更糟：
// 我是先写了 200 行才去核对，顺序完全反了。
//
// ★ 硬规则（写进模板）：**写任何跨包调用前，先 grep 那个包的真实定义。**
//   项目根的定位方式尤其反直觉 —— 它不是 projects 表上的一个列，
//   而是【一个名为项目 ID 字符串、owner 为系统账号、无父目录的 folder 行】
//   （见 dbfs.findProjectRootWith / projectRootName）。
package teamtrash

import (
	"context"
	"regexp"
	"strconv"
	"time"

	"github.com/cloudreve/Cloudreve/v4/application/constants"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/file"
	"github.com/cloudreve/Cloudreve/v4/ent/metadata"
	"github.com/cloudreve/Cloudreve/v4/ent/teammember"
	"github.com/cloudreve/Cloudreve/v4/ent/teamproject"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs/dbfs"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
)

// teamUriPattern 匹配团队空间的 restore_uri，捕获项目 ID。
//
//	"cloudreve://team/1/subA/fA.txt"  ->  "1"
var teamUriPattern = regexp.MustCompile(
	"^" + regexp.QuoteMeta(constants.CloudreveScheme) + `://` +
		regexp.QuoteMeta(string(constants.FileSystemTeam)) + `/(\d+)(/|$)`)

// parseProjectID 从 restore_uri 解析项目 ID。
//
// 返回 (projectID, true) 表示这是一条团队空间的删除记录。
// 个人空间（cloudreve://my/...）返回 false —— 【正向判据】，
// 不依赖"不是 team 就是 my"这种排除法。
func parseProjectID(restoreUri string) (int, bool) {
	m := teamUriPattern.FindStringSubmatch(restoreUri)
	if m == nil {
		return 0, false
	}
	id, err := strconv.Atoi(m[1])
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// OriginalNameFromUri 从原 URI 取末段文件名（= dbfs.File.DisplayName 的语义，
// 见 file.go:86-97：有 restore_uri 时取 path.Base，否则取 Name()）。
func OriginalNameFromUri(uri string) string {
	for i := len(uri) - 1; i >= 0; i-- {
		if uri[i] == '/' {
			return uri[i+1:]
		}
	}
	return uri
}

// TrashedFile 是回收站里的一条记录。
//
// ★ 文件实体按 Cloudreve 惯例只暴露 hashid（raw id 是内部标识；
//   团队自己的实体用裸 int，但"Cloudreve 的实体仍沿用 hashid"——见 pathID 注释）。
type TrashedFile struct {
	HashID       string    `json:"hash_id"`
	ProjectID    int       `json:"project_id"`
	ProjectName  string    `json:"project_name"`
	OriginalPath string    `json:"original_path"` // sys:restore_uri 原值，含项目段
	OriginalName string    `json:"original_name"` // 从原路径取的末段
	TrashedAt    time.Time `json:"trashed_at"`
	Size         int64     `json:"size"`
	IsFolder     bool      `json:"is_folder"`
}

// ListResult 是一次列表查询的结果。
type ListResult struct {
	Files []TrashedFile `json:"files"`
	Total int           `json:"total"`
}

// ── 错误 ──────────────────────────────────────────────────────────────
//
// ★ "项目不存在"与"不是成员"返回【同一个错误】：否则可以通过错误码
//   差异枚举出"哪些项目 ID 存在"（与 service/team.checkAccess 的
//   CodeNotFound 口径一致）。

var ErrProjectNotFound = serializer.NewError(serializer.CodeNotFound,
	"Project not found", nil)

var ErrNotPermitted = serializer.NewError(serializer.CodeNoPermissionErr,
	"You need write permission on this project to view and restore its trash bin", nil)

// ErrOriginalDirGone 恢复目标目录已不存在。
//
// ★ 明确报错而不是静默丢到根目录 —— 静默丢根会让用户以为"恢复成功"，
//   但文件出现在他没预期的地方，比报错更难排查（Peer 2026-10-05 明确要求）。
var ErrOriginalDirGone = serializer.NewError(serializer.CodeParentNotExist,
	"Original directory no longer exists, cannot restore to original location", nil)

// ErrNameConflict 恢复目标位置已存在同名文件。
var ErrNameConflict = serializer.NewError(serializer.CodeObjectExist,
	"A file with the same name already exists in the original directory", nil)

// Service 提供项目级回收站的查询与恢复。
type Service struct {
	client     *ent.Client
	fileClient inventory.FileClient
	acl        inventory.FilePermissionClient
	userClient inventory.UserClient
	hasher     hashid.Encoder
	logger     logging.Logger
}

// New 从依赖装配服务。
func New(client *ent.Client, fileClient inventory.FileClient, acl inventory.FilePermissionClient,
	userClient inventory.UserClient, hasher hashid.Encoder, l logging.Logger) *Service {
	return &Service{
		client:     client,
		fileClient: fileClient,
		acl:        acl,
		userClient: userClient,
		hasher:     hasher,
		logger:     l,
	}
}

// findProjectRoot 定位项目根目录行。
//
// ★ 与 dbfs.findProjectRootWith 完全同一判据（照抄，不自己发明）：
//
//	owner_id = 系统账号, name = strconv.Itoa(projectID),
//	type = folder, file_children IS NULL
//
// 之所以不用"projects 表上的某个列"，是因为 teamproject schema 里
// 根本没有指向根目录的字段（只有 name/description/owner_id/status/settings）。
func (s *Service) findProjectRoot(ctx context.Context, projectID, sysUserID int) (*ent.File, error) {
	root, err := s.client.File.Query().
		Where(
			file.OwnerIDEQ(sysUserID),
			file.NameEQ(strconv.Itoa(projectID)),
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
	return root, nil
}

// hasWriteOnProject 项目级写权限判定 —— 与 dbfs.canWriteProjectID【严格同口径】。
//
// 判定顺序与回落方向逐条对应（见 dbfs/team_navigator.go:1156-1259）：
//
//	1. 全局管理员 -> true
//	2. 项目根行不存在（项目刚建/尚未落根）-> true（无 ACL 可命中）
//	3. ACL 解析失败 -> true（与 acl.go"解析失败放行"一致 —— ACL 是附加能力，
//	   绝不允许它把已有的文件系统操作弄挂；能删 = 能恢复，同进同退）
//	4. 未命中 ACL -> true（团队路径的所有者判定已被旁路，无 ACL 即允许）
//	5. 命中 ACL -> 必须是 write
//
// ★ 我第一版在 3 上选了"失败拒绝"并写了理由 —— 但 Peer 的口径是
//   "与能删完全同一口径"，而删除在失败时是【放行】的。
//   一个"删除放行、回收站拒绝"的组合属于两套口径打架，比任何单侧
//   取舍都糟。改为严格一致，并把这条记在这里。
func (s *Service) hasWriteOnProject(ctx context.Context, u *ent.User, projectID int) (bool, error) {
	// 1. 全局管理员
	if u.Edges.Group != nil &&
		u.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		return true, nil
	}

	sysUserID, _, err := inventory.SystemTeamIDs(ctx, s.client)
	if err != nil {
		return false, err
	}
	if sysUserID == 0 {
		// 系统账号未初始化 -> 团队空间不可用，按"无 ACL 可命中"处理
		return false, nil
	}

	// 2. 项目根行不存在 -> 放行（与 canWriteProjectID 回落 4 一致）
	root, err := s.findProjectRoot(ctx, projectID, sysUserID)
	if err != nil {
		return false, err
	}
	if root == nil {
		return true, nil
	}

	groupID := 0
	if u.Edges.Group != nil {
		groupID = u.Edges.Group.ID
	}

	// ★ 与 acl.go:107-109 同一模式：参与当前事务，不另开连接。
	//   SQLite 连接池上限为 1，事务内另开连接会永久等待、整个请求挂死。
	acl := s.acl
	if bound, tx := inventory.InheritTx(ctx, s.acl); tx != nil {
		acl = bound
	}

	resolved, err := acl.ResolveFilePermission(ctx, root.ID, u.ID, groupID)
	// 3. 解析失败 -> 放行（与 acl.go:113 一致）
	if err != nil {
		return true, nil
	}
	// 4. 未命中 ACL -> 放行
	if resolved == nil {
		return true, nil
	}
	// 5. 命中 -> 必须 write
	return resolved.Permission == inventory.FileAclPermissionWrite, nil
}

// requireWriteOnProject 校验：项目存在 + 是成员 + 项目级写权限（= 能删）。
func (s *Service) requireWriteOnProject(ctx context.Context, u *ent.User, projectID int) error {
	if u == nil {
		return ErrProjectNotFound
	}

	proj, err := s.client.TeamProject.Query().
		Where(teamproject.IDEQ(projectID)).Only(ctx)
	if err != nil {
		return ErrProjectNotFound
	}

	// 全局管理员：任何项目都可管理（与 service/team.checkAccess 一致）
	if u.Edges.Group != nil &&
		u.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		return nil
	}

	// 成员门：与 checkAccess 同序 —— 先判项目 owner（owner 未必有
	// 成员表记录，checkAccess 就是 owner 在前、成员表在后），
	// 再查成员表；两者都没有 = 不是成员，与"项目不存在"同错误，防枚举。
	//
	// ★ 这里第一版我直接查成员表、漏了 owner 判定 —— 项目 owner
	//   若没有成员表行会被误拒（正向用例会挂在第一门）。
	//   写权限判定时"先照抄 checkAccess 的顺序"这一步不能省。
	if proj.OwnerID != u.ID {
		if _, err := s.client.TeamMember.Query().
			Where(
				teammember.ProjectID(projectID),
				teammember.UserID(u.ID),
			).Only(ctx); err != nil {
			return ErrProjectNotFound
		}
	}

	// 项目级写权限门（= 能删的判定）
	ok, err := s.hasWriteOnProject(ctx, u, projectID)
	if err != nil {
		return ErrNotPermitted
	}
	if !ok {
		return ErrNotPermitted
	}
	return nil
}

// List 返回某项目的回收站内容。
func (s *Service) List(ctx context.Context, u *ent.User, projectID int) (*ListResult, error) {
	if err := s.requireWriteOnProject(ctx, u, projectID); err != nil {
		return nil, err
	}

	sysUserID, _, err := inventory.SystemTeamIDs(ctx, s.client)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load system account", err)
	}
	if sysUserID == 0 {
		return &ListResult{Files: []TrashedFile{}}, nil
	}

	// ★ 查询以 owner = 系统账号 为轴（不是以 parent=NULL 为轴）：
	//   · owner 是"团队文件"这个身份的本质，软删不会改变它
	//   · parent=NULL 只是软删的实现细节，将来可能变
	rows, err := s.client.File.Query().
		Where(
			file.OwnerIDEQ(sysUserID),
			file.Not(file.HasParent()),
		).
		All(ctx)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to query trash bin", err)
	}

	// 批量取 metadata，避免 N+1
	ids := make([]int, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	metaByFile := map[int]string{}
	if len(ids) > 0 {
		mds, err := s.client.Metadata.Query().
			Where(
				metadata.NameEQ(dbfs.MetadataRestoreUri),
				metadata.FileIDIn(ids...),
			).
			All(ctx)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to load restore metadata", err)
		}
		for _, m := range mds {
			metaByFile[m.FileID] = m.Value
		}
	}

	proj, err := s.client.TeamProject.Query().
		Where(teamproject.IDEQ(projectID)).Only(ctx)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load project", err)
	}

	out := make([]TrashedFile, 0)
	for _, r := range rows {
		restoreUri := metaByFile[r.ID]
		if restoreUri == "" {
			// 无 restore_uri -> 不是"可恢复的删除"，不入回收站
			// （否则前端会显示一个恢复不了的项）
			continue
		}
		pid, ok := parseProjectID(restoreUri)
		if !ok || pid != projectID {
			// ★ 隔离判据：只收属于本项目的。
			//   个人空间(my)与其它项目在这里被【精确排除】，
			//   而不是靠"应该不会出现"。
			continue
		}

		out = append(out, TrashedFile{
			HashID:       hashid.EncodeFileID(s.hasher, r.ID),
			ProjectID:    pid,
			ProjectName:  proj.Name,
			OriginalPath: restoreUri,
			OriginalName: OriginalNameFromUri(restoreUri),
			TrashedAt:    r.UpdatedAt,
			Size:         r.Size,
			IsFolder:     r.Type == int(types.FileTypeFolder),
		})
	}

	return &ListResult{Files: out, Total: len(out)}, nil
}
