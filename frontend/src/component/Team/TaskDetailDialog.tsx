import {
  Autocomplete,
  Avatar,
  Box,
  Button,
  Chip,
  DialogContent,
  Divider,
  IconButton,
  LinearProgress,
  List,
  ListItemButton,
  ListItemText,
  MenuItem,
  Stack,
  Tab,
  Tabs,
  Typography,
} from "@mui/material";
import dayjs from "dayjs";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { FileResponse, FileType, ListResponse } from "../../api/explorer.ts";
import { getFileList } from "../../api/api.ts";
import {
  getTeamAttachments,
  getTeamComments,
  getTeamTask,
  sendAddTaskCollaborator,
  sendAttachFile,
  sendCreateComment,
  sendDeleteAttachment,
  sendRemoveTaskCollaborator,
  sendUpdateTask,
  TeamAttachment,
  TeamComment,
  TeamPriority,
  TeamTask,
  TeamTaskStatus,
  TeamUserBrief,
} from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import { DenseFilledTextField } from "../Common/StyledComponents.tsx";
import DraggableDialog from "../Dialogs/DraggableDialog.tsx";
import Delete from "../Icons/DeleteOutlined.tsx";
import Document from "../Icons/Document.tsx";
import Folder from "../Icons/Folder.tsx";

const BOARD_STATUSES: TeamTaskStatus[] = [
  TeamTaskStatus.todo,
  TeamTaskStatus.doing,
  TeamTaskStatus.review,
  TeamTaskStatus.done,
];

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

/** 简易的 Cloudreve 文件选择器：从我的文件根目录开始浏览并选中一个文件。
 *  导出供讨论区附件复用，避免两处各写一套文件浏览逻辑。 */
export const FilePicker = ({ onPick }: { onPick: (f: FileResponse) => void }) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const [uri, setUri] = useState("cloudreve://my");
  const [files, setFiles] = useState<FileResponse[]>([]);
  const [loading, setLoading] = useState(false);

  const load = useCallback(
    (target: string) => {
      setLoading(true);
      dispatch(getFileList({ uri: target, page_size: 200 }))
        .then((res: ListResponse) => {
          setFiles(res.files ?? []);
          setUri(target);
        })
        .catch(() => setFiles([]))
        .finally(() => setLoading(false));
    },
    [dispatch],
  );

  useEffect(() => {
    load("cloudreve://my");
  }, [load]);

  return (
    <Box sx={{ height: 320, overflowY: "auto", border: 1, borderColor: "divider", borderRadius: 1 }}>
      {loading && <LinearProgress />}
      <List dense>
        {files.map((f) => (
          <ListItemButton
            key={f.id}
            onClick={() => {
              if (f.type === FileType.folder) {
                // 仅支持在「我的文件」内向下浏览
                const base = uri.endsWith("/") ? uri.slice(0, -1) : uri;
                load(`${base}/${f.name}`);
              } else {
                onPick(f);
              }
            }}
          >
            {f.type === FileType.folder ? (
              <Folder fontSize="small" style={{ marginRight: 8 }} />
            ) : (
              <Document fontSize="small" style={{ marginRight: 8 }} />
            )}
            <ListItemText
              primary={f.name}
              secondary={f.type === FileType.file ? formatSize(f.size) : undefined}
            />
          </ListItemButton>
        ))}
        {!loading && files.length === 0 && (
          <Box sx={{ p: 3, textAlign: "center" }}>
            <Typography variant="body2" color="text.secondary">
              {t("team.emptyFolder")}
            </Typography>
          </Box>
        )}
      </List>
    </Box>
  );
};

interface TaskDetailDialogProps {
  taskId: number;
  members: TeamUserBrief[];
  onClose: () => void;
  onChanged: () => void;
}

/**
 * 任务详情弹窗：编辑任务、评论讨论、附件（引用 Cloudreve 文件）。
 */
