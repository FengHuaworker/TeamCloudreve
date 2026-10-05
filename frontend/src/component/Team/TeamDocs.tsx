import {
  Autocomplete,
  Box,
  Button,
  Chip,
  Collapse,
  DialogContent,
  Divider,
  LinearProgress,
  List,
  ListItemButton,
  ListItemText,
  Menu,
  MenuItem,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import dayjs from "dayjs";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { getSearchUser } from "../../api/api.ts";
import { User } from "../../api/user.ts";
import {
  buildMention,
  getTeamDoc,
  getTeamDocTree,
  getTeamMembers,
  sendCreateDoc,
  sendDeleteDoc,
  sendUpdateDoc,
  TeamDocNode,
  TeamMember,
} from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import { DenseFilledTextField } from "../Common/StyledComponents.tsx";
import Nothing from "../Common/Nothing.tsx";
import DraggableDialog from "../Dialogs/DraggableDialog.tsx";
import Add from "../Icons/Add.tsx";
import Article from "../Icons/DocumentText.tsx";
import CaretDown from "../Icons/CaretDown.tsx";
import CaretRight from "../Icons/CaretRight.tsx";
import Delete from "../Icons/DeleteOutlined.tsx";
import Folder from "../Icons/Folder.tsx";
import AlternateEmail from "../Icons/MailOutlined.tsx";
import Save from "../Icons/Save.tsx";
import MarkdownEditor from "../Viewers/MarkdownEditor/Editor.tsx";

/** 文档树节点（递归渲染，自带折叠） */
const DocTreeNode = ({
  node,
  depth,
  activeId,
  onSelect,
  onAddChild,
  onDelete,
}: {
  node: TeamDocNode;
  depth: number;
  activeId: number | null;
  onSelect: (n: TeamDocNode) => void;
  onAddChild: (parent: TeamDocNode) => void;
  onDelete: (n: TeamDocNode) => void;
}) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(true);
  const [menu, setMenu] = useState<null | HTMLElement>(null);
  const hasChildren = !!node.children?.length;

  return (
    <>
      <ListItemButton
        selected={activeId === node.id}
        onClick={() => onSelect(node)}
        onContextMenu={(e) => {
          e.preventDefault();
          setMenu(e.currentTarget);
        }}
        sx={{ pl: 1 + depth * 1.5, py: 0.5 }}
      >
        <Box
          sx={{ width: 20, display: "flex", alignItems: "center", flexShrink: 0 }}
          onClick={(e) => {
            e.stopPropagation();
            setOpen((v) => !v);
          }}
        >
          {hasChildren ? open ? <CaretDown fontSize="small" /> : <CaretRight fontSize="small" /> : null}
        </Box>
        {node.is_folder ? (
          <Folder fontSize="small" style={{ marginRight: 6 }} />
        ) : (
          <Article fontSize="small" style={{ marginRight: 6 }} />
        )}
        <ListItemText
          primary={node.icon ? `${node.icon} ${node.title}` : node.title}
          slotProps={{ primary: { variant: "body2", noWrap: true } }}
        />
      </ListItemButton>

      <Menu anchorEl={menu} open={!!menu} onClose={() => setMenu(null)}>
        <MenuItem
          onClick={() => {
            setMenu(null);
            onAddChild(node);
          }}
        >
          <Add fontSize="small" style={{ marginRight: 8 }} />
          {t("team.newSubDoc")}
        </MenuItem>
        <MenuItem
          onClick={() => {
            setMenu(null);
            onDelete(node);
          }}
        >
          <Delete fontSize="small" style={{ marginRight: 8 }} />
          {t("team.deleteDoc")}
        </MenuItem>
      </Menu>

      {hasChildren && (
        <Collapse in={open} timeout="auto" unmountOnExit>
          {(node.children ?? []).map((c) => (
            <DocTreeNode
              key={c.id}
              node={c}
              depth={depth + 1}
              activeId={activeId}
              onSelect={onSelect}
              onAddChild={onAddChild}
              onDelete={onDelete}
            />
          ))}
        </Collapse>
      )}
    </>
  );
};

export interface TeamDocsViewProps {
  projectId: number;
}

/**
 * 项目文档（Wiki）：左侧页面树，右侧 Markdown 编辑器。
 * 编辑器复用 Cloudreve 内置的 MDXEditor 封装，未引入任何新依赖。
 *
 * 作为项目页的「文档」tab 内容，页头与 tab 由 TeamProject 统一提供。
 */
