import {
  Avatar,
  Box,
  Checkbox,
  Chip,
  Container,
  Divider,
  Grid,
  Link,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { useQueryState } from "nuqs";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link as RouterLink } from "react-router-dom";
import { CSSTransition, SwitchTransition } from "react-transition-group";
import { getAdminTeamMembers, getAdminTeamPermission, getAdminTeamProjects, getAdminTeamStats, sendAdminTeamPermission, AdminTeamMember, AdminTeamProject, AdminTeamStats, TEAM_CAPABILITIES, TEAM_CONFIGURABLE_ROLES, TeamCapability, TeamConfigurableRole, TeamRoleMatrix } from "../../../api/team.ts";
import { useAppDispatch } from "../../../redux/hooks.ts";
import ResponsiveTabs, { Tab } from "../../Common/ResponsiveTabs.tsx";
import { DenseFilledTextField, NoWrapTableCell, SecondaryButton, StyledTableContainerPaper } from "../../Common/StyledComponents.tsx";
import Nothing from "../../Common/Nothing.tsx";
import TimeBadge from "../../Common/TimeBadge.tsx";
import CommentMultiple from "../../Icons/CommentMultiple.tsx";
import DataHistogram from "../../Icons/DataHistogram.tsx";
import GridIcon from "../../Icons/Grid.tsx";
import People from "../../Icons/People.tsx";
import Person from "../../Icons/Person.tsx";
import Search from "../../Icons/Search.tsx";
import PageContainer from "../../Pages/PageContainer.tsx";
import PageHeader, { PageTabQuery } from "../../Pages/PageHeader.tsx";
import TeamMembersDialog from "../../Team/TeamMembersDialog.tsx";
import TeamStatsDialog from "../../Team/TeamStatsDialog.tsx";
import TablePagination from "../Common/TablePagination.tsx";

enum TeamAdminTab {
  Project = "project",
  Member = "member",
  Permission = "permission",
}

const PAGE_SIZE = 10;

/** 概览数字卡片：与 Cloudreve 首页的统计块保持同一种克制的排版 */
const StatCard = ({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: number | string;
}) => (
  <StyledTableContainerPaper sx={{ p: 2, display: "flex", alignItems: "center", gap: 2 }}>
    <Box sx={{ display: "flex", color: "action.active" }}>{icon}</Box>
    <Box sx={{ minWidth: 0 }}>
      <Typography variant="h6" fontWeight={600} noWrap>
        {value}
      </Typography>
      <Typography variant="caption" color="text.secondary" noWrap>
        {label}
      </Typography>
    </Box>
  </StyledTableContainerPaper>
);

