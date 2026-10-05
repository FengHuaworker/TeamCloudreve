import { Box, Button, Chip, List, ListItem, ListItemText, Skeleton, Typography } from "@mui/material";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AppError } from "../../api/request.ts";
import { getTeamTrash, sendRestoreTeamTrash, TeamTrashFile } from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import { formatLocalTime } from "../../util/datetime.ts";
import { fsOf, isCollectiveFs } from "../../util/uri.ts";
import Nothing from "../Common/Nothing.tsx";
import ArrowClockwise from "../Icons/ArrowClockwise.tsx";
import DeleteOutlined from "../Icons/DeleteOutlined.tsx";

/** 与 TaskDetailDialog.tsx:56 同一实现（那边未导出；保持字节一致便于比对）。 */
function formatSize(bytes: number): string {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = bytes;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

interface TeamTrashProps {
  projectId: number;
}

/**
 * 项目级团队回收站。
 *
 * 权限口径 = 甲（与"能删"完全同口径）：
 *   可见 = 可恢复 = 能删。后端列表返回
 *     403 = 是成员但对项目无写权限（种了 read ACL）
 *     404 = 项目不存在 / 你不是成员
 *   两者都被视为【权限答案】而非故障：渲染成"无权访问"整块，
 *   不展示任何恢复入口，也不弹全局 toast（bypassSnackbar 在 api 层）。
 *
 * 基线不动的边界（Peer 约束）：
 *   · 本组件只读 team 回收站；个人空间回收站行为不受影响
 *   · 不碰 EmptyFileList 的空目录判定（那是文件管理器的事）
 */
const TeamTrash = ({ projectId }: TeamTrashProps) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();

  const [files, setFiles] = useState<TeamTrashFile[]>([]);
  const [loading, setLoading] = useState(true);
  const [denied, setDenied] = useState(false);
  const [restoring, setRestoring] = useState<string | undefined>();

  const load = useCallback(() => {
    if (!projectId) return;
    setLoading(true);
    dispatch(getTeamTrash(projectId))
      .then((res) => {
        setFiles(res?.files ?? []);
        setDenied(false);
      })
      .catch((e) => {
        // 403/404 = 权限答案 -> 隐藏整个入口；其它错误已由全局 toast 提示
        if (e instanceof AppError && (e.code === 403 || e.code === 404)) {
          setDenied(true);
        }
        setFiles([]);
      })
      .finally(() => setLoading(false));
  }, [dispatch, projectId]);

  useEffect(() => {
    load();
  }, [load]);

  const restore = (hashId: string) => {
    setRestoring(hashId);
    dispatch(sendRestoreTeamTrash(projectId, { file_hash_ids: [hashId] }))
      .then(() => load())
      .catch(() => {
        // 错误已由全局 toast 呈现（此处未 bypass）
      })
      .finally(() => setRestoring(undefined));
  };

  if (loading) {
    return (
      <Box sx={{ pt: 2 }}>
        <Skeleton height={56} />
        <Skeleton height={56} />
        <Skeleton height={56} />
      </Box>
    );
  }

  if (denied) {
    // 整块无权限视图：列表为空 + 没有任何恢复按钮，即"隐藏恢复入口"
    return (
      <Nothing
        primary={t("team.trashNoAccess")}
        secondary={t("team.trashNoAccessSecondary")}
        size={0.7}
        top={63}
      />
    );
  }

  if (files.length === 0) {
    return <Nothing primary={t("team.trashEmpty")} size={0.7} top={63} />;
  }

  return (
    <Box sx={{ pt: 1 }}>
      <Typography variant="body2" sx={{ color: "text.secondary", mb: 1 }}>
        {t("team.trashHint")}
      </Typography>
      <List disablePadding>
        {files.map((f) => {
          // 防御：团队回收站里的记录必须是 team 路径（后端已按项目过滤，
          // 这里再挡一次，防止将来接口变化把别的文件系统内容渲染进来）。
          //
          // ★ 生产 bug（Peer 2026-10-06 实测，DOM 判据定位）：
          //   isCollectiveFs 的参数是【fs 名】（"team"），不是 URI ——
          //   我第一版写成 isCollectiveFs(f.original_path)，整串 URI
          //   永远 !== "team" -> 所有行被滤成 null：
          //   接口有数据、提示文案正常显示（files.length>0 走对了分支）、
          //   但列表零行 —— "送达验证的标记在包里"防不住这种错，
          //   界面只有界面能证明（DOM 断言：文件名 + 恢复按钮）。
          //   正确组合见 mayWrite 的写法：isCollectiveFs(fsOf(uri))。
          if (!isCollectiveFs(fsOf(f.original_path))) return null;
          return (
            <ListItem
              key={f.hash_id}
              divider
              secondaryAction={
                <Button
                  size="small"
                  variant="outlined"
                  startIcon={<ArrowClockwise />}
                  disabled={restoring !== undefined}
                  onClick={() => restore(f.hash_id)}
                >
                  {restoring === f.hash_id ? t("team.trashRestoring") : t("team.trashRestore")}
                </Button>
              }
            >
              <ListItemText
                disableTypography
                primary={
                  <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                    <DeleteOutlined sx={{ color: "action.disabled" }} />
                    <Typography variant="body2" sx={{ fontWeight: 500 }}>
                      {f.original_name}
                    </Typography>
                    <Chip
                      size="small"
                      variant="outlined"
                      label={f.is_folder ? t("team.trashFolder") : t("team.trashFile")}
                    />
                    <Typography variant="caption" sx={{ color: "text.secondary" }}>
                      {f.is_folder ? "" : formatSize(f.size)}
                    </Typography>
                  </Box>
                }
                secondary={
                  <Typography variant="caption" sx={{ color: "text.secondary" }}>
                    {t("team.trashFrom")} {f.original_path} · {t("team.trashDeletedAt")}{" "}
                    {formatLocalTime(f.trashed_at)}
                  </Typography>
                }
              />
            </ListItem>
          );
        })}
      </List>
    </Box>
  );
};

export default TeamTrash;
