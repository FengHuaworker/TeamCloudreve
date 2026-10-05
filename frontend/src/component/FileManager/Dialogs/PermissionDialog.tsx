import { Box, Chip, DialogContent, IconButton, Stack, Switch, Tooltip, Typography } from "@mui/material";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AclEntry,
  AclPermission,
  FileAclResponse,
  getFileAcl,
  sendDeleteFileAcl,
  sendSetFileAcl,
} from "../../../api/fileacl.ts";
import { User } from "../../../api/user.ts";
import { AppError } from "../../../api/request.ts";
import { useAppDispatch, useAppSelector } from "../../../redux/hooks.ts";
import { closeFileAclDialog } from "../../../redux/globalStateSlice.ts";
import UserSearchInput from "../../Admin/File/UserSearchInput.tsx";
import { DenseAutocomplete, DenseFilledTextField } from "../../Common/StyledComponents.tsx";
import DraggableDialog from "../../Dialogs/DraggableDialog.tsx";
import Nothing from "../../Common/Nothing.tsx";
import DeleteOutlined from "../../Icons/DeleteOutlined.tsx";

/**
 * 文件访问控制（ACL）对话框。
 *
 * 设计口径（用户指令：与 Cloudreve 原版融为一体）：
 *  · 复用原版组件：DraggableDialog（标题栏 X 关闭）/ UserSearchInput（管理员面板同款
 *    用户搜索）/ Nothing 空态；视觉与 Pin、分享等原版对话框一致。
 *  · i18n 全部挂 application:fileManager.*（原版命名空间）。
 *  · 入口在原版右键菜单（share 旁），不另设"TeamCloudreve 设置"岛。
 *
 * 权限口径 = 后端规则的镜像：
 *  · 查看 403/404 -> 内联"无权管理"（bypassSnackbar 在 api 层），不弹全局红条；
 *  · 成功设置后用响应回填列表（server 返回的就是刷新后的列表）。
 *
 * 已知缺口（交付报告注明，待 Peer 仲裁）：
 *  用户组（subject_type=group）后端支持，但【没有可用的组列表接口给前端】
 *  （/admin/group 是 admin-only 且返回裸整型 id，而 fileacl 要 hashid；
 *   前端无 hashids 编码库）。v1 先开放"用户"主体，组主体等接口口径定了再补。
 */
