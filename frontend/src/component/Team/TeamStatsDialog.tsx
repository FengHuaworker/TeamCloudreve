import {
  Avatar,
  Box,
  Chip,
  DialogContent,
  Divider,
  LinearProgress,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { getTeamStats, TeamAssigneeStat, TeamStats } from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import Nothing from "../Common/Nothing.tsx";
import DraggableDialog from "../Dialogs/DraggableDialog.tsx";
import WarningAmber from "../Icons/Warning.tsx";

/** 单人工作量卡片 */
const AssigneeCard = ({ stat }: { stat: TeamAssigneeStat }) => {
  const { t } = useTranslation();
  const percent = stat.total > 0 ? Math.round((stat.done / stat.total) * 100) : 0;
  return (
    <Box sx={{ p: 1.5, border: 1, borderColor: "divider", borderRadius: 1 }}>
      <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 1 }}>
        <Avatar src={stat.user?.avatar} sx={{ width: 28, height: 28, fontSize: 13 }}>
          {stat.user?.nickname?.[0]}
        </Avatar>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography variant="body2" fontWeight={500} noWrap>
            {stat.user?.nickname}
          </Typography>
        </Box>
        {stat.overdue > 0 && (
          <Tooltip title={t("team.overdueCount", { count: stat.overdue })}>
            <Chip size="small" color="error" icon={<WarningAmber />} label={stat.overdue} />
          </Tooltip>
        )}
        <Typography variant="body2" fontWeight={600}>
          {stat.done}/{stat.total}
        </Typography>
      </Stack>

      <LinearProgress variant="determinate" value={percent} sx={{ mb: 1 }} />

      <Stack direction="row" spacing={1} sx={{ flexWrap: "wrap", gap: 0.5 }}>
        <Chip size="small" variant="outlined" label={`${t("team.status_todo")} ${stat.todo}`} />
        <Chip size="small" variant="outlined" color="info" label={`${t("team.status_doing")} ${stat.doing}`} />
        <Chip size="small" variant="outlined" color="success" label={`${t("team.status_done")} ${stat.done}`} />
      </Stack>
    </Box>
  );
};

interface TeamStatsDialogProps {
  projectId: number;
  onClose: () => void;
}

/**
 * 分工与进度统计：
 *  - 顶部为项目总体进度
 *  - 下方按负责人列出工作量，便于判断分工是否均衡
 *  - 逾期任务单独标红提示
 */
const TeamStatsDialog = ({ projectId, onClose }: TeamStatsDialogProps) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const [stats, setStats] = useState<TeamStats | undefined>();

  const load = useCallback(() => {
    dispatch(getTeamStats(projectId))
      .then((res) => setStats(res))
      .catch(() => setStats(undefined));
  }, [dispatch, projectId]);

  useEffect(() => {
    load();
  }, [load]);

  const total = stats?.total ?? 0;
  const donePercent = total > 0 ? Math.round(((stats?.done ?? 0) / total) * 100) : 0;
  // 未指派的剩余任务数（total - 所有已指派人任务数）
  const assignedTotal = (stats?.by_assignee ?? []).reduce((s, a) => s + a.total, 0);
  const unassigned = Math.max(0, total - assignedTotal);

  return (
    <DraggableDialog
      title={t("team.workload")}
      showActions
      showCancel
      hideOk
      cancelText={t("common:close")}
      dialogProps={{ open: true, onClose, fullWidth: true, maxWidth: "sm" }}
    >
      <DialogContent>
        {total === 0 ? (
          <Nothing primary={t("team.noTask")} secondary={t("team.noTaskDes")} />
        ) : (
          <Stack spacing={2}>
            {/* 总体进度 */}
            <Box>
              <Stack direction="row" justifyContent="space-between" sx={{ mb: 0.5 }}>
                <Typography variant="subtitle2" fontWeight={600}>
                  {t("team.overallProgress")}
                </Typography>
                <Typography variant="subtitle2">
                  {stats?.done}/{total}（{donePercent}%）
                </Typography>
              </Stack>
              <LinearProgress variant="determinate" value={donePercent} sx={{ height: 8, borderRadius: 4 }} />
            </Box>

            <Stack direction="row" spacing={1} sx={{ flexWrap: "wrap", gap: 1 }}>
              <Chip label={`${t("team.status_todo")} ${stats?.todo ?? 0}`} variant="outlined" />
              <Chip label={`${t("team.status_doing")} ${stats?.doing ?? 0}`} variant="outlined" color="info" />
              <Chip label={`${t("team.status_done")} ${stats?.done ?? 0}`} variant="outlined" color="success" />
              {(stats?.overdue ?? 0) > 0 && (
                <Chip
                  label={`${t("team.overdue")} ${stats?.overdue}`}
                  color="error"
                  icon={<WarningAmber />}
                />
              )}
            </Stack>

            <Divider />

            {/* 按人分工 */}
            <Typography variant="subtitle2" fontWeight={600}>
              {t("team.byAssignee")}
            </Typography>

            {(stats?.by_assignee ?? []).length === 0 && (
              <Typography variant="body2" color="text.secondary">
                {t("team.noAssignee")}
              </Typography>
            )}

            <Stack spacing={1.5}>
              {(stats?.by_assignee ?? [])
                // 任务多的排在前面，便于一眼看出负载差异
                .slice()
                .sort((a, b) => b.total - a.total)
                .map((a) => (
                  <AssigneeCard key={a.user?.id ?? a.user?.nickname} stat={a} />
                ))}
            </Stack>

            {unassigned > 0 && (
              <Box sx={{ p: 1.5, border: 1, borderColor: "warning.main", borderRadius: 1 }}>
                <Typography variant="body2" color="warning.main">
                  {t("team.unassignedCount", { count: unassigned })}
                </Typography>
              </Box>
            )}
          </Stack>
        )}
      </DialogContent>
    </DraggableDialog>
  );
};

export default TeamStatsDialog;
