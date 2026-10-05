package inventory

import "testing"

// 第 0 步验收：注册拦截的纯函数判定。
//
// 为什么用单元测试而不是 HTTP 端到端：
// 注册路由（routers/router.go:379）挂了 middleware.IsFunctionEnabled(RegisterEnabled)，
// 该中间件在 controller 之前执行。本实例 register_enabled=0，
// 所有注册请求都会在中间件层被 40019 挡掉，根本到不了 register.go 的拦截分支。
// 因此端到端无法验证，只能用单元测试验证判定函数本身。
func TestIsSystemTeamEmail(t *testing.T) {
	cases := []struct {
		email string
		want  bool
		why   string
	}{
		// 必须拦截
		{"e5fca29b-5ebc-4fe5-b8a0-259a374f44b6@team-space.invalid", true, "实际系统账号邮箱"},
		{"attacker@team-space.invalid", true, "同域任意地址"},
		{"ATTACKER@TEAM-SPACE.INVALID", true, "大写变体"},
		{"x@Team-Space.Invalid", true, "混合大小写"},
		{"  spaced@team-space.invalid  ", true, "首尾空白"},

		// 不得误伤
		{"user@example.com", false, "普通域名"},
		{"team-space.invalid@gmail.com", false, "域名字符串出现在 local part"},
		{"user@team-space.invalid.evil.com", false, "后缀伪装"},
		{"user@notteam-space.invalid", false, "前缀多字符"},
		{"user@team-space.invali", false, "截断 TLD"},
		{"", false, "空字符串"},
		{"@team-space.invalid", true, "空 local part（仍属该域，应拦）"},
	}

	for _, c := range cases {
		got := IsSystemTeamEmail(c.email)
		if got != c.want {
			t.Errorf("IsSystemTeamEmail(%q) = %v, want %v (%s)",
				c.email, got, c.want, c.why)
		} else {
			t.Logf("OK  %-50q -> %-5v  %s", c.email, got, c.why)
		}
	}
}