const PermissionDialog = () => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();

  const open = useAppSelector((state) => state.globalState.fileAclDialogOpen);
  const uri = useAppSelector((state) => state.globalState.fileAclDialogUri);
  const name = useAppSelector((state) => state.globalState.fileAclDialogName);

  const [loading, setLoading] = useState(false);
  const [denied, setDenied] = useState(false);
  const [resp, setResp] = useState<FileAclResponse | undefined>(undefined);
  const [subject, setSubject] = useState<User | null>(null);
  const [permission, setPermission] = useState<AclPermission>("write");
  const [inherit, setInherit] = useState(true);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    if (!uri) return;
    setLoading(true);
    dispatch(getFileAcl(uri))
      .then((res: FileAclResponse) => {
        setResp(res);
        setDenied(false);
      })
      .catch((e) => {
        // 403/404 = 权限答案 -> 内联隐藏；其它错误已由全局 toast 提示
        if (e instanceof AppError && (e.code === 403 || e.code === 404)) {
          setDenied(true);
        }
        setResp(undefined);
      })
      .finally(() => setLoading(false));
  }, [dispatch, uri]);

  useEffect(() => {
    if (open && uri) {
      setSubject(null);
      setPermission("write");
      setInherit(true);
      setDenied(false);
      setResp(undefined);
      load();
    }
  }, [open, uri, load]);

  const onClose = useCallback(() => {
    if (!busy) {
      dispatch(closeFileAclDialog());
    }
  }, [dispatch, busy]);

  const onAdd = useCallback(() => {
    if (!uri || !subject) return;
    setBusy(true);
    dispatch(
      sendSetFileAcl({
        uri,
        subject_type: "user",
        subject_id: subject.id,
        permission,
        inherit,
      }),
    )
      .then((res: FileAclResponse) => {
        setResp(res);
        setSubject(null);
      })
      .catch(() => {
        // 后端拒绝（含 owner 锁死保护的人话文案）已由全局 toast 呈现
      })
      .finally(() => setBusy(false));
  }, [dispatch, uri, subject, permission, inherit]);

  const onDelete = useCallback(
    (entry: AclEntry) => {
      if (!uri || busy) return;
      setBusy(true);
      dispatch(
        sendDeleteFileAcl({
          uri,
          subject_type: entry.subject_type,
          subject_id: entry.subject?.id ?? String(entry.subject_id),
        }),
      )
        .then(() => load())
        .catch(() => {
          // 错误 toast 由全局处理
        })
        .finally(() => setBusy(false));
    },
    [dispatch, uri, busy, load],
  );

  const effective = resp?.effective?.[0];
  const entries = resp?.self ?? [];

  return (
    <DraggableDialog
      title={t("application:fileManager.aclTitle")}
      showActions
      showCancel
      loading={busy}
      disabled={!subject || denied}
      okText={t("application:fileManager.aclAdd")}
      onAccept={onAdd}
      dialogProps={{
        open: open ?? false,
        onClose: onClose,
        fullWidth: true,
        maxWidth: "sm",
      }}
    >
      <DialogContent>
        {denied ? (
          <Nothing
            primary={t("application:fileManager.aclDenied")}
            secondary={t("application:fileManager.aclDeniedHint")}
            size={0.55}
            top={30}
          />
        ) : (
          <>
            <Typography variant="caption" color="text.secondary">
              {name ? `${name} · ` : ""}
              {t("application:fileManager.aclHint")}
            </Typography>

            {effective && (
              <Box sx={{ mt: 1, display: "flex", alignItems: "center", gap: 1 }}>
                <Typography variant="caption" color="text.secondary">
                  {t("application:fileManager.aclEffective")}
                </Typography>
                <Chip
                  size="small"
                  color={effective.permission === "write" ? "primary" : "default"}
                  label={
                    effective.permission === "write"
                      ? t("application:fileManager.aclWrite")
                      : t("application:fileManager.aclRead")
                  }
                />
                {!effective.direct && effective.inherited_from && (
                  <Typography variant="caption" color="text.secondary">
                    {t("application:fileManager.aclFrom")} {effective.inherited_from}
                  </Typography>
                )}
              </Box>
            )}

            {/* 现有规则 */}
            <Box sx={{ mt: 2 }}>
              {entries.length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  {t("application:fileManager.aclEmpty")}
                </Typography>
              ) : (
                entries.map((e) => (
                  <Stack
                    key={e.id}
                    direction="row"
                    alignItems="center"
                    spacing={1}
                    sx={{ py: 0.5, borderBottom: "1px solid", borderColor: "divider" }}
                  >
                    <Chip
                      size="small"
                      variant="outlined"
                      label={e.subject_type === "group" ? t("application:fileManager.aclSubjectGroup") : t("application:fileManager.aclSubjectUser")}
                    />
                    <Typography variant="body2" sx={{ flexGrow: 1 }}>
                      {e.subject?.nickname ?? `#${e.subject_id}`}
                    </Typography>
                    <Chip
                      size="small"
                      color={e.permission === "write" ? "primary" : "default"}
                      label={
                        e.permission === "write"
                          ? t("application:fileManager.aclWrite")
                          : t("application:fileManager.aclRead")
                      }
                    />
                    {e.inherit && (
                      <Typography variant="caption" color="text.secondary">
                        {t("application:fileManager.aclInherit")}
                      </Typography>
                    )}
                    <Tooltip title={t("application:fileManager.aclRemove")}>
                      <IconButton size="small" disabled={busy} onClick={() => onDelete(e)}>
                        <DeleteOutlined fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </Stack>
                ))
              )}
            </Box>

            {/* 新增规则：主体搜索 + 读写 + 继承 */}
            <Box sx={{ mt: 2 }}>
              <UserSearchInput
                label={t("application:fileManager.aclPickUser")}
                onUserSelected={(u) => setSubject(u)}
              />
              <Stack direction="row" alignItems="center" spacing={2} sx={{ mt: 1 }}>
                <DenseAutocomplete
                  sx={{ minWidth: 140 }}
                  options={["write", "read"] as AclPermission[]}
                  value={permission}
                  onChange={(_e, v) => {
                    // DenseAutocomplete 的联合泛型在字面量选项下推断为 {}，显式收窄
                    if (v) setPermission(v as AclPermission);
                  }}
                  getOptionLabel={(o) =>
                    o === "write"
                      ? t("application:fileManager.aclWrite")
                      : t("application:fileManager.aclRead")
                  }
                  renderInput={(params) => (
                    <DenseFilledTextField
                      {...params}
                      variant="outlined"
                      margin="dense"
                      fullWidth
                    />
                  )}
                />
                <Stack direction="row" alignItems="center" spacing={0.5}>
                  <Switch
                    size="small"
                    checked={inherit}
                    onChange={(e) => setInherit(e.target.checked)}
                  />
                  <Typography variant="caption" color="text.secondary">
                    {t("application:fileManager.aclInherit")}
                  </Typography>
                </Stack>
              </Stack>
            </Box>
          </>
        )}
      </DialogContent>
    </DraggableDialog>
  );
};

export default PermissionDialog;