const TeamDocsView = ({ projectId }: TeamDocsViewProps) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();

  const [tree, setTree] = useState<TeamDocNode[]>([]);
  const [active, setActive] = useState<TeamDocNode | null>(null);
  const [content, setContent] = useState("");
  const [initialContent, setInitialContent] = useState("");
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(false);
  const [members, setMembers] = useState<TeamMember[]>([]);

  /**
   * 是否存在未保存改动：直接由内容比较得出，而不是靠 onChange 置位的布尔量。
   */
  const isDirty = content !== initialContent;

  /**
   * 抑制「编辑器刚挂载时的规范化回调」的时间窗。
   *
   * MarkdownEditor 内部的 MDXEditor 是等 i18n 命名空间加载完才挂载的
   * （`{nsLoaded && <MDXEditor .../>}`），挂载时它会解析 markdown 并回调一次 onChange，
   * 回调值是被规范化过的文本，与服务器原文不完全一致。
   *
   * 内容确实变了，所以单纯把 dirty 改成派生值并不能解决问题 ——
   * 必须把这次规范化回调并入基线。做法：文档载入后开一个 2 秒窗口，
   * 窗口内的**首次**回调视为「编辑器就绪」而非用户修改；窗口外或第二次回调一律算真实修改。
   */
  const settleUntilRef = useRef(0);

  const [createOpen, setCreateOpen] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [parentFor, setParentFor] = useState<TeamDocNode | null>(null);
  const [isFolder, setIsFolder] = useState(false);

  const [mentionOpen, setMentionOpen] = useState(false);
  const [mentionKeyword, setMentionKeyword] = useState("");
  const [mentionOptions, setMentionOptions] = useState<User[]>([]);

  const loadTree = useCallback(
    (selectFirst = false) => {
      if (!projectId) return;
      setLoading(true);
      dispatch(getTeamDocTree(projectId))
        .then((res) => {
          const nodes = res ?? [];
          setTree(nodes);
          if (selectFirst && !active && nodes.length > 0) {
            openDoc(nodes[0]);
          }
        })
        .catch(() => setTree([]))
        .finally(() => setLoading(false));
    },
    [dispatch, projectId, active],
  );

  const openDoc = useCallback(
    (node: TeamDocNode) => {
      if (node.is_folder) return;
      setActive(node);
      dispatch(getTeamDoc(node.id)).then((d) => {
        const raw = d.content ?? "";
        setContent(raw);
        setInitialContent(raw);
        // 打开上面的抑制窗口，等编辑器完成首次规范化回调
        settleUntilRef.current = Date.now() + 2000;
      });
    },
    [dispatch],
  );

  useEffect(() => {
    loadTree(true);
    dispatch(getTeamMembers(projectId))
      .then((res) => setMembers(res ?? []))
      .catch(() => setMembers([]));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId]);

  const onSave = useCallback(() => {
    if (!active || !isDirty) return;
    setSaving(true);
    dispatch(sendUpdateDoc(active.id, { content, title: active.title }))
      .then(() => {
        setInitialContent(content);
        loadTree();
      })
      .finally(() => setSaving(false));
  }, [active, content, isDirty, dispatch, loadTree]);

  const onCreate = () => {
    dispatch(
      sendCreateDoc({
        project_id: projectId,
        parent_id: parentFor?.id,
        title: newTitle.trim() || undefined,
        is_folder: isFolder,
      }),
    ).then((d) => {
      setCreateOpen(false);
      setNewTitle("");
      setIsFolder(false);
      setParentFor(null);
      loadTree();
      if (d && !d.is_folder) openDoc(d);
    });
  };

  const onDelete = (node: TeamDocNode) => {
    dispatch(sendDeleteDoc(node.id)).then(() => {
      if (active?.id === node.id) {
        setActive(null);
        setContent("");
        setInitialContent("");
      }
      loadTree();
    });
  };

  // 提及：复用 Cloudreve 的用户搜索
  useEffect(() => {
    const kw = mentionKeyword.trim();
    if (!kw) {
      setMentionOptions([]);
      return;
    }
    const timer = setTimeout(() => {
      dispatch(getSearchUser(kw))
        .then((res) => setMentionOptions(res ?? []))
        .catch(() => setMentionOptions([]));
    }, 300);
    return () => clearTimeout(timer);
  }, [dispatch, mentionKeyword]);

  const allMembers = useMemo(() => members.map((m) => m.user).filter(Boolean), [members]);

  return (
    <>
      <Stack direction="row" justifyContent="flex-end" spacing={1} sx={{ mb: 2 }}>
        <Button
          variant="outlined"
          startIcon={<AlternateEmail />}
          disabled={!active}
          onClick={() => setMentionOpen(true)}
        >
          {t("team.mention")}
        </Button>
        <Button
          variant="contained"
          startIcon={<Save />}
          disabled={!active || !isDirty || saving}
          onClick={onSave}
        >
          {t("team.save")}
        </Button>
        <Button
          variant="contained"
          startIcon={<Add />}
          onClick={() => {
            setParentFor(null);
            setCreateOpen(true);
          }}
        >
          {t("team.newDoc")}
        </Button>
      </Stack>

      <Box sx={{ display: "flex", gap: 2, alignItems: "flex-start" }}>
        {/* 左侧页面树 */}
        <Box
          sx={{
            width: 260,
            flexShrink: 0,
            border: 1,
            borderColor: "divider",
            borderRadius: 1,
            maxHeight: "calc(100vh - 220px)",
            overflowY: "auto",
          }}
        >
          <Typography variant="subtitle2" sx={{ px: 1.5, py: 1 }}>
            {t("team.docTree")}
          </Typography>
          <Divider />
          {loading && <LinearProgress />}
          {tree.length === 0 ? (
            <Box sx={{ p: 2 }}>
              <Typography variant="body2" color="text.secondary">
                {t("team.noDoc")}
              </Typography>
            </Box>
          ) : (
            <List dense disablePadding>
              {tree.map((n) => (
                <DocTreeNode
                  key={n.id}
                  node={n}
                  depth={0}
                  activeId={active?.id ?? null}
                  onSelect={openDoc}
                  onAddChild={(p) => {
                    setParentFor(p);
                    setCreateOpen(true);
                  }}
                  onDelete={onDelete}
                />
              ))}
            </List>
          )}
        </Box>

        {/* 右侧编辑器 */}
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          {!active ? (
            <Nothing primary={t("team.pickDoc")} secondary={t("team.pickDocDes")} />
          ) : (
            <>
              <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
                <Typography variant="h6" sx={{ flexGrow: 1 }}>
                  {active.icon ? `${active.icon} ` : ""}
                  {active.title}
                </Typography>
                {isDirty && <Chip size="small" color="warning" label={t("team.unsaved")} />}
                {active.editor && (
                  <Tooltip title={t("team.lastEditor", { name: active.editor.nickname })}>
                    <Typography variant="caption" color="text.secondary">
                      {dayjs(active.updated_at).format("MM-DD HH:mm")}
                    </Typography>
                  </Tooltip>
                )}
              </Stack>
              <MarkdownEditor
                value={content}
                initialValue={initialContent}
                onChange={(v) => {
                  setContent(v);
                  if (Date.now() < settleUntilRef.current) {
                    // 编辑器挂载后的规范化回调，不算用户修改；只吞一次
                    settleUntilRef.current = 0;
                    setInitialContent(v);
                  }
                }}
                onSaveShortcut={onSave}
              />
            </>
          )}
        </Box>
      </Box>

      {/* 新建文档 */}
      <DraggableDialog
        title={parentFor ? `${t("team.newSubDoc")} · ${parentFor.title}` : t("team.newDoc")}
        showActions
        showCancel
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
              label={t("team.docTitle")}
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
            />
            <Stack direction="row" spacing={1}>
              <Chip
                label={t("team.docTypePage")}
                color={!isFolder ? "primary" : "default"}
                onClick={() => setIsFolder(false)}
              />
              <Chip
                label={t("team.docTypeFolder")}
                color={isFolder ? "primary" : "default"}
                onClick={() => setIsFolder(true)}
              />
            </Stack>
          </Stack>
        </DialogContent>
      </DraggableDialog>

      {/* 插入 @提及 */}
      <DraggableDialog
        title={t("team.mention")}
        showActions
        showCancel
        hideOk
        cancelText={t("common:close")}
        dialogProps={{
          open: mentionOpen,
          onClose: () => setMentionOpen(false),
          fullWidth: true,
          maxWidth: "xs",
        }}
      >
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <Autocomplete
              fullWidth
              size="small"
              options={mentionOptions.length > 0 ? mentionOptions : (allMembers as User[])}
              getOptionLabel={(o) => o?.nickname ?? ""}
              onChange={(_, v) => {
                if (!v) return;
                // 追加到正文末尾：MDXEditor 未暴露 ref，这里采用追加方式
                setContent((prev) => `${prev}\n\n${buildMention({ id: v.id, nickname: v.nickname, avatar: v.avatar ?? "" })} `);
                setMentionOpen(false);
                setMentionKeyword("");
              }}
              onInputChange={(_, v) => setMentionKeyword(v)}
              renderInput={(p) => <DenseFilledTextField {...p} label={t("team.searchUser")} />}
            />
            <Typography variant="caption" color="text.secondary">
              {t("team.mentionHint")}
            </Typography>
          </Stack>
        </DialogContent>
      </DraggableDialog>
    </>
  );
};

export default TeamDocsView;
