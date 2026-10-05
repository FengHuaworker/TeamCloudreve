import {
  Avatar,
  AvatarGroup,
  Box,
  Button,
  Chip,
  DialogContent,
  IconButton,
  MenuItem,
  Paper,
  Skeleton,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import dayjs from "dayjs";
import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  BOARD_STATUSES,
  sendCreateTask,
  sendMoveTask,
  TeamBoard as TeamBoardData,
  TeamPriority,
  TeamTask,
  TeamTaskStatus,
} from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import { DenseFilledTextField } from "../Common/StyledComponents.tsx";
import DraggableDialog from "../Dialogs/DraggableDialog.tsx";
import Add from "../Icons/Add.tsx";
import CommentMultiple from "../Icons/CommentMultiple.tsx";
import Document from "../Icons/Document.tsx";
import Warning from "../Icons/Warning.tsx";
import { SummaryButton } from "../Pages/Tasks/TaskCard.tsx";
import TaskDetailDialog from "./TaskDetailDialog.tsx";

/** 优先级对应的 MUI 配色（与 Cloudreve 主题一致） */
const priorityColor: Record<TeamPriority, "default" | "info" | "warning" | "error"> = {
  low: "default",
  normal: "info",
  high: "warning",
  urgent: "error",
};

interface TaskCardProps {
  task: TeamTask;
  onClick: () => void;
  onDragStart: (taskId: number) => void;
  dragging: boolean;
}

/**
 * 看板卡片。
 *
 * 复用 Cloudreve 自己的 SummaryButton，因此 hover 变色、250ms 缓动曲线、
 * 圆角与「后台任务」等页面完全一致；拖拽用原生 HTML5 DnD，不引入额外依赖。
 */
const TaskCard = ({ task, onClick, onDragStart, dragging }: TaskCardProps) => {
  const { t } = useTranslation();
  return (
    <SummaryButton
      expanded={false}
      draggable
      onDragStart={() => onDragStart(task.id)}
      onClick={onClick}
      sx={{
        p: 1.5,
        minHeight: 0,
        width: "100%",
        display: "block",
        textAlign: "left",
        cursor: "grab",
        transition: "all 250ms cubic-bezier(0.4, 0, 0.2, 1) 0ms",
        opacity: dragging ? 0.4 : 1,
        transform: dragging ? "scale(0.98)" : "none",
        "&:active": { cursor: "grabbing" },
      }}
    >
      <Typography variant="body2" fontWeight={500} sx={{ wordBreak: "break-word" }}>
        {task.title}
      </Typography>

      <Stack direction="row" spacing={0.5} sx={{ mt: 1, flexWrap: "wrap", gap: 0.5 }}>
        <Chip size="small" label={t(`team.priority_${task.priority}`)} color={priorityColor[task.priority]} />
        {task.overdue && (
          <Tooltip title={t("team.overdue")}>
            <Chip
              size="small"
              icon={<Warning />}
              color="error"
              label={t("team.overdue")}
              sx={{ "& .MuiChip-icon": { fontSize: 14 } }}
            />
          </Tooltip>
        )}
      </Stack>

      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mt: 1 }}>
        <Stack direction="row" spacing={0.5} alignItems="center" sx={{ minWidth: 0 }}>
          {task.assignee ? (
            <>
              <Avatar src={task.assignee.avatar} sx={{ width: 20, height: 20, fontSize: 12 }}>
                {task.assignee.nickname?.[0]}
              </Avatar>
              <Typography variant="caption" color="text.secondary" noWrap>
                {task.assignee.nickname}
              </Typography>
            </>
          ) : (
            <Typography variant="caption" color="text.disabled">
              {t("team.unassigned")}
            </Typography>
          )}

          {/* 协作者：叠放头像，让「一个任务多人参与」在看板上直接可见 */}
          {(task.collaborators ?? []).length > 0 && (
            <Tooltip title={(task.collaborators ?? []).map((c) => c.nickname).join("、")}>
              <AvatarGroup
                max={3}
                sx={{
                  ml: 0.5,
                  "& .MuiAvatar-root": {
                    width: 20,
                    height: 20,
                    fontSize: 11,
                    borderWidth: 1,
                  },
                }}
              >
                {(task.collaborators ?? []).map((c) => (
                  <Avatar key={c.id} src={c.avatar} alt={c.nickname}>
                    {c.nickname?.[0]}
                  </Avatar>
                ))}
              </AvatarGroup>
            </Tooltip>
          )}
        </Stack>

        <Stack direction="row" spacing={1} alignItems="center" sx={{ flexShrink: 0 }}>
          {/* 用 Cloudreve 的线性图标替代原来的 emoji，避免与站内图标语言割裂 */}
          {task.comment_total > 0 && (
            <Tooltip title={t("team.comments")}>
              <Stack direction="row" alignItems="center" spacing={0.25} sx={{ color: "text.secondary" }}>
                <CommentMultiple sx={{ fontSize: 15 }} />
                <Typography variant="caption">{task.comment_total}</Typography>
              </Stack>
            </Tooltip>
          )}
          {task.attachment_total > 0 && (
            <Tooltip title={t("team.attachments")}>
              <Stack direction="row" alignItems="center" spacing={0.25} sx={{ color: "text.secondary" }}>
                <Document sx={{ fontSize: 15 }} />
                <Typography variant="caption">{task.attachment_total}</Typography>
              </Stack>
            </Tooltip>
          )}
          {task.due_at && (
            <Typography variant="caption" color={task.overdue ? "error" : "text.secondary"}>
              {dayjs(task.due_at).format("MM-DD")}
            </Typography>
          )}
        </Stack>
      </Stack>

      {task.progress > 0 && (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: "block" }}>
          {t("team.progressLabel")} {task.progress}%
        </Typography>
      )}
    </SummaryButton>
  );
};