/** 项目 tab：全站项目总览，并把「成员管理 / 分工统计」这两个管理动作收拢到这里 */
const ProjectTab = () => {
  const { t } = useTranslation("dashboard");
  const dispatch = useAppDispatch();
  const [rows, setRows] = useState<AdminTeamProject[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [keyword, setKeyword] = useState("");
  const [loading, setLoading] = useState(true);
  const [membersFor, setMembersFor] = useState<AdminTeamProject | null>(null);
  const [statsFor, setStatsFor] = useState<AdminTeamProject | null>(null);

  const load = useCallback(
    (targetPage = page, size = pageSize) => {
      setLoading(true);
      dispatch(getAdminTeamProjects(keyword.trim(), targetPage, size))
        .then((res) => {
          setRows(res?.projects ?? []);
          setTotal(res?.total ?? 0);
        })
        .catch(() => {
          setRows([]);
          setTotal(0);
        })
        .finally(() => setLoading(false));
    },
    [dispatch, keyword, page, pageSize],
  );

  useEffect(() => {
    load();
  }, [load]);

  return (
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
          placeholder={t("team.searchProject")}
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
      </Stack>

      <TableContainer component={StyledTableContainerPaper}>
        <Table size="small" stickyHeader sx={{ width: "100%", tableLayout: "fixed" }}>
          <TableHead>
            <TableRow>
              <NoWrapTableCell width={240}>{t("team.project")}</NoWrapTableCell>
              <NoWrapTableCell width={180}>{t("team.owner")}</NoWrapTableCell>
              <NoWrapTableCell width={90}>{t("team.memberCount")}</NoWrapTableCell>
              <NoWrapTableCell width={140}>{t("team.taskProgress")}</NoWrapTableCell>
              <NoWrapTableCell width={90}>{t("team.topicCount")}</NoWrapTableCell>
              <NoWrapTableCell width={140}>{t("team.updatedAt")}</NoWrapTableCell>
              <NoWrapTableCell width={180} align="right" />
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((p) => (
              <TableRow key={p.id} hover>
                <NoWrapTableCell>
                  <Link component={RouterLink} to={`/team/${p.id}`} underline="hover">
                    {p.name}
                  </Link>
                  {p.status === "archived" && (
                    <Chip size="small" variant="outlined" label={t("team.archived")} sx={{ ml: 1, height: 18 }} />
                  )}
                </NoWrapTableCell>
                <NoWrapTableCell>
                  <Stack direction="row" alignItems="center" spacing={1}>
                    <Avatar src={p.owner?.avatar} sx={{ width: 20, height: 20, fontSize: 11 }}>
                      {p.owner?.nickname?.[0]}
                    </Avatar>
                    <Typography variant="body2" noWrap>
                      {p.owner?.nickname ?? "-"}
                    </Typography>
                  </Stack>
                </NoWrapTableCell>
                <NoWrapTableCell>{p.member_total}</NoWrapTableCell>
                <NoWrapTableCell>
                  {p.done_total} / {p.task_total}
                </NoWrapTableCell>
                <NoWrapTableCell>{p.topic_total}</NoWrapTableCell>
                <NoWrapTableCell>
                  <TimeBadge variant="caption" datetime={p.updated_at} />
                </NoWrapTableCell>
                <NoWrapTableCell align="right">
                  <Stack direction="row" spacing={1} justifyContent="flex-end">
                    <SecondaryButton size="small" onClick={() => setMembersFor(p)}>
                      {t("team.manageMembers")}
                    </SecondaryButton>
                    <SecondaryButton size="small" onClick={() => setStatsFor(p)}>
                      {t("team.viewStats")}
                    </SecondaryButton>
                  </Stack>
                </NoWrapTableCell>
              </TableRow>
            ))}
            {!loading && rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={7}>
                  <Box sx={{ py: 4 }}>
                    <Nothing size={0.7} top={63} primary={t("team.noProject")} secondary={t("team.noProjectDes")} />
                  </Box>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>

      <TablePagination
        page={page}
        totalItems={total}
        rowsPerPage={pageSize}
        onRowsPerPageChange={(size) => {
          setPageSize(size);
          setPage(1);
        }}
        onChange={(_e, v) => {
          setPage(v);
          load(v, pageSize);
        }}
      />

      {membersFor && (
        <TeamMembersDialog
          projectId={membersFor.id}
          onClose={() => setMembersFor(null)}
          onChanged={() => load()}
        />
      )}
      {statsFor && <TeamStatsDialog projectId={statsFor.id} onClose={() => setStatsFor(null)} />}
    </>
  );
};

/** 成员 tab：跨项目的成员总表，用于回答「某个人在哪些项目里、什么角色」 */
const MemberTab = () => {
  const { t } = useTranslation("dashboard");
  const dispatch = useAppDispatch();
  const [rows, setRows] = useState<AdminTeamMember[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [keyword, setKeyword] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(
    (targetPage = page, size = pageSize) => {
      setLoading(true);
      dispatch(getAdminTeamMembers(keyword.trim(), targetPage, size))
        .then((res) => {
          setRows(res?.members ?? []);
          setTotal(res?.total ?? 0);
        })
        .catch(() => {
          setRows([]);
          setTotal(0);
        })
        .finally(() => setLoading(false));
    },
    [dispatch, keyword, page, pageSize],
  );

  useEffect(() => {
    load();
  }, [load]);

  return (
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
          placeholder={t("team.searchMember")}
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
      </Stack>

      <TableContainer component={StyledTableContainerPaper}>
        <Table size="small" stickyHeader sx={{ width: "100%", tableLayout: "fixed" }}>
          <TableHead>
            <TableRow>
              <NoWrapTableCell width={240}>{t("team.user")}</NoWrapTableCell>
              <NoWrapTableCell width={120}>{t("team.role")}</NoWrapTableCell>
              <NoWrapTableCell>{t("team.project")}</NoWrapTableCell>
              <NoWrapTableCell width={160}>{t("team.joinedAt")}</NoWrapTableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((m, i) => (
              <TableRow key={`${m.project_id}-${m.user?.id ?? i}`} hover>
                <NoWrapTableCell>
                  <Stack direction="row" alignItems="center" spacing={1}>
                    <Avatar src={m.user?.avatar} sx={{ width: 24, height: 24, fontSize: 12 }}>
                      {m.user?.nickname?.[0]}
                    </Avatar>
                    <Box sx={{ minWidth: 0 }}>
                      <Typography variant="body2" noWrap>
                        {m.user?.nickname ?? t("team.unknownUser")}
                      </Typography>
                      <Typography variant="caption" color="text.secondary" noWrap>
                        {m.user?.email}
                      </Typography>
                    </Box>
                  </Stack>
                </NoWrapTableCell>
                <NoWrapTableCell>
                  <Chip
                    size="small"
                    variant={m.role === "owner" ? "filled" : "outlined"}
                    color={m.role === "owner" ? "primary" : "default"}
                    label={t(`team.role_${m.role}`)}
                  />
                </NoWrapTableCell>
                <NoWrapTableCell>
                  <Link component={RouterLink} to={`/team/${m.project_id}`} underline="hover">
                    {m.project_name}
                  </Link>
                </NoWrapTableCell>
                <NoWrapTableCell>
                  <TimeBadge variant="caption" datetime={m.joined_at} />
                </NoWrapTableCell>
              </TableRow>
            ))}
            {!loading && rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={4}>
                  <Box sx={{ py: 4 }}>
                    <Nothing size={0.7} top={63} primary={t("team.noMember")} />
                  </Box>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>

      <TablePagination
        page={page}
        totalItems={total}
        rowsPerPage={pageSize}
        onRowsPerPageChange={(size) => {
          setPageSize(size);
          setPage(1);
        }}
        onChange={(_e, v) => {
          setPage(v);
          load(v, pageSize);
        }}
      />
    </>
  );
};

/**
 * 权限 tab：团队角色能力矩阵。
 *
 * 与 Cloudreve 的用户组合并：全局管理员不在此配置（天然全权），
 * 这里配置的是「项目内角色」的能力；owner 恒为全权，因此不出现在表里。
 */
const PermissionTab = () => {
  const { t } = useTranslation("dashboard");
  const dispatch = useAppDispatch();
  const [matrix, setMatrix] = useState<TeamRoleMatrix | null>(null);
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    dispatch(getAdminTeamPermission())
      .then((res) => setMatrix(res?.matrix ?? null))
      .catch(() => setMatrix(null))
      .finally(() => setLoading(false));
  }, [dispatch]);

  const toggle = (role: TeamConfigurableRole, cap: TeamCapability) => {
    setSaved(false);
    setMatrix((prev) => {
      if (!prev) return prev;
      return { ...prev, [role]: { ...prev[role], [cap]: !prev[role][cap] } };
    });
  };

  const onSave = () => {
    if (!matrix) return;
    setSaving(true);
    dispatch(sendAdminTeamPermission(matrix))
      .then(() => setSaved(true))
      .finally(() => setSaving(false));
  };

  return (
    <>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t("team.permissionHint")}
      </Typography>

      {loading && !matrix ? (
        <Box sx={{ py: 4 }}>
          <Nothing size={0.7} top={63} primary={t("team.loading")} />
        </Box>
      ) : (
        matrix && (
          <>
            <TableContainer component={StyledTableContainerPaper}>
              <Table size="small" sx={{ width: "100%" }}>
                <TableHead>
                  <TableRow>
                    <NoWrapTableCell>{t("team.capability")}</NoWrapTableCell>
                    {TEAM_CONFIGURABLE_ROLES.map((r) => (
                      <NoWrapTableCell key={r} width={140} align="center">
                        {t(`team.role_${r}`)}
                      </NoWrapTableCell>
                    ))}
                  </TableRow>
                </TableHead>
                <TableBody>
                  {TEAM_CAPABILITIES.map((cap) => (
                    <TableRow key={cap} hover>
                      <NoWrapTableCell>
                        <Typography variant="body2">{t(`team.cap_${cap}`)}</Typography>
                      </NoWrapTableCell>
                      {TEAM_CONFIGURABLE_ROLES.map((role) => (
                        <TableCell key={role} align="center" padding="checkbox">
                          <Checkbox
                            size="small"
                            checked={!!matrix[role]?.[cap]}
                            onChange={() => toggle(role, cap)}
                          />
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>

            <Stack direction="row" alignItems="center" spacing={2} sx={{ mt: 2 }}>
              <Typography variant="caption" color="text.secondary" sx={{ flexGrow: 1 }}>
                {t("team.ownerFixedHint")}
              </Typography>
              {saved && (
                <Typography variant="caption" color="success.main">
                  {t("team.permissionSaved")}
                </Typography>
              )}
              <SecondaryButton variant="contained" disabled={saving} onClick={onSave}>
                {t("team.save")}
              </SecondaryButton>
            </Stack>
          </>
        )
      )}
    </>
  );
};

/** 概览统计条 */
const StatsBar = () => {
  const { t } = useTranslation("dashboard");
  const dispatch = useAppDispatch();
  const [stats, setStats] = useState<AdminTeamStats | undefined>();

  useEffect(() => {
    dispatch(getAdminTeamStats())
      .then((res) => setStats(res))
      .catch(() => setStats(undefined));
  }, [dispatch]);

  return (
    <Grid container spacing={1} sx={{ mb: 2 }}>
      <Grid item xs={6} md={2}>
        <StatCard icon={<GridIcon />} label={t("team.statProjects")} value={stats?.project_total ?? "-"} />
      </Grid>
      <Grid item xs={6} md={2}>
        <StatCard icon={<DataHistogram />} label={t("team.statTasks")} value={stats?.task_total ?? "-"} />
      </Grid>
      <Grid item xs={6} md={2}>
        <StatCard icon={<People />} label={t("team.statMembers")} value={stats?.member_total ?? "-"} />
      </Grid>
      <Grid item xs={6} md={2}>
        <StatCard icon={<CommentMultiple />} label={t("team.statTopics")} value={stats?.topic_total ?? "-"} />
      </Grid>
      <Grid item xs={6} md={2}>
        <StatCard icon={<Person />} label={t("team.statDone")} value={stats?.done_total ?? "-"} />
      </Grid>
    </Grid>
  );
};

/**
 * 管理面板 → 团队管理。
 *
 * 把原先散落在项目页的「成员管理 / 分工统计」统一收拢到管理面板，
 * 项目页因此只保留 看板 / 文档 / 讨论 三件与日常工作直接相关的事。
 */
const TeamSetting = () => {
  const { t } = useTranslation("dashboard");
  const [tab, setTab] = useQueryState(PageTabQuery);
  const [refreshKey, setRefreshKey] = useState(0);

  const activeTab = useMemo(() => {
    switch (tab) {
      case TeamAdminTab.Member:
        return TeamAdminTab.Member;
      case TeamAdminTab.Permission:
        return TeamAdminTab.Permission;
      default:
        return TeamAdminTab.Project;
    }
  }, [tab]);

  const tabs: Tab<TeamAdminTab>[] = useMemo(
    () => [
      { label: t("team.projectTab"), value: TeamAdminTab.Project, icon: <GridIcon /> },
      { label: t("team.memberTab"), value: TeamAdminTab.Member, icon: <People /> },
      { label: t("team.permissionTab"), value: TeamAdminTab.Permission, icon: <Person /> },
    ],
    [t],
  );

  return (
    <PageContainer>
      <Container maxWidth="xl">
        <PageHeader title={t("nav.team")} onRefresh={() => setRefreshKey((k) => k + 1)} />

        <StatsBar />

        <ResponsiveTabs value={activeTab} onChange={(_e, v) => setTab(v)} tabs={tabs} />

        <SwitchTransition>
          <CSSTransition
            key={activeTab}
            addEndListener={(node, done) => node.addEventListener("transitionend", done, false)}
            classNames="fade"
          >
            <Box>
              {activeTab === TeamAdminTab.Project && <ProjectTab key={refreshKey} />}
              {activeTab === TeamAdminTab.Member && <MemberTab key={refreshKey} />}
              {activeTab === TeamAdminTab.Permission && <PermissionTab />}
            </Box>
          </CSSTransition>
        </SwitchTransition>

        <Divider sx={{ mt: 4 }} />
        <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 1 }}>
          {t("team.adminHint")}
        </Typography>
      </Container>
    </PageContainer>
  );
};

export default TeamSetting;
