import {
  Avatar,
  Box,
  Chip,
  Container,
  Divider,
  MenuItem,
  Pagination,
  Skeleton,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import {
  getTeamProjects,
  getTeamTopics,
  TeamProject,
  TeamTopic,
  TeamTopicCategory,
  TOPIC_CATEGORIES,
} from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import Nothing from "../Common/Nothing.tsx";
import { DenseFilledTextField } from "../Common/StyledComponents.tsx";
import TimeBadge from "../Common/TimeBadge.tsx";
import CheckmarkCircleFilled from "../Icons/CheckmarkCircleFilled.tsx";
import CommentMultiple from "../Icons/CommentMultiple.tsx";
import LockClosedOutlined from "../Icons/LockClosedOutlined.tsx";
import PinOutlined from "../Icons/PinOutlined.tsx";
import Search from "../Icons/Search.tsx";
import PageContainer from "../Pages/PageContainer.tsx";
import PageHeader from "../Pages/PageHeader.tsx";

const PAGE_SIZE = 20;

const categoryColor: Record<TeamTopicCategory, "default" | "primary" | "info" | "success" | "warning"> = {
  discuss: "default",
  announce: "warning",
  question: "info",
  share: "success",
};

/** 跨项目的话题 + 它所属项目（聚合视图需要额外记住来源） */
interface AggregatedTopic extends TeamTopic {
  project_name: string;
}

/**
 * 讨论区（一级入口）。
 *
 * 与项目页里的「讨论」tab 的区别：这里是**跨项目聚合**，
 * 回答「我参与的所有项目里，最近在讨论什么」，不需要先点进某个项目。
 *
 * 实现上逐项目调用既有的 `GET /team/project/:id/topic` 再合并，
 * 因此不依赖任何后端新增接口；项目数量在团队协作场景下通常是个位数，开销可接受。
 */
const TeamDiscussionAll = () => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const navigate = useNavigate();

  const [topics, setTopics] = useState<AggregatedTopic[]>([]);
  const [loading, setLoading] = useState(true);
  const [keyword, setKeyword] = useState("");
  const [category, setCategory] = useState<TeamTopicCategory | "all">("all");
  const [projectFilter, setProjectFilter] = useState<number | "all">("all");
  const [page, setPage] = useState(1);

  const load = useCallback(() => {
    setLoading(true);
    dispatch(getTeamProjects())
      .then(async (projects) => {
        const list: TeamProject[] = projects ?? [];
        if (list.length === 0) {
          setTopics([]);
          return;
        }
        // 后端单页上限 maxPageSize = 100。不能只拉第 1 页 ——
        // 某项目话题数超过 100 时会**静默只拿前 100 条**，聚合结果少了数据却不报错。
        // 这里循环拉到底（返回不足一页即停止），并用 total 兜底防死循环。
        const PAGE = 100;
        const results = await Promise.all(
          list.map(async (p) => {
            const acc: TeamTopic[] = [];
            for (let page = 1; page <= 50; page += 1) {
              const res = await dispatch(
                getTeamTopics(p.id, { page, page_size: PAGE }),
              ).catch(() => null);
              const batch = res?.topics ?? [];
              acc.push(...batch);
              const total = res?.total ?? acc.length;
              if (batch.length < PAGE || acc.length >= total) break;
            }
            return acc.map((tp) => ({ ...tp, project_name: p.name }));
          }),
        );
        const merged = results.flat();
        // 与服务端一致的口径：置顶优先，其次最后活动时间倒序
        merged.sort((a, b) => {
          if (a.is_pinned !== b.is_pinned) return a.is_pinned ? -1 : 1;
          const at = new Date(a.last_reply_at ?? a.created_at).getTime();
          const bt = new Date(b.last_reply_at ?? b.created_at).getTime();
          return bt - at;
        });
        setTopics(merged);
      })
      .catch(() => setTopics([]))
      .finally(() => setLoading(false));
  }, [dispatch]);

  useEffect(() => {
    load();
  }, [load]);

  const projectOptions = useMemo(() => {
    const map = new Map<number, string>();
    topics.forEach((tp) => map.set(tp.project_id, tp.project_name));
    return Array.from(map.entries());
  }, [topics]);

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    return topics.filter((tp) => {
      if (projectFilter !== "all" && tp.project_id !== projectFilter) return false;
      if (category !== "all" && tp.category !== category) return false;
      if (kw && !(tp.title.toLowerCase().includes(kw) || tp.content.toLowerCase().includes(kw))) return false;
      return true;
    });
  }, [topics, keyword, category, projectFilter]);

  const paged = useMemo(() => filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE), [filtered, page]);
  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));

  return (
    <PageContainer>
      <Container>
        <PageHeader title={t("team.discussion")} loading={loading} onRefresh={load} />

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
            value={projectFilter}
            onChange={(e) => {
              setProjectFilter(e.target.value as number | "all");
              setPage(1);
            }}
            sx={{ minWidth: 160 }}
          >
            <MenuItem value="all">{t("team.allProjects")}</MenuItem>
            {projectOptions.map(([id, name]) => (
              <MenuItem key={id} value={id}>
                {name}
              </MenuItem>
            ))}
          </DenseFilledTextField>
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
        </Stack>

        <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, overflow: "hidden" }}>
          {loading &&
            [...Array(5)].map((_, i) => (
              <Stack key={`sk-${i}`} direction="row" alignItems="center" spacing={2} sx={{ px: 2, py: 1.5 }}>
                <Skeleton variant="circular" width={36} height={36} />
                <Box sx={{ flexGrow: 1 }}>
                  <Skeleton variant="text" width="45%" />
                  <Skeleton variant="text" width="25%" />
                </Box>
                <Skeleton variant="text" width={80} />
              </Stack>
            ))}

          {!loading &&
            paged.map((tp, i) => (
              <Box key={`${tp.project_id}-${tp.id}`}>
                {i > 0 && <Divider />}
                <Box
                  onClick={() => navigate(`/team/${tp.project_id}?tab=discussion`)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      navigate(`/team/${tp.project_id}?tab=discussion`);
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
                  <Avatar src={tp.user?.avatar} sx={{ width: 36, height: 36, flexShrink: 0 }}>
                    {tp.user?.nickname?.[0]}
                  </Avatar>

                  <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                    <Stack direction="row" alignItems="center" spacing={0.75} sx={{ minWidth: 0 }}>
                      {tp.is_pinned && (
                        <Tooltip title={t("team.pinned")}>
                          <Box sx={{ display: "flex", color: "warning.main", flexShrink: 0 }}>
                            <PinOutlined fontSize="small" />
                          </Box>
                        </Tooltip>
                      )}
                      {tp.is_locked && (
                        <Tooltip title={t("team.locked")}>
                          <Box sx={{ display: "flex", color: "text.disabled", flexShrink: 0 }}>
                            <LockClosedOutlined fontSize="small" />
                          </Box>
                        </Tooltip>
                      )}
                      {tp.is_resolved && (
                        <Tooltip title={t("team.resolved")}>
                          <Box sx={{ display: "flex", color: "success.main", flexShrink: 0 }}>
                            <CheckmarkCircleFilled fontSize="small" />
                          </Box>
                        </Tooltip>
                      )}
                      <Typography variant="subtitle2" fontWeight={600} noWrap sx={{ minWidth: 0 }}>
                        {tp.title}
                      </Typography>
                    </Stack>

                    <Stack direction="row" alignItems="center" spacing={1} sx={{ mt: 0.25, minWidth: 0 }}>
                      <Chip
                        size="small"
                        variant="outlined"
                        color={categoryColor[tp.category] ?? "default"}
                        label={t(`team.category_${tp.category}`)}
                        sx={{ height: 18, fontSize: 11 }}
                      />
                      <Chip
                        size="small"
                        variant="outlined"
                        color="primary"
                        label={tp.project_name}
                        sx={{ height: 18, fontSize: 11 }}
                      />
                      <Typography variant="caption" color="text.secondary" noWrap>
                        {tp.user?.nickname}
                      </Typography>
                    </Stack>
                  </Box>

                  <Stack
                    direction="row"
                    alignItems="center"
                    spacing={2}
                    sx={{ flexShrink: 0, display: { xs: "none", sm: "flex" } }}
                  >
                    <Stack direction="row" alignItems="center" spacing={0.5} sx={{ color: "text.secondary" }}>
                      <CommentMultiple sx={{ fontSize: 16 }} />
                      <Typography variant="caption">{tp.reply_total}</Typography>
                    </Stack>
                    <Box sx={{ minWidth: 84, textAlign: "right" }}>
                      <TimeBadge variant="caption" datetime={tp.last_reply_at ?? tp.created_at} />
                    </Box>
                  </Stack>
                </Box>
              </Box>
            ))}

          {!loading && filtered.length === 0 && (
            <Box sx={{ py: 4 }}>
              <Nothing
                size={0.7}
                top={63}
                primary={keyword || category !== "all" || projectFilter !== "all" ? t("team.noTopicMatch") : t("team.noTopic")}
                secondary={
                  keyword || category !== "all" || projectFilter !== "all"
                    ? t("team.noTopicMatchDes")
                    : t("team.noTopicDes")
                }
              />
            </Box>
          )}
        </Box>

        {!loading && totalPages > 1 && (
          <Box sx={{ display: "flex", justifyContent: "center", mt: 2 }}>
            <Pagination page={page} count={totalPages} onChange={(_e, v) => setPage(v)} shape="rounded" />
          </Box>
        )}
      </Container>
    </PageContainer>
  );
};

export default TeamDiscussionAll;