/** 列骨架屏：与列表页保持同一种加载语言 */
const ColumnSkeleton = () => (
  <Paper variant="outlined" sx={{ width: 280, flexShrink: 0, p: 1.5 }}>
    <Skeleton variant="text" width={80} />
    <Stack spacing={1} sx={{ mt: 1 }}>
      {[...Array(2)].map((_, i) => (
        <Skeleton key={i} variant="rounded" height={72} />
      ))}
    </Stack>
  </Paper>
);

export interface TeamBoardViewProps {
  projectId: number;
  board?: TeamBoardData;
  loading: boolean;
  /** 数据变更后通知外层重新拉取看板 */
  reload: () => void;
}

/**
 * 任务看板（项目页的「看板」tab 内容）。
 *
 * 只负责看板本身的交互：拖拽换列、打开任务详情、新建任务。
 * 页头、tab、项目级操作由 TeamProject 外壳统一提供，避免各 tab 各画一套头。
 */
const TeamBoardView = ({ projectId, board, loading, reload }: TeamBoardViewProps) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();

  const [draggingId, setDraggingId] = useState<number | null>(null);
  const [hoverStatus, setHoverStatus] = useState<TeamTaskStatus | null>(null);
  const [openTaskId, setOpenTaskId] = useState<number | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [createStatus, setCreateStatus] = useState<TeamTaskStatus>(TeamTaskStatus.todo);
  const [newTitle, setNewTitle] = useState("");
  const [newDesc, setNewDesc] = useState("");
  const [newPriority, setNewPriority] = useState<TeamPriority>(TeamPriority.normal);
  /**
   * 负责人。存的是 Cloudreve **hashid** 字符串，`"none"` 表示「待认领」。
   *
   * 两个要点：
   *  1. 必须是 hashid —— 成员的 `id` 就是 hashid，此前这里做了 `Number(m.id)`
   *     得到 NaN，导致指派从未生效；
   *  2. 哨兵值必须是**非空**字符串 —— MUI 的 Select 在值为空串时不会渲染任何文案，
   *     下拉框会变成一个空白框。
   */
  const [newAssignee, setNewAssignee] = useState<string>("none");
  const [newDue, setNewDue] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const memberOptions = useMemo(() => board?.members ?? [], [board]);
  const boardRef = useRef<HTMLDivElement>(null);

  /**
   * 拖拽到看板左右边缘时自动横向滚动。
   *
   * 列数超出一屏时必须先松手、滚动、再重新拖，很打断节奏；
   * 这里在离边缘 80px 内按帧微调 scrollLeft，手感接近原生拖拽。
   */
  const onBoardDragOver = (e: React.DragEvent<HTMLDivElement>) => {
    const el = boardRef.current;
    if (!el || draggingId === null) return;
    const rect = el.getBoundingClientRect();
    const EDGE = 80;
    const STEP = 18;
    if (e.clientX - rect.left < EDGE) {
      el.scrollLeft -= STEP;
    } else if (rect.right - e.clientX < EDGE) {
      el.scrollLeft += STEP;
    }
  };

  const onDrop = (status: TeamTaskStatus) => {
    setHoverStatus(null);
    const taskId = draggingId;
    setDraggingId(null);
    if (!taskId) return;

    const task = board?.columns.flatMap((c) => c.tasks).find((x) => x.id === taskId);
    if (!task || task.status === status) return;

    // 乐观更新：先本地移动，再提交；失败或成功后都重新拉取，保证与服务端一致
    const sortOrder = Date.now() % 100000;
    dispatch(sendMoveTask(taskId, { status, sort_order: sortOrder })).then(() => reload());
  };

  const onCreate = () => {
    if (!newTitle.trim()) return;
    setSubmitting(true);
    dispatch(
      sendCreateTask({
        project_id: projectId,
        title: newTitle.trim(),
        description: newDesc,
        status: createStatus,
        priority: newPriority,
        assignee_id: newAssignee === "none" ? undefined : newAssignee,
        due_at: newDue ? dayjs(newDue).toISOString() : undefined,
      }),
    )
      .then(() => {
        setCreateOpen(false);
        setNewTitle("");
        setNewDesc("");
        setNewDue("");
        setNewAssignee("none");
        reload();
      })
      .finally(() => setSubmitting(false));
  };

  return (
    <>
      <Stack direction="row" justifyContent="flex-end" sx={{ mb: 2 }}>
        <Button
          variant="contained"
          startIcon={<Add />}
          onClick={() => {
            setCreateStatus(TeamTaskStatus.todo);
            setCreateOpen(true);
          }}
        >
          {t("team.newTask")}
        </Button>
      </Stack>

      <Box
        ref={boardRef}
        onDragOver={onBoardDragOver}
        sx={{
          display: "flex",
          gap: 2,
          overflowX: "auto",
          alignItems: "flex-start",
          pb: 2,
          minHeight: 300,
        }}
      >
        {loading && !board
          ? BOARD_STATUSES.map((status) => <ColumnSkeleton key={status} />)
          : BOARD_STATUSES.map((status) => {
              const column = board?.columns.find((c) => c.status === status);
              const tasks = column?.tasks ?? [];
              const hovering = hoverStatus === status;
              return (
                <Paper
                  key={status}
                  variant="outlined"
                  onDragOver={(e) => {
                    e.preventDefault();
                    setHoverStatus(status);
                  }}
                  onDragLeave={() => setHoverStatus((s) => (s === status ? null : s))}
                  onDrop={() => onDrop(status)}
                  sx={{
                    width: 280,
                    flexShrink: 0,
                    p: 1.5,
                    borderColor: hovering ? "primary.main" : "divider",
                    bgcolor: hovering ? "action.hover" : "background.paper",
                    transition: "background-color 250ms cubic-bezier(0.4, 0, 0.2, 1) 0ms, border-color 250ms cubic-bezier(0.4, 0, 0.2, 1) 0ms",
                  }}
                >
                  <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 1 }}>
                    <Stack direction="row" alignItems="center" spacing={1}>
                      <Typography variant="subtitle2" fontWeight={600}>
                        {t(`team.status_${status}`)}
                      </Typography>
                      <Chip size="small" variant="outlined" label={tasks.length} sx={{ height: 18, fontSize: 11 }} />
                    </Stack>
                    <Tooltip title={t("team.newTask")}>
                      <IconButton
                        size="small"
                        onClick={() => {
                          setCreateStatus(status);
                          setCreateOpen(true);
                        }}
                      >
                        <Add sx={{ fontSize: 18 }} />
                      </IconButton>
                    </Tooltip>
                  </Stack>

                  <Stack spacing={1}>
                    {tasks.map((task) => (
                      <TaskCard
                        key={task.id}
                        task={task}
                        dragging={draggingId === task.id}
                        onDragStart={setDraggingId}
                        onClick={() => setOpenTaskId(task.id)}
                      />
                    ))}

                    {tasks.length === 0 && (
                      <Box
                        sx={{
                          py: 3,
                          textAlign: "center",
                          border: "1px dashed",
                          borderColor: hovering ? "primary.main" : "divider",
                          borderRadius: 1,
                          transition: "border-color 250ms cubic-bezier(0.4, 0, 0.2, 1) 0ms",
                        }}
                      >
                        <Typography variant="caption" color="text.disabled">
                          {t("team.emptyColumn")}
                        </Typography>
                      </Box>
                    )}
                  </Stack>
                </Paper>
              );
            })}
      </Box>

      {openTaskId !== null && (
        <TaskDetailDialog
          taskId={openTaskId}
          members={memberOptions}
          onClose={() => setOpenTaskId(null)}
          onChanged={reload}
        />
      )}

      <DraggableDialog
        title={t("team.newTask")}
        showActions
        showCancel
        loading={submitting}
        disabled={!newTitle.trim()}
        onAccept={onCreate}
        dialogProps={{
          open: createOpen,
          onClose: () => setCreateOpen(false),
          fullWidth: true,
          maxWidth: "sm",
        }}
      >
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <DenseFilledTextField
              autoFocus
              fullWidth
              required
              label={t("team.taskTitle")}
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
            />
            <DenseFilledTextField
              fullWidth
              multiline
              rows={4}
              label={t("team.taskDescription")}
              value={newDesc}
              onChange={(e) => setNewDesc(e.target.value)}
            />
            <Stack direction="row" spacing={2}>
              <DenseFilledTextField
                select
                fullWidth
                value={createStatus}
                label={t("team.status")}
                onChange={(e) => setCreateStatus(e.target.value as TeamTaskStatus)}
              >
                {BOARD_STATUSES.map((s) => (
                  <MenuItem key={s} value={s}>
                    {t(`team.status_${s}`)}
                  </MenuItem>
                ))}
              </DenseFilledTextField>
              <DenseFilledTextField
                select
                fullWidth
                value={newPriority}
                label={t("team.priority")}
                onChange={(e) => setNewPriority(e.target.value as TeamPriority)}
              >
                {Object.values(TeamPriority).map((p) => (
                  <MenuItem key={p} value={p}>
                    {t(`team.priority_${p}`)}
                  </MenuItem>
                ))}
              </DenseFilledTextField>
            </Stack>
            <Stack direction="row" spacing={2}>
              <DenseFilledTextField
                select
                fullWidth
                value={newAssignee}
                label={t("team.assignee")}
                onChange={(e) => setNewAssignee(e.target.value)}
              >
                <MenuItem value="none">
                  <em>{t("team.unassigned")}</em>
                </MenuItem>
                {memberOptions.map((m) => (
                  <MenuItem key={m.id} value={m.id}>
                    {m.nickname}
                  </MenuItem>
                ))}
              </DenseFilledTextField>
              <DenseFilledTextField
                fullWidth
                type="date"
                label={t("team.dueAt")}
                slotProps={{ inputLabel: { shrink: true } }}
                value={newDue}
                onChange={(e) => setNewDue(e.target.value)}
              />
            </Stack>
          </Stack>
        </DialogContent>
      </DraggableDialog>
    </>
  );
};

export default TeamBoardView;
