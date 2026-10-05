import {
  Badge,
  Box,
  Button,
  Divider,
  IconButton,
  ListItemText,
  Menu,
  Skeleton,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import {
  getTeamNotifications,
  getTeamUnreadCount,
  sendReadNotifications,
  TeamNotification,
} from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import { SquareMenuItem } from "../FileManager/ContextMenu/ContextMenu.tsx";
import SendLogging from "../Icons/SendLogging.tsx";

/** 轮询间隔。团队通知不是即时通讯，60 秒足够，也避免给后端压力。 */
const POLL_MS = 60_000;

/**
 * 未读色条的颜色映射。
 *
 * 这里的 key 必须与后端 `inventory/team_doc.go` 里实际定义的常量**逐字对应** ——
 * 不在表里的 type 会回落到默认色（不会报错，但配色不对）。
 * 当前后端全部 8 个 type：
 *   mention / assigned / commented / status_changed / due_soon / member_added（既有）
 *   topic_mention / post_mention（讨论区新增）
 */
const typeColor: Record<string, "primary" | "success" | "warning" | "info" | "error"> = {
  // 提及类：需要我回应，用信息色
  mention: "info",
  topic_mention: "info",
  post_mention: "info",
  // 指派与协作：与我直接相关
  assigned: "primary",
  // 状态与进度变化
  status_changed: "warning",
  due_soon: "error",
  commented: "success",
  member_added: "success",
};

/**
 * 团队协作通知入口（顶栏铃铛）。
 *
 * 之前这个模块会**产生**通知（@提及、指派、加协作者），但前端没有任何地方能**看到**它们 ——
 * 等于「系统有收件箱，产品里没有入口」。这个组件补上这个入口。
 *
 * 与 TaskListIconButton 采用同一套结构与间距，保证顶栏图标语言一致。
 */
export const TeamNotificationButton = () => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const navigate = useNavigate();

  const [anchor, setAnchor] = useState<null | HTMLElement>(null);
  const [unread, setUnread] = useState(0);
  const [items, setItems] = useState<TeamNotification[]>([]);
  const [loading, setLoading] = useState(false);

  const loadCount = useCallback(() => {
    dispatch(getTeamUnreadCount())
      .then((r) => setUnread(r?.unread ?? 0))
      .catch(() => setUnread(0));
  }, [dispatch]);

  useEffect(() => {
    loadCount();
    const timer = setInterval(loadCount, POLL_MS);
    return () => clearInterval(timer);
  }, [loadCount]);

  const openPanel = (e: React.MouseEvent<HTMLElement>) => {
    setAnchor(e.currentTarget);
    setLoading(true);
    dispatch(getTeamNotifications(false, 20))
      .then((r) => setItems(r ?? []))
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  };

  const onClose = () => setAnchor(null);

  const markAllRead = () => {
    dispatch(sendReadNotifications({ all: true })).then(() => {
      setUnread(0);
      setItems((prev) => prev.map((n) => ({ ...n, is_read: true })));
    });
  };

  const onItemClick = (n: TeamNotification) => {
    if (!n.is_read) {
      dispatch(sendReadNotifications({ ids: [n.id] })).then(loadCount);
    }
    onClose();
    // 通知只带得动有限上下文：能定位到项目就跳项目页，否则跳到讨论区总览
    if (n.project_id) {
      navigate(`/team/${n.project_id}`);
    } else {
      navigate("/discussion");
    }
  };

  return (
    <Box sx={{ position: "relative" }}>
      <Tooltip title={t("team.notifications")} enterDelay={0}>
        <IconButton size="large" onClick={openPanel}>
          <Badge
            sx={{ fontSize: (theme) => theme.typography.body2.fontSize }}
            badgeContent={unread}
            color={"secondary"}
          >
            <SendLogging />
          </Badge>
        </IconButton>
      </Tooltip>

      <Menu
        anchorEl={anchor}
        open={!!anchor}
        onClose={onClose}
        slotProps={{
          paper: { sx: { width: 380, maxHeight: 460 } },
          list: { dense: true, sx: { py: 0 } },
        }}
      >
        <Stack
          direction="row"
          alignItems="center"
          sx={{ px: 2, py: 1, position: "sticky", top: 0, bgcolor: "background.paper", zIndex: 1 }}
        >
          <Typography variant="subtitle2" fontWeight={600} sx={{ flexGrow: 1 }}>
            {t("team.notifications")}
          </Typography>
          {unread > 0 && (
            <Button size="small" onClick={markAllRead}>
              {t("team.markAllRead")}
            </Button>
          )}
        </Stack>
        <Divider />

        {loading &&
          [...Array(3)].map((_, i) => (
            <Box key={`sk-${i}`} sx={{ px: 2, py: 1.5 }}>
              <Skeleton variant="text" width="70%" />
              <Skeleton variant="text" width="45%" />
            </Box>
          ))}

        {!loading &&
          items.map((n) => (
            <SquareMenuItem
              key={n.id}
              onClick={() => onItemClick(n)}
              sx={{
                alignItems: "flex-start",
                py: 1,
                // 未读用左侧色条表示，比整行加粗更克制
                borderLeft: 3,
                borderColor: n.is_read ? "transparent" : (typeColor[n.type] ?? "primary") + ".main",
              }}
            >
              <ListItemText
                slotProps={{
                  primary: {
                    variant: "body2",
                    fontWeight: n.is_read ? 400 : 600,
                    sx: { whiteSpace: "normal", wordBreak: "break-word" },
                  },
                  secondary: {
                    variant: "caption",
                    sx: { whiteSpace: "normal", wordBreak: "break-word" },
                  },
                }}
                primary={n.title}
                secondary={n.body}
              />
            </SquareMenuItem>
          ))}

        {!loading && items.length === 0 && (
          <Box sx={{ px: 2, py: 4, textAlign: "center" }}>
            <Typography variant="body2" color="text.secondary">
              {t("team.noNotification")}
            </Typography>
          </Box>
        )}
      </Menu>
    </Box>
  );
};

export default TeamNotificationButton;