const TaskDetailDialog = ({ taskId, members, onClose, onChanged }: TaskDetailDialogProps) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();

  const [task, setTask] = useState<TeamTask | undefined>();
  const [comments, setComments] = useState<TeamComment[]>([]);
  const [attachments, setAttachments] = useState<TeamAttachment[]>([]);
  const [tab, setTab] = useState(0);

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [status, setStatus] = useState<TeamTaskStatus>(TeamTaskStatus.todo);
  const [priority, setPriority] = useState<TeamPriority>(TeamPriority.normal);
  const [progress, setProgress] = useState(0);
  /** 负责人 hashid；`"none"` 表示未指派（同上：必须用非空哨兵值，MUI 才会渲染文案） */
  const [assignee, setAssignee] = useState<string>("none");
  const [dueAt, setDueAt] = useState("");
  const [saving, setSaving] = useState(false);

  const [newComment, setNewComment] = useState("");
  const [pickerOpen, setPickerOpen] = useState(false);

  const load = useCallback(() => {
    dispatch(getTeamTask(taskId)).then((tk) => {
      setTask(tk);
      setTitle(tk.title);
      setDescription(tk.description ?? "");
      setStatus(tk.status);
      setPriority(tk.priority);
      setProgress(tk.progress ?? 0);
      setAssignee(tk.assignee ? tk.assignee.id : "none");
      setDueAt(tk.due_at ? dayjs(tk.due_at).format("YYYY-MM-DD") : "");
    });
    dispatch(getTeamComments(taskId))
      .then((res) => setComments(res ?? []))
      .catch(() => setComments([]));
    dispatch(getTeamAttachments(taskId))
      .then((res) => setAttachments(res ?? []))
      .catch(() => setAttachments([]));
  }, [dispatch, taskId]);

  useEffect(() => {
    load();
  }, [load]);

  const onSave = () => {
    setSaving(true);
    dispatch(
      sendUpdateTask(taskId, {
        title,
        description,
        status,
        priority,
        progress,
        // 空串 = 取消指派；非空 = 该 hashid 对应的成员
        assignee_id: assignee === "none" ? "" : assignee,
        due_at: dueAt ? dayjs(dueAt).toISOString() : "",
      }),
    )
      .then(() => {
        load();
        onChanged();
      })
      .finally(() => setSaving(false));
  };

  const onComment = () => {
    if (!newComment.trim()) return;
    dispatch(sendCreateComment(taskId, newComment.trim())).then((res) => {
      setComments(res ?? []);
      setNewComment("");
      onChanged();
    });
  };

  const onPickFile = (f: FileResponse) => {
    dispatch(sendAttachFile(taskId, f.id)).then((res) => {
      setAttachments(res ?? []);
      setPickerOpen(false);
      onChanged();
    });
  };

  const onRemoveAttachment = (id: number) => {
    dispatch(sendDeleteAttachment(id)).then(() => {
      setAttachments((prev) => prev.filter((a) => a.id !== id));
      onChanged();
    });
  };

  // ---------------- 协作者（任务看板的团队协作）----------------
  // 一个任务一个负责人，但可以有多名协作者共同参与。
  const onAddCollaborator = (userId: string) => {
    dispatch(sendAddTaskCollaborator(taskId, userId)).then((res) => {
      if (res) setTask(res);
      onChanged();
    });
  };

  const onRemoveCollaborator = (userId: string) => {
    dispatch(sendRemoveTaskCollaborator(taskId, userId)).then((res) => {
      if (res) setTask(res);
      onChanged();
    });
  };

  /** 候选协作者 = 项目成员 − 当前负责人 − 已是协作者的人 */
  const collaboratorCandidates = useMemo(
    () =>
      members.filter(
        (m) =>
          m.id !== task?.assignee?.id &&
          !(task?.collaborators ?? []).some((c) => c.id === m.id),
      ),
    [members, task],
  );

  return (
    <>
      <DraggableDialog
        title={t("team.taskDetail")}
        showActions
        showCancel
        loading={saving}
        disabled={!title.trim()}
        onAccept={onSave}
        dialogProps={{ open: true, onClose, fullWidth: true, maxWidth: "md" }}
      >
        <DialogContent>
          <Stack spacing={2}>
            <DenseFilledTextField
              fullWidth
              required
              label={t("team.taskTitle")}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
            <DenseFilledTextField
              fullWidth
              multiline
              rows={4}
              label={t("team.taskDescription")}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
            <Stack direction="row" spacing={2}>
              <DenseFilledTextField select
                fullWidth
                label={t("team.status")}
                value={status}
                onChange={(e) => setStatus(e.target.value as TeamTaskStatus)}
              >
                {BOARD_STATUSES.map((s) => (
                  <MenuItem key={s} value={s}>
                    {t(`team.status_${s}`)}
                  </MenuItem>
                ))}
              </DenseFilledTextField>
              <DenseFilledTextField select
                fullWidth
                label={t("team.priority")}
                value={priority}
                onChange={(e) => setPriority(e.target.value as TeamPriority)}
              >
                {Object.values(TeamPriority).map((p) => (
                  <MenuItem key={p} value={p}>
                    {t(`team.priority_${p}`)}
                  </MenuItem>
                ))}
              </DenseFilledTextField>
            </Stack>
            <Stack direction="row" spacing={2}>
              <DenseFilledTextField select
                fullWidth
                label={t("team.assignee")}
                value={assignee}
                onChange={(e) => setAssignee(e.target.value)}
              >
                <MenuItem value="none">
                  <em>{t("team.unassigned")}</em>
                </MenuItem>
                {members.map((m) => (
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
                value={dueAt}
                onChange={(e) => setDueAt(e.target.value)}
              />
            </Stack>
            <Stack direction="row" spacing={2} alignItems="center">
              <DenseFilledTextField
                type="number"
                label={t("team.progress")}
                slotProps={{ htmlInput: { min: 0, max: 100 } }}
                value={progress}
                onChange={(e) => setProgress(Number(e.target.value))}
                sx={{ width: 160 }}
              />
              <LinearProgress variant="determinate" value={progress} sx={{ flexGrow: 1 }} />
            </Stack>

            {/* 协作者：与「负责人」区分开——负责人是唯一责任人，协作者是共同参与者 */}
            <Stack direction="row" spacing={2} alignItems="flex-start">
              <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                <Typography variant="caption" color="text.secondary">
                  {t("team.collaborators")}
                </Typography>
                <Stack direction="row" spacing={0.5} sx={{ flexWrap: "wrap", gap: 0.5, mt: 0.5 }}>
                  {(task?.collaborators ?? []).map((c) => (
                    <Chip
                      key={c.id}
                      size="small"
                      avatar={
                        <Avatar src={c.avatar} sx={{ width: 18, height: 18, fontSize: 10 }}>
                          {c.nickname?.[0]}
                        </Avatar>
                      }
                      label={c.nickname}
                      onDelete={() => onRemoveCollaborator(c.id)}
                    />
                  ))}
                  {(task?.collaborators ?? []).length === 0 && (
                    <Typography variant="body2" color="text.disabled">
                      {t("team.noCollaborator")}
                    </Typography>
                  )}
                </Stack>
              </Box>
              <Autocomplete
                size="small"
                sx={{ width: 220, flexShrink: 0 }}
                options={collaboratorCandidates}
                getOptionLabel={(o) => o.nickname}
                value={null}
                blurOnSelect
                clearOnBlur
                onChange={(_, v) => v && onAddCollaborator(v.id)}
                renderInput={(p) => <DenseFilledTextField {...p} label={t("team.addCollaborator")} />}
              />
            </Stack>

            <Divider />

            <Tabs value={tab} onChange={(_, v) => setTab(v)}>
              <Tab label={`${t("team.comments")} (${comments.length})`} />
              <Tab label={`${t("team.attachments")} (${attachments.length})`} />
            </Tabs>

            {tab === 0 && (
              <Stack spacing={1.5}>
                {comments.map((c) => (
                  <Stack key={c.id} direction="row" spacing={1.5}>
                    <Avatar src={c.user?.avatar} sx={{ width: 32, height: 32 }}>
                      {c.user?.nickname?.[0]}
                    </Avatar>
                    <Box sx={{ flexGrow: 1 }}>
                      <Stack direction="row" spacing={1} alignItems="baseline">
                        <Typography variant="body2" fontWeight={600}>
                          {c.user?.nickname}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {dayjs(c.created_at).format("YYYY-MM-DD HH:mm")}
                        </Typography>
                      </Stack>
                      <Typography variant="body2" sx={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
                        {c.content}
                      </Typography>
                    </Box>
                  </Stack>
                ))}
                {comments.length === 0 && (
                  <Typography variant="body2" color="text.secondary">
                    {t("team.noComment")}
                  </Typography>
                )}
                <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
                  <DenseFilledTextField
                    fullWidth
                    multiline
                    rows={2}
                    placeholder={t("team.writeComment")}
                    value={newComment}
                    onChange={(e) => setNewComment(e.target.value)}
                  />
                  <Button variant="contained" disabled={!newComment.trim()} onClick={onComment}>
                    {t("team.send")}
                  </Button>
                </Stack>
              </Stack>
            )}

            {tab === 1 && (
              <Stack spacing={1}>
                {attachments.map((a) => (
                  <Stack
                    key={a.id}
                    direction="row"
                    alignItems="center"
                    spacing={1}
                    sx={{ p: 1, border: 1, borderColor: "divider", borderRadius: 1 }}
                  >
                    <Document fontSize="small" />
                    <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                      <Typography variant="body2" noWrap>
                        {a.name}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {formatSize(a.size)} · {a.user?.nickname}
                      </Typography>
                    </Box>
                    <Chip size="small" variant="outlined" label={t("team.fileRef")} />
                    <IconButton size="small" onClick={() => onRemoveAttachment(a.id)}>
                      <Delete fontSize="small" />
                    </IconButton>
                  </Stack>
                ))}
                {attachments.length === 0 && (
                  <Typography variant="body2" color="text.secondary">
                    {t("team.noAttachment")}
                  </Typography>
                )}
                <Button variant="outlined" onClick={() => setPickerOpen(true)} sx={{ alignSelf: "flex-start" }}>
                  {t("team.attachFile")}
                </Button>
                <Typography variant="caption" color="text.secondary">
                  {t("team.attachFileDes")}
                </Typography>
              </Stack>
            )}
          </Stack>
        </DialogContent>
      </DraggableDialog>

      <DraggableDialog
        title={t("team.pickFile")}
        showActions
        showCancel
        hideOk
        dialogProps={{
          open: pickerOpen,
          onClose: () => setPickerOpen(false),
          fullWidth: true,
          maxWidth: "sm",
        }}
      >
        <DialogContent>
          <FilePicker onPick={onPickFile} />
        </DialogContent>
      </DraggableDialog>
    </>
  );
};

export default TaskDetailDialog;
