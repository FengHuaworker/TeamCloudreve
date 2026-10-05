import {
  Autocomplete,
  Avatar,
  Box,
  Button,
  Chip,
  DialogContent,
  Divider,
  IconButton,
  MenuItem,
  Pagination,
  Skeleton,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import dayjs from "dayjs";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { CSSTransition, SwitchTransition } from "react-transition-group";
import {
  buildMention,
  getTeamMembers,
  getTeamTopic,
  getTeamTopics,
  getTopicAttachments,
  sendAddTopicAttachment,
  sendCreateTopic,
  sendDeletePost,
  sendDeleteTopic,
  sendDeleteTopicAttachment,
  sendReplyTopic,
  sendTopicState,
  TeamMember,
  TeamTopic,
  TeamTopicCategory,
  TeamTopicDetail,
  TeamUserBrief,
  TOPIC_CATEGORIES,
  TopicAttachment,
} from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import { DenseFilledTextField } from "../Common/StyledComponents.tsx";
import Nothing from "../Common/Nothing.tsx";
import TimeBadge from "../Common/TimeBadge.tsx";
import DraggableDialog from "../Dialogs/DraggableDialog.tsx";
import Add from "../Icons/Add.tsx";
import ArrowLeft from "../Icons/ArrowLeft.tsx";
import CheckmarkCircleFilled from "../Icons/CheckmarkCircleFilled.tsx";
import CommentMultiple from "../Icons/CommentMultiple.tsx";
import DeleteOutlined from "../Icons/DeleteOutlined.tsx";
import Document from "../Icons/Document.tsx";
import LockClosedOutlined from "../Icons/LockClosedOutlined.tsx";
import MailOutlined from "../Icons/MailOutlined.tsx";
import PinOutlined from "../Icons/PinOutlined.tsx";
import Search from "../Icons/Search.tsx";
import { FilePicker } from "./TaskDetailDialog.tsx";

const PAGE_SIZE = 20;

/**
 * 分类的语义配色，与看板的优先级配色保持同一套语言：
 * 只用主题色，不引入硬编码色值。
 */
const categoryColor: Record<TeamTopicCategory, "default" | "primary" | "info" | "success" | "warning"> = {
  discuss: "default",
  announce: "warning",
  question: "info",
  share: "success",
};

/** 话题列表行：Discourse 式的信息密度 —— 一行里同时给出「谁、什么分类、多少回复、最后谁回的」 */
const TopicRow = ({ topic, onClick }: { topic: TeamTopic; onClick: () => void }) => {
  const { t } = useTranslation();

  return (
    <Box
      onClick={onClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }}
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 2,
        px: 2,
        py: 1.5,
        cursor: "pointer",
        transition: "background-color 250ms cubic-bezier(0.4, 0, 0.2, 1) 0ms",
        "&:hover": { bgcolor: "action.hover" },
      }}
    >
      <Avatar src={topic.user?.avatar} sx={{ width: 36, height: 36, flexShrink: 0 }}>
        {topic.user?.nickname?.[0]}
      </Avatar>

      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        <Stack direction="row" alignItems="center" spacing={0.75} sx={{ minWidth: 0 }}>
          {topic.is_pinned && (
            <Tooltip title={t("team.pinned")}>
              <Box sx={{ display: "flex", color: "warning.main", flexShrink: 0 }}>
                <PinOutlined fontSize="small" />
              </Box>
            </Tooltip>
          )}
          {topic.is_locked && (
            <Tooltip title={t("team.locked")}>
              <Box sx={{ display: "flex", color: "text.disabled", flexShrink: 0 }}>
                <LockClosedOutlined fontSize="small" />
              </Box>
            </Tooltip>
          )}
          {topic.is_resolved && (
            <Tooltip title={t("team.resolved")}>
              <Box sx={{ display: "flex", color: "success.main", flexShrink: 0 }}>
                <CheckmarkCircleFilled fontSize="small" />
              </Box>
            </Tooltip>
          )}
          <Typography variant="subtitle2" fontWeight={600} noWrap sx={{ minWidth: 0 }}>
            {topic.title}
          </Typography>
        </Stack>

        <Stack direction="row" alignItems="center" spacing={1} sx={{ mt: 0.25 }}>
          <Chip
            size="small"
            variant="outlined"
            color={categoryColor[topic.category] ?? "default"}
            label={t(`team.category_${topic.category}`)}
            sx={{ height: 18, fontSize: 11 }}
          />
          <Typography variant="caption" color="text.secondary" noWrap>
            {topic.user?.nickname}
          </Typography>
        </Stack>
      </Box>

      {/* 右侧统计：回复数 / 浏览数 / 最后活动 */}
      <Stack
        direction="row"
        alignItems="center"
        spacing={2}
        sx={{ flexShrink: 0, display: { xs: "none", sm: "flex" } }}
      >
        <Stack direction="row" alignItems="center" spacing={0.5} sx={{ color: "text.secondary" }}>
          <CommentMultiple sx={{ fontSize: 16 }} />
          <Typography variant="caption">{topic.reply_total}</Typography>
        </Stack>
        <Typography variant="caption" color="text.disabled" sx={{ minWidth: 48, textAlign: "right" }}>
          {t("team.viewCount", { count: topic.view_total })}
        </Typography>
        <Box sx={{ minWidth: 84, textAlign: "right" }}>
          <TimeBadge variant="caption" datetime={topic.last_reply_at ?? topic.created_at} />
        </Box>
      </Stack>
    </Box>
  );
};

