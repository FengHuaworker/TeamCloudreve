package dbfs

import (
	"context"
	"testing"

	"github.com/cloudreve/Cloudreve/v4/application/constants"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
)

// 护栏 2：证明 withTeamOwnerBypass 的作用域**不泄漏**到非团队文件系统。
//
// 背景：团队空间的文件归属于系统账号，写操作者是被授权的成员，二者必然不等，
// 因此团队写路径需要跳过「仅所有者可写」校验（见 withTeamOwnerBypass 注释）。
// 该旁路一旦泄漏到个人空间，就会导致**任何人都能写别人的文件** —— 这是安全底线。
//
// 本测试覆盖泄漏的两个方向：
//   A. 对个人空间 URI 调用 withTeamOwnerBypassIfTeam -> 必须**不**设置旁路
//   B. 同一次调用中传入 team + 个人混合 URI（MoveOrCopy 场景）
//      -> 混合时不得设置旁路（跨文件系统必须走严格校验）

func mustUri(t *testing.T, s string) *fs.URI {
	t.Helper()
	u, err := fs.NewUriFromString(s)
	if err != nil {
		t.Fatalf("构造 URI %q 失败: %s", s, err)
	}
	return u
}

func bypassSet(ctx context.Context) bool {
	v, ok := ctx.Value(ByPassOwnerCheckCtxKey{}).(bool)
	return ok && v
}

// TestTeamBypassNotSetForPersonalFs 个人空间 URI 不得触发旁路。
func TestTeamBypassNotSetForPersonalFs(t *testing.T) {
	cases := []string{
		constants.CloudreveScheme + "://my",
		constants.CloudreveScheme + "://my/folder/a.txt",
		constants.CloudreveScheme + "://trash",
		constants.CloudreveScheme + "://shared_with_me",
	}

	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			ctx := withTeamOwnerBypassIfTeam(context.Background(), mustUri(t, c))
			if bypassSet(ctx) {
				t.Fatalf("旁路泄漏到非团队 URI %q：个人空间的所有者校验会被错误跳过", c)
			}
		})
	}
}

// TestTeamBypassSetForTeamFs 团队 URI 必须触发旁路（否则团队写入被 ErrOwnerOnly 拒绝）。
func TestTeamBypassSetForTeamFs(t *testing.T) {
	cases := []string{
		constants.CloudreveScheme + "://team",
		constants.CloudreveScheme + "://team/2",
	}

	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			ctx := withTeamOwnerBypassIfTeam(context.Background(), mustUri(t, c))
			if !bypassSet(ctx) {
				t.Fatalf("团队 URI %q 未设置旁路：写入会被 ErrOwnerOnly 拒绝", c)
			}
		})
	}
}

// TestTeamBypassDoesNotLeakToPersonalFs 混合 URI 场景（MoveOrCopy 的核心风险）。
//
// 关键断言：只要**任一**参与方落在非团队文件系统，就不得设置旁路。
// 否则跨文件系统的 move/copy 会让个人空间一侧的校验失效。
func TestTeamBypassDoesNotLeakToPersonalFs(t *testing.T) {
	team := mustUri(t, constants.CloudreveScheme+"://team/2")
	personal := mustUri(t, constants.CloudreveScheme+"://my")

	t.Run("team+personal 混合 -> 不设置", func(t *testing.T) {
		ctx := withTeamOwnerBypassIfTeam(context.Background(), team, personal)
		if bypassSet(ctx) {
			t.Fatal("源/目标跨越团队与个人空间时设置了旁路：" +
				"个人空间一侧的所有者校验会失效（越权风险）")
		}
	})

	t.Run("personal+team 混合 -> 不设置", func(t *testing.T) {
		ctx := withTeamOwnerBypassIfTeam(context.Background(), personal, team)
		if bypassSet(ctx) {
			t.Fatal("源/目标跨越个人与团队空间时设置了旁路（顺序无关）")
		}
	})

	t.Run("全部为 team -> 设置", func(t *testing.T) {
		ctx := withTeamOwnerBypassIfTeam(context.Background(), team, team)
		if !bypassSet(ctx) {
			t.Fatal("全部落在团队空间时未设置旁路")
		}
	})

	t.Run("无参数 -> 不设置", func(t *testing.T) {
		ctx := withTeamOwnerBypassIfTeam(context.Background())
		if bypassSet(ctx) {
			t.Fatal("未传入任何 URI 时设置了旁路")
		}
	})
}

// TestTeamBypassNilUriSafe nil URI 不得 panic，也不得设置旁路。
func TestTeamBypassNilUriSafe(t *testing.T) {
	ctx := withTeamOwnerBypassIfTeam(context.Background(), nil)
	if bypassSet(ctx) {
		t.Fatal("nil URI 设置了旁路")
	}
}

// TestIsTeamUri 直接验证判定函数本身。
func TestIsTeamUri(t *testing.T) {
	if isTeamUri(nil) {
		t.Fatal("nil 被判为 team")
	}
	if !isTeamUri(mustUri(t, constants.CloudreveScheme+"://team")) {
		t.Fatal("cloudreve://team 未被判为 team")
	}
	if !isTeamUri(mustUri(t, constants.CloudreveScheme+"://team/9")) {
		t.Fatal("cloudreve://team/9 未被判为 team")
	}
	if isTeamUri(mustUri(t, constants.CloudreveScheme+"://my")) {
		t.Fatal("cloudreve://my 被判为 team")
	}
}
