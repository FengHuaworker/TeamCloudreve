package inventory

import (
	"context"
	"fmt"

	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/setting"
	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
)

// TeamSchemaVersionKey 是团队协作模块自己的 schema 版本标记。
//
// 为什么不复用 Cloudreve 的 db_version_<BackendVersion>：
// needMigration() 只在版本标记缺失时才执行 migrate()，而 migrate() 是唯一调用
// client.Schema.Create() 的地方。对于已经初始化过的数据库（已写入
// db_version_4.19.1），迁移不会再跑，团队模块新增的表就永远不会被创建。
//
// 因此这里使用独立的标记：首次启动执行一次 Schema.Create 建表，之后直接跳过。
// Schema.Create 是增量操作（只创建缺失的表/索引），对已存在的 Cloudreve 表无副作用。
//
// ⚠️ 每次给团队模块新增 ent schema 或新增字段，都必须把版本号 +1，
//
//	否则已有数据库不会执行这次的增量迁移。
//	v1 → 项目 / 任务 / 成员 / 参与人 / 评论 / 附件 / 动态
//	v2 → 文档(Wiki) / 通知
//	v3 → 讨论区话题 / 楼层（team_topics / team_posts）
//	v4 → 讨论区附件 + 通知 topic_id
//	v5 → 文件 ACL（file_permissions）
//
// 关于「加字段」：ent 的 Schema.Create 是【建表 + 补列】的增量迁移
// （已用一次性探针实测确认：删掉某列后再调用 Create，该列会被补回）。
// 因此新增字段同样只需 bump 版本号，无需手写 ALTER TABLE。
const TeamSchemaVersionKey = "db_version_team_v5"

// EnsureTeamSchema 确保团队协作模块所需的表存在。可在每次启动时安全调用。
func EnsureTeamSchema(l logging.Logger, client *ent.Client, ctx context.Context) error {
	c, err := client.Setting.Query().
		Where(setting.NameEQ(TeamSchemaVersionKey)).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("failed to check team schema version: %w", err)
	}

	if c > 0 {
		l.Debug("Team module schema is up to date.")
		return nil
	}

	l.Info("Initializing team collaboration module schema...")
	if err := client.Schema.Create(ctx); err != nil {
		return fmt.Errorf("failed to create team module schema: %w", err)
	}

	if err := client.Setting.Create().
		SetName(TeamSchemaVersionKey).
		SetValue("installed").
		Exec(ctx); err != nil {
		return fmt.Errorf("failed to write team schema version marker: %w", err)
	}

	l.Info("Team collaboration module schema initialized.")
	return nil
}