const TopicRowSkeleton = () => (
  <Stack direction="row" alignItems="center" spacing={2} sx={{ px: 2, py: 1.5 }}>
    <Skeleton variant="circular" width={36} height={36} />
    <Box sx={{ flexGrow: 1 }}>
      <Skeleton variant="text" width="45%" />
      <Skeleton variant="text" width="25%" />
    </Box>
    <Skeleton variant="text" width={80} />
  </Stack>
);

interface TeamDiscussionProps {
  projectId: number;
  /** 话题数变化时通知外层（用于 tab 上的计数） */
  onCountChange?: (total: number) => void;
}

/**
 * 项目讨论区。
 *
 * 交互参考 Discourse / GitHub Discussions 的「列表 → 详情」两段式：
 * 列表页只承载「发现问题」，详情页承载「读完整上下文并回复」，
 * 两级之间用 Cloudreve 原生的 fade 过渡衔接，避免与站内其他页面手感割裂。
 */
const TeamDiscussion = ({ projectId, onCountChange }: TeamDiscussionProps) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();

  const [topics, setTopics] = useState<TeamTopic[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [keyword, setKeyword] = useState("");
  /**
   * 分类筛选。用 "all" 而不是空串做「全部分类」的取值：
   * MUI 的 Select 在值为空串且未配 renderValue/displayEmpty 时不会渲染任何文案，
   * 下拉框会变成一个空白框。用一个非空哨兵值即可让 MUI 正常显示对应 MenuItem 的文字。
   */
  const [category, setCategory] = useState<TeamTopicCategory | "all">("all");

  const [detail, setDetail] = useState<TeamTopicDetail | undefined>();
  const [openTopicId, setOpenTopicId] = useState<number | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailPage, setDetailPage] = useState(1);

  const [reply, setReply] = useState("");
  const [replying, setReplying] = useState(false);

  const [createOpen, setCreateOpen] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [newContent, setNewContent] = useState("");
  const [newCategory, setNewCategory] = useState<TeamTopicCategory>(TeamTopicCategory.discuss);
  const [submitting, setSubmitting] = useState(false);

  // ---- @提及（发帖与回复共用同一套）----
  const [members, setMembers] = useState<TeamMember[]>([]);
  const [mentionOpen, setMentionOpen] = useState(false);
  /** 提及插入到哪个输入框：发帖正文 / 回复框 */
  const [mentionTarget, setMentionTarget] = useState<"topic" | "reply">("reply");

  // ---- 附件 ----
  const [attachments, setAttachments] = useState<TopicAttachment[]>([]);
  const [attachOpen, setAttachOpen] = useState(false);

  useEffect(() => {
    dispatch(getTeamMembers(projectId))
      .then((res) => setMembers(res ?? []))
      .catch(() => setMembers([]));
  }, [dispatch, projectId]);

  const loadList = useCallback(
    (targetPage = page) => {
      if (!projectId) return;
      setLoading(true);
      dispatch(
        getTeamTopics(projectId, {
          keyword: keyword.trim() || undefined,
          category: category === "all" ? undefined : category,
          page: targetPage,
          page_size: PAGE_SIZE,
        }),
      )
        .then((res) => {
          setTopics(res?.topics ?? []);
          setTotal(res?.total ?? 0);
          onCountChange?.(res?.total ?? 0);
        })
        .catch(() => {
          setTopics([]);
          setTotal(0);
        })
        .finally(() => setLoading(false));
    },
    // onCountChange 故意不入依赖：它只是通知，变化不应触发重新拉取
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [dispatch, projectId, keyword, category, page],
  );

  useEffect(() => {
    loadList();
  }, [loadList]);

  const loadDetail = useCallback(
    (topicId: number, targetPage = 1) => {
      setDetailLoading(true);
      dispatch(getTeamTopic(topicId, targetPage, PAGE_SIZE))
        .then((res) => {
          setDetail(res);
          setDetailPage(targetPage);
        })
        .catch(() => setDetail(undefined))
        .finally(() => setDetailLoading(false));
    },
    [dispatch],
  );

  const openTopic = (topicId: number) => {
    setOpenTopicId(topicId);
    setReply("");
    loadDetail(topicId, 1);
    dispatch(getTopicAttachments(topicId))
      .then((res) => setAttachments(res ?? []))
      .catch(() => setAttachments([]));
  };

  /** 把 @提及 片段插到对应输入框末尾（与 Wiki 编辑器同一套 mention:hashid 格式） */
  const insertMention = (member: { id: string; nickname: string; avatar: string }) => {
    const snippet = buildMention({
      id: member.id,
      nickname: member.nickname,
      avatar: member.avatar ?? "",
    });
    if (mentionTarget === "topic") {
      setNewContent((prev) => `${prev}${prev && !prev.endsWith(" ") ? " " : ""}${snippet} `);
    } else {
      setReply((prev) => `${prev}${prev && !prev.endsWith(" ") ? " " : ""}${snippet} `);
    }
    setMentionOpen(false);
  };

  /** 上传附件：接口返回刷新后的全量列表，因此直接替换本地状态，无需再 GET */
  const onPickAttachment = (fileId: string) => {
    if (!openTopicId) return;
    dispatch(sendAddTopicAttachment(openTopicId, fileId)).then((res) => {
      setAttachments(res ?? []);
      setAttachOpen(false);
    });
  };

  const onRemoveAttachment = (id: number) => {
    dispatch(sendDeleteTopicAttachment(id)).then(() => {
      setAttachments((prev) => prev.filter((a) => a.id !== id));
    });
  };

  const backToList = () => {
    setOpenTopicId(null);
    setDetail(undefined);
    loadList();
  };

  const onReply = () => {
    if (!openTopicId || !reply.trim()) return;
    setReplying(true);
    dispatch(sendReplyTopic(openTopicId, reply.trim()))
      .then((res) => {
        setReply("");
        // 接口默认回第 1 页，但新楼层落在最后一页；直接跳到最后一页，
        // 否则多页话题里用户回复完会看不到自己刚发的那层。
        const lastPage = Math.max(1, Math.ceil((res?.total ?? 1) / PAGE_SIZE));
        loadDetail(openTopicId, lastPage);
      })
      .finally(() => setReplying(false));
  };

  const onCreate = () => {
    if (!newTitle.trim()) return;
    setSubmitting(true);
    dispatch(
      sendCreateTopic({
        project_id: projectId,
        title: newTitle.trim(),
        content: newContent,
        category: newCategory,
      }),
    )
      .then((topic) => {
        setCreateOpen(false);
        setNewTitle("");
        setNewContent("");
        setNewCategory(TeamTopicCategory.discuss);
        setKeyword("");
        setCategory("");
        setPage(1);
        loadList(1);
        if (topic?.id) openTopic(topic.id);
      })
      .finally(() => setSubmitting(false));
  };

  const onToggleState = (patch: {
    is_pinned?: boolean;
    is_locked?: boolean;
    is_resolved?: boolean;
  }) => {
    if (!openTopicId) return;
    dispatch(sendTopicState(openTopicId, patch)).then(() => loadDetail(openTopicId, detailPage));
  };

  const onDeleteTopic = () => {
    if (!openTopicId || !window.confirm(t("team.deleteTopicConfirm"))) return;
    dispatch(sendDeleteTopic(openTopicId)).then(() => backToList());
  };

  const onDeletePost = (postId: number) => {
    if (!openTopicId || !window.confirm(t("team.deletePostConfirm"))) return;
    dispatch(sendDeletePost(postId)).then(() => loadDetail(openTopicId, detailPage));
  };

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const topic = detail?.topic;
  const posts = detail?.posts ?? [];
  // 首帖由话题本身承载，楼层列表只渲染回复，因此从第 2 楼开始
  const firstFloor = (detailPage - 1) * PAGE_SIZE + 2;

  return (
    <SwitchTransition>
      <CSSTransition
        key={openTopicId === null ? "list" : `topic-${openTopicId}`}
        addEndListener={(node, done) => node.addEventListener("transitionend", done, false)}
        classNames="fade"
      >
        <Box>
          {openTopicId === null ? (
            /* ------------------------------ 列表 ------------------------------ */
            <>
              <Stack direction="row" spacing={1} sx={{ mb: 2 }}>
                <DenseFilledTextField
                  fullWidth
                  size="small"
                  value={keyword}
                  onChange={(e) => {
                    setKeyword(e.target.value);
                    setPage(1);
                  }}
                  placeholder={t("team.searchTopic")}
                  slotProps={{
                    input: {
                      startAdornment: (
                        <Box sx={{ display: "flex", mr: 1, color: "action.active" }}>
                          <Search fontSize="small" />
                        </Box>
                      ),
                    },
                  }}
                />
                <DenseFilledTextField
                  select
                  size="small"
                  value={category}
                  onChange={(e) => {
                    setCategory(e.target.value as TeamTopicCategory | "all");
                    setPage(1);
                  }}
                  sx={{ minWidth: 140 }}
                >
                  <MenuItem value="all">{t("team.allCategories")}</MenuItem>
                  {TOPIC_CATEGORIES.map((c) => (
                    <MenuItem key={c} value={c}>
                      {t(`team.category_${c}`)}
                    </MenuItem>
                  ))}
                </DenseFilledTextField>
                <Button
                  variant="contained"
                  startIcon={<Add />}
                  sx={{ flexShrink: 0 }}
                  onClick={() => setCreateOpen(true)}
                >
                  {t("team.newTopic")}
                </Button>
              </Stack>

              <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, overflow: "hidden" }}>
                {loading &&
                  [...Array(4)].map((_, i) => (
                    <Box key={`sk-${i}`}>
                      {i > 0 && <Divider />}
                      <TopicRowSkeleton />
                    </Box>
                  ))}

                {!loading &&
                  topics.map((item, i) => (
                    <Box key={item.id}>
                      {i > 0 && <Divider />}
                      <TopicRow topic={item} onClick={() => openTopic(item.id)} />
                    </Box>
                  ))}

                {!loading && topics.length === 0 && (
                  <Box sx={{ py: 4 }}>
                    <Nothing
                      size={0.7}
                      top={63}
                      primary={
                        keyword || category !== "all" ? t("team.noTopicMatch") : t("team.noTopic")
                      }
                      secondary={
                        keyword || category !== "all"
                          ? t("team.noTopicMatchDes")
                          : t("team.noTopicDes")
                      }
                    />
                  </Box>
                )}
              </Box>

              {!loading && totalPages > 1 && (
                <Box sx={{ display: "flex", justifyContent: "center", mt: 2 }}>
                  <Pagination
                    page={page}
                    count={totalPages}
                    onChange={(_e, v) => setPage(v)}
                    shape="rounded"
                  />
                </Box>
              )}
            </>
          ) : (
            /* ------------------------------ 详情 ------------------------------ */
            <>
              <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 2 }}>
                <IconButton onClick={backToList} size="small">
                  <ArrowLeft />
                </IconButton>
                <Typography variant="h6" sx={{ flexGrow: 1, minWidth: 0 }} noWrap>
                  {topic?.title ?? <Skeleton variant="text" width={220} />}
                </Typography>

                {topic && (
                  <>
                    <Tooltip title={topic.is_pinned ? t("team.unpin") : t("team.pin")}>
                      <IconButton
                        size="small"
                        color={topic.is_pinned ? "warning" : "default"}
                        onClick={() => onToggleState({ is_pinned: !topic.is_pinned })}
                      >
                        <PinOutlined />
                      </IconButton>
                    </Tooltip>
                    <Tooltip title={topic.is_resolved ? t("team.markUnresolved") : t("team.markResolved")}>
                      <IconButton
                        size="small"
                        color={topic.is_resolved ? "success" : "default"}
                        onClick={() => onToggleState({ is_resolved: !topic.is_resolved })}
                      >
                        <CheckmarkCircleFilled />
                      </IconButton>
                    </Tooltip>
                    <Tooltip title={topic.is_locked ? t("team.unlock") : t("team.lock")}>
                      <IconButton
                        size="small"
                        onClick={() => onToggleState({ is_locked: !topic.is_locked })}
                      >
                        <LockClosedOutlined />
                      </IconButton>
                    </Tooltip>
                    <Tooltip title={t("team.deleteTopic")}>
                      <IconButton size="small" color="error" onClick={onDeleteTopic}>
                        <DeleteOutlined />
                      </IconButton>
                    </Tooltip>
                  </>
                )}
              </Stack>

              {detailLoading && !topic ? (
                <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
                  <Skeleton variant="text" width="40%" />
                  <Skeleton variant="text" width="90%" />
                  <Skeleton variant="text" width="70%" />
                </Box>
              ) : (
                topic && (
                  <Stack spacing={2}>
                    {/* 首帖 */}
                    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
                      <Stack direction="row" spacing={1.5} sx={{ mb: 1.5 }}>
                        <Avatar src={topic.user?.avatar} sx={{ width: 36, height: 36 }}>
                          {topic.user?.nickname?.[0]}
                        </Avatar>
                        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                          <Stack direction="row" alignItems="center" spacing={1}>
                            <Typography variant="body2" fontWeight={600}>
                              {topic.user?.nickname}
                            </Typography>
                            <Chip
                              size="small"
                              variant="outlined"
                              color={categoryColor[topic.category] ?? "default"}
                              label={t(`team.category_${topic.category}`)}
                              sx={{ height: 18, fontSize: 11 }}
                            />
                            <Typography variant="caption" color="text.secondary">
                              {t("team.floorLabel", { floor: 1 })}
                            </Typography>
                          </Stack>
                          <Typography variant="caption" color="text.secondary">
                            {dayjs(topic.created_at).format("YYYY-MM-DD HH:mm")}
                          </Typography>
                        </Box>
                      </Stack>
                      <Typography variant="body2" sx={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
                        {topic.content}
                      </Typography>
                    </Box>

                    {/* 楼层 */}
                    {posts.map((post, i) => (
                      <Box
                        key={post.id}
                        sx={{
                          border: 1,
                          borderColor: "divider",
                          borderRadius: 1,
                          p: 2,
                          transition: "border-color 250ms cubic-bezier(0.4, 0, 0.2, 1) 0ms",
                          "&:hover": { borderColor: "action.active" },
                        }}
                      >
                        <Stack direction="row" spacing={1.5}>
                          <Avatar src={post.user?.avatar} sx={{ width: 36, height: 36 }}>
                            {post.user?.nickname?.[0]}
                          </Avatar>
                          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                            <Stack direction="row" alignItems="center" spacing={1}>
                              <Typography variant="body2" fontWeight={600}>
                                {post.user?.nickname}
                              </Typography>
                              <Typography variant="caption" color="text.secondary">
                                {t("team.floorLabel", { floor: firstFloor + i })}
                              </Typography>
                              <Box sx={{ flexGrow: 1 }} />
                              <Typography variant="caption" color="text.secondary">
                                {dayjs(post.created_at).format("YYYY-MM-DD HH:mm")}
                              </Typography>
                              <Tooltip title={t("team.deletePost")}>
                                <IconButton size="small" onClick={() => onDeletePost(post.id)}>
                                  <DeleteOutlined sx={{ fontSize: 16 }} />
                                </IconButton>
                              </Tooltip>
                            </Stack>
                            <Typography
                              variant="body2"
                              sx={{ mt: 0.5, whiteSpace: "pre-wrap", wordBreak: "break-word" }}
                            >
                              {post.content}
                            </Typography>
                          </Box>
                        </Stack>
                      </Box>
                    ))}

                    {posts.length === 0 && (
                      <Typography variant="body2" color="text.secondary" sx={{ px: 1 }}>
                        {t("team.noReplyYet")}
                      </Typography>
                    )}

                    {detail && detail.total > PAGE_SIZE && (
                      <Box sx={{ display: "flex", justifyContent: "center" }}>
                        <Pagination
                          page={detailPage}
                          count={Math.max(1, Math.ceil(detail.total / PAGE_SIZE))}
                          onChange={(_e, v) => loadDetail(openTopicId, v)}
                          shape="rounded"
                        />
                      </Box>
                    )}

                    {/* 附件：只引用 Cloudreve 文件，不复制 */}
                    <Box>
                      <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
                        <Typography variant="subtitle2" fontWeight={600} sx={{ flexGrow: 1 }}>
                          {t("team.attachments")}
                          {attachments.length > 0 ? ` (${attachments.length})` : ""}
                        </Typography>
                        <Button size="small" variant="outlined" startIcon={<Document />} onClick={() => setAttachOpen(true)}>
                          {t("team.attachFile")}
                        </Button>
                      </Stack>
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
                                {a.user?.nickname}
                              </Typography>
                            </Box>
                            <Tooltip title={t("team.deletePost")}>
                              <IconButton size="small" onClick={() => onRemoveAttachment(a.id)}>
                                <DeleteOutlined sx={{ fontSize: 16 }} />
                              </IconButton>
                            </Tooltip>
                          </Stack>
                        ))}
                        {attachments.length === 0 && (
                          <Typography variant="body2" color="text.secondary">
                            {t("team.noAttachment")}
                          </Typography>
                        )}
                      </Stack>
                    </Box>

                    <Divider />
                    {topic.is_locked ? (
                      <Typography variant="body2" color="text.secondary">
                        {t("team.replyLocked")}
                      </Typography>
                    ) : (
                      <Stack spacing={1}>
                        <DenseFilledTextField
                          fullWidth
                          multiline
                          rows={3}
                          placeholder={t("team.writeReply")}
                          value={reply}
                          onChange={(e) => setReply(e.target.value)}
                          onKeyDown={(e) => {
                            // Ctrl/Cmd + Enter 快速发送，与 Cloudreve 编辑器的快捷键习惯一致
                            if ((e.ctrlKey || e.metaKey) && e.key === "Enter") onReply();
                          }}
                        />
                        <Box sx={{ display: "flex", justifyContent: "flex-end", gap: 1 }}>
                          <Button
                            variant="outlined"
                            startIcon={<MailOutlined />}
                            onClick={() => {
                              setMentionTarget("reply");
                              setMentionOpen(true);
                            }}
                          >
                            {t("team.mention")}
                          </Button>
                          <Button
                            variant="contained"
                            disabled={!reply.trim() || replying}
                            onClick={onReply}
                          >
                            {t("team.reply")}
                          </Button>
                        </Box>
                      </Stack>
                    )}
                  </Stack>
                )
              )}
            </>
          )}

          {/* @提及：写入 @[名](mention:hashid)，与 Wiki 编辑器共用同一套格式与后端解析 */}
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
                  options={members.map((m) => m.user).filter(Boolean) as TeamUserBrief[]}
                  getOptionLabel={(o) => o?.nickname ?? ""}
                  value={null}
                  blurOnSelect
                  clearOnBlur
                  onChange={(_, v) => v && insertMention(v)}
                  renderInput={(p) => <DenseFilledTextField {...p} label={t("team.searchUser")} />}
                />
                <Typography variant="caption" color="text.secondary">
                  {t("team.mentionHint")}
                </Typography>
              </Stack>
            </DialogContent>
          </DraggableDialog>

          {/* 选择附件文件（复用任务弹窗的文件选择器） */}
          <DraggableDialog
            title={t("team.attachFile")}
            showActions
            showCancel
            hideOk
            dialogProps={{
              open: attachOpen,
              onClose: () => setAttachOpen(false),
              fullWidth: true,
              maxWidth: "sm",
            }}
          >
            <DialogContent>
              <FilePicker onPick={(f) => onPickAttachment(f.id)} />
            </DialogContent>
          </DraggableDialog>

          {/* 新建话题 */}
          <DraggableDialog
            title={t("team.newTopic")}
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
                  label={t("team.topicTitle")}
                  value={newTitle}
                  onChange={(e) => setNewTitle(e.target.value)}
                />
                <DenseFilledTextField
                  select
                  fullWidth
                  label={t("team.topicCategory")}
                  value={newCategory}
                  onChange={(e) => setNewCategory(e.target.value as TeamTopicCategory)}
                >
                  {TOPIC_CATEGORIES.map((c) => (
                    <MenuItem key={c} value={c}>
                      {t(`team.category_${c}`)}
                    </MenuItem>
                  ))}
                </DenseFilledTextField>
                <DenseFilledTextField
                  fullWidth
                  multiline
                  rows={5}
                  label={t("team.topicContent")}
                  value={newContent}
                  onChange={(e) => setNewContent(e.target.value)}
                />
              </Stack>
            </DialogContent>
          </DraggableDialog>
        </Box>
      </CSSTransition>
    </SwitchTransition>
  );
};

export default TeamDiscussion;
