import {
  Autocomplete,
  Avatar,
  Box,
  Button,
  Chip,
  DialogContent,
  IconButton,
  MenuItem,
  Stack,
  Typography,
} from "@mui/material";
import dayjs from "dayjs";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { getSearchUser } from "../../api/api.ts";
import { User } from "../../api/user.ts";
import { getTeamMembers, sendAddMember, sendRemoveMember, TeamMember } from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import { DenseFilledTextField } from "../Common/StyledComponents.tsx";
import DraggableDialog from "../Dialogs/DraggableDialog.tsx";
import Delete from "../Icons/DeleteOutlined.tsx";

const ROLE_KEYS = ["admin", "member", "viewer"] as const;

interface TeamMembersDialogProps {
  projectId: number;
  onClose: () => void;
  onChanged?: () => void;
}

/**
 * 项目成员管理：添加/移除成员与调整角色。
 *
 * 注意：项目 owner 由项目创建者固定持有，不可在此移除或改角色；
 * 全局管理员（Cloudreve 用户组权限）无需加入成员即可管理任意项目。
 */
const TeamMembersDialog = ({ projectId, onClose, onChanged }: TeamMembersDialogProps) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();

  const [members, setMembers] = useState<TeamMember[]>([]);
  const [loading, setLoading] = useState(false);

  const [options, setOptions] = useState<User[]>([]);
  const [picked, setPicked] = useState<User | null>(null);
  const [role, setRole] = useState<string>("member");
  const [keyword, setKeyword] = useState("");
  const [adding, setAdding] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    dispatch(getTeamMembers(projectId))
      .then((res) => setMembers(res ?? []))
      .catch(() => setMembers([]))
      .finally(() => setLoading(false));
  }, [dispatch, projectId]);

  useEffect(() => {
    load();
  }, [load]);

  // 用户搜索（复用 Cloudreve 的 /user/search）
  useEffect(() => {
    const kw = keyword.trim();
    if (!kw) {
      setOptions([]);
      return;
    }
    const timer = setTimeout(() => {
      dispatch(getSearchUser(kw))
        .then((res) => setOptions(res ?? []))
        .catch(() => setOptions([]));
    }, 300);
    return () => clearTimeout(timer);
  }, [dispatch, keyword]);

  const onAdd = () => {
    if (!picked) return;
    setAdding(true);
    dispatch(sendAddMember(projectId, { user_id: picked.id, role }))
      .then(() => {
        setPicked(null);
        setKeyword("");
        load();
        onChanged?.();
      })
      .finally(() => setAdding(false));
  };

  const onRemove = (userId: string) => {
    dispatch(sendRemoveMember(projectId, userId)).then(() => {
      load();
      onChanged?.();
    });
  };

  return (
    <DraggableDialog
      title={t("team.memberManage")}
      showActions
      showCancel
      hideOk
      cancelText={t("common:close")}
      dialogProps={{ open: true, onClose, fullWidth: true, maxWidth: "sm" }}
    >
      <DialogContent>
        <Stack spacing={2}>
          {/* 添加成员 */}
          <Stack direction="row" spacing={1} alignItems="flex-start">
            <Autocomplete
              fullWidth
              size="small"
              options={options}
              value={picked}
              loading={loading}
              getOptionLabel={(o) => `${o.nickname}${o.email ? ` (${o.email})` : ""}`}
              isOptionEqualToValue={(a, b) => a.id === b.id}
              onChange={(_, v) => setPicked(v)}
              onInputChange={(_, v) => setKeyword(v)}
              noOptionsText={keyword.trim() ? t("team.noUserFound") : t("team.searchUserHint")}
              renderInput={(params) => (
                <DenseFilledTextField {...params} label={t("team.searchUser")} />
              )}
            />
            <DenseFilledTextField
              select
              size="small"
              label={t("team.role")}
              value={role}
              onChange={(e) => setRole(e.target.value)}
              sx={{ minWidth: 130 }}
            >
              {ROLE_KEYS.map((r) => (
                <MenuItem key={r} value={r}>
                  {t(`team.role_${r}`)}
                </MenuItem>
              ))}
            </DenseFilledTextField>
            <Button variant="contained" disabled={!picked || adding} onClick={onAdd} sx={{ mt: 0.25 }}>
              {t("team.add")}
            </Button>
          </Stack>

          {/* 成员列表 */}
          <Stack spacing={1}>
            {members.map((m) => {
              const isOwner = m.role === "owner";
              return (
                <Stack
                  key={m.user?.id ?? m.role}
                  direction="row"
                  alignItems="center"
                  spacing={1.5}
                  sx={{ p: 1, border: 1, borderColor: "divider", borderRadius: 1 }}
                >
                  <Avatar src={m.user?.avatar} sx={{ width: 32, height: 32 }}>
                    {m.user?.nickname?.[0]}
                  </Avatar>
                  <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                    <Typography variant="body2" noWrap>
                      {m.user?.nickname ?? t("team.unknownUser")}
                    </Typography>
                    <Typography variant="caption" color="text.secondary" noWrap>
                      {m.user?.email}
                      {m.joined_at ? ` · ${dayjs(m.joined_at).format("YYYY-MM-DD")}` : ""}
                    </Typography>
                  </Box>
                  <Chip
                    size="small"
                    color={isOwner ? "primary" : "default"}
                    variant={isOwner ? "filled" : "outlined"}
                    label={t(`team.role_${m.role}`)}
                  />
                  {!isOwner && (
                    <IconButton size="small" onClick={() => onRemove(m.user?.id ?? "")}>
                      <Delete fontSize="small" />
                    </IconButton>
                  )}
                </Stack>
              );
            })}
            {members.length === 0 && (
              <Typography variant="body2" color="text.secondary">
                {t("team.noMember")}
              </Typography>
            )}
          </Stack>

          <Typography variant="caption" color="text.secondary">
            {t("team.memberHint")}
          </Typography>
        </Stack>
      </DialogContent>
    </DraggableDialog>
  );
};

export default TeamMembersDialog;
