package team

import (
	"regexp"
	"strings"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/gin-gonic/gin"
)

// ============================================================
// @提及（mention）—— 全模块唯一实现
// ============================================================
//
// 本文件是 mention 解析与通知的【唯一来源】，Wiki（docs.go）、
// 任务评论（service.go）、讨论区（discussion.go）全部调用这里。
//
// 为什么抽成独立文件而不是留在 docs.go：
// 讨论区要复用同一套解析，若各写一份正则，两边会随时间漂移
// （一边支持了新的用户名字符，另一边没有），导致「@了但没通知」这种
// 极难排查的缺陷。放一处、共用一份，从结构上消除漂移可能。

// mentionRe 匹配 @提及。前端写入的格式为 @[显示名](mention:<userHashID>)，
// 这样即使显示名重复也能精确定位到人。
//
// 兼容性说明：hashid 允许字母、数字、下划线与连字符，故字符集为 [A-Za-z0-9_-]+。
var mentionRe = regexp.MustCompile(`@\[([^\]]*)\]\(mention:([A-Za-z0-9_-]+)\)`)

// extractMentions 从 Markdown 文本中解析出被提及的用户 ID。
//
// 行为约定：
//   - 自动去重：同一人即使在文本中出现多次，也只返回一次
//   - 非法 hashid 静默跳过：这是正文内容而非请求参数，
//     不能因为作者写错一个 @ 就让整个发帖请求失败
//   - 顺序稳定：按首次出现顺序返回，便于测试与调试
func extractMentions(dep dependency.Dep, content string) []int {
	if content == "" {
		return nil
	}

	seen := make(map[int]struct{})
	var ids []int
	for _, m := range mentionRe.FindAllStringSubmatch(content, -1) {
		uid, err := dep.HashIDEncoder().Decode(m[2], hashid.UserID)
		if err != nil || uid <= 0 {
			// 非法 hashid：静默跳过（见上面的行为约定）
			continue
		}
		if _, ok := seen[int(uid)]; ok {
			continue
		}
		seen[int(uid)] = struct{}{}
		ids = append(ids, int(uid))
	}
	return ids
}

// notify 写一条通知。
//
// 注意：CreateNotification 内部会跳过「自己给自己发」的情况
// （见 inventory/team_doc.go 的 UserID == ActorID 判断），
// 因此调用方无需重复判断。
func notify(c *gin.Context, dep dependency.Dep, args *inventory.NewTeamNotificationArgs) {
	nc := inventory.NewTeamNotificationClient(dep.DBClient())
	_, _ = nc.CreateNotification(c, args)
}

// MentionTarget 描述一条提及通知要落到哪个实体上。
//
// 用一个结构体而不是长参数列表，是因为讨论区加入后参数已增至 6 个，
// 且 topic/doc/task 三者互斥，位置参数极易传错（编译器不会报错）。
type MentionTarget struct {
	ProjectID int
	// Type 通知类型，取 inventory.Notification* 常量
	Type string
	// Title 通知标题（中文，面向用户）
	Title string
	// Body 通知正文（中文，面向用户）
	Body string

	// 以下三个互斥，指向被提及内容所在的实体
	TaskID *int
	DocID  *int
	// TopicID 被提及内容所在的话题。
	// 非 nil 时启用「同一话题不重复通知」的去重（见 notifyMentions）。
	TopicID *int
}

// notifyMentions 为文本中的 @提及 批量发通知。
//
// 统一入口：所有需要「@ 某人就通知他」的地方都调用这个函数。
//
// 去重规则（仅对带 TopicID 的讨论区通知生效）：
// 按 (user_id, topic_id, type) 判断是否已通知过 —— 有则跳过。
// 这样「反复保存话题」不会刷屏，而「编辑时新 @ 的人」因为此前无记录仍会正常收到。
func notifyMentions(c *gin.Context, dep dependency.Dep, content string,
	target MentionTarget, actorID int) {

	ids := extractMentions(dep, content)
	if len(ids) == 0 {
		return
	}

	nc := inventory.NewTeamNotificationClient(dep.DBClient())

	for _, uid := range ids {
		// 讨论区通知去重：同一话题同一类型只通知一次
		if target.TopicID != nil {
			exists, err := nc.ExistsTopicMention(c, uid, *target.TopicID, target.Type)
			if err == nil && exists {
				continue
			}
			// 查询出错时按「未通知过」处理，宁可多通知也不要漏通知
		}

		// 循环内取副本，避免把同一个指针传给多条通知
		// （ent 侧虽然只读，但共享指针容易被后续改动引入隐蔽 bug）
		pid := target.ProjectID
		args := &inventory.NewTeamNotificationArgs{
			UserID:    uid,
			ActorID:   actorID,
			Type:      target.Type,
			ProjectID: &pid,
			Title:     target.Title,
			Body:      target.Body,
		}
		if target.TaskID != nil {
			tid := *target.TaskID
			args.TaskID = &tid
		}
		if target.DocID != nil {
			did := *target.DocID
			args.DocID = &did
		}
		if target.TopicID != nil {
			tid := *target.TopicID
			args.TopicID = &tid
		}
		notify(c, dep, args)
	}
}

// truncateForNotification 把用户内容裁成适合放进通知正文的片段。
//
// 通知列表是窄栏展示，整篇正文塞进去既不好读也浪费存储。
// 按【字符】而非【字节】截断，避免把一个汉字切成两半产生乱码。
func truncateForNotification(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}
