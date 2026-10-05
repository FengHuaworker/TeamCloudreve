import { Box, Button, Container } from "@mui/material";
import { useQueryState } from "nuqs";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useNavigate, useParams } from "react-router-dom";
import { CSSTransition, SwitchTransition } from "react-transition-group";
import { getTeamBoard, TeamBoard as TeamBoardData } from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import ResponsiveTabs, { Tab } from "../Common/ResponsiveTabs.tsx";
import ArrowLeft from "../Icons/ArrowLeft.tsx";
import CommentMultiple from "../Icons/CommentMultiple.tsx";
import DeleteOutlined from "../Icons/DeleteOutlined.tsx";
import DocumentText from "../Icons/DocumentText.tsx";
import Grid from "../Icons/Grid.tsx";
import PageContainer from "../Pages/PageContainer.tsx";
import PageHeader, { PageTabQuery } from "../Pages/PageHeader.tsx";
import TeamBoardView from "./TeamBoard.tsx";
import TeamDiscussion from "./TeamDiscussion.tsx";
import TeamDocsView from "./TeamDocs.tsx";
import TeamTrash from "./TeamTrash.tsx";

/** 项目页的四个 tab。取值即 URL 的 ?tab=，便于直接分享到某个 tab。 */
export enum TeamProjectTab {
  Board = "board",
  Docs = "docs",
  Discussion = "discussion",
  Trash = "trash",
}

/**
 * 项目页外壳。
 *
 * 结构刻意对齐 Cloudreve 原生页面（如 管理面板 → 站点设置）：
 * PageContainer → Container → PageHeader → ResponsiveTabs → SwitchTransition/CSSTransition(fade)。
 * 四个 tab 只提供内容，页头与 tab 由这里统一持有，因此切换时页头不会闪烁、滚动位置稳定，
 * 并且获得了与站内其他页面完全一致的淡入淡出过渡。
 */
const TeamProject = () => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const navigate = useNavigate();
  const params = useParams();
  const projectId = Number(params.projectId);

  const [tab, setTab] = useQueryState(PageTabQuery);
  const [board, setBoard] = useState<TeamBoardData | undefined>();
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    if (!projectId) return;
    setLoading(true);
    dispatch(getTeamBoard(projectId))
      .then((res) => setBoard(res))
      .catch(() => setBoard(undefined))
      .finally(() => setLoading(false));
  }, [dispatch, projectId]);

  useEffect(() => {
    load();
  }, [load]);

  const activeTab = useMemo(() => {
    switch (tab) {
      case TeamProjectTab.Docs:
        return TeamProjectTab.Docs;
      case TeamProjectTab.Discussion:
        return TeamProjectTab.Discussion;
      case TeamProjectTab.Trash:
        return TeamProjectTab.Trash;
      default:
        return TeamProjectTab.Board;
    }
  }, [tab]);

  const tabs: Tab<TeamProjectTab>[] = useMemo(
    () => [
      { label: t("team.board"), value: TeamProjectTab.Board, icon: <Grid /> },
      { label: t("team.docs"), value: TeamProjectTab.Docs, icon: <DocumentText /> },
      { label: t("team.discussion"), value: TeamProjectTab.Discussion, icon: <CommentMultiple /> },
      { label: t("team.trash"), value: TeamProjectTab.Trash, icon: <DeleteOutlined /> },
    ],
    [t],
  );

  return (
    <PageContainer>
      <Container maxWidth={false}>
        <PageHeader
          title={board?.project.name ?? t("team.board")}
          loading={loading}
          onRefresh={load}
          secondaryAction={
            <Button variant="outlined" startIcon={<ArrowLeft />} onClick={() => navigate("/team")}>
              {t("team.backToProjects")}
            </Button>
          }
        />

        <ResponsiveTabs
          value={activeTab}
          onChange={(_e, v) => setTab(v)}
          tabs={tabs}
        />

        <SwitchTransition>
          <CSSTransition
            key={activeTab}
            addEndListener={(node, done) => node.addEventListener("transitionend", done, false)}
            classNames="fade"
          >
            <Box>
              {activeTab === TeamProjectTab.Board && (
                <TeamBoardView projectId={projectId} board={board} loading={loading} reload={load} />
              )}
              {activeTab === TeamProjectTab.Docs && <TeamDocsView projectId={projectId} />}
              {activeTab === TeamProjectTab.Discussion && <TeamDiscussion projectId={projectId} />}
              {activeTab === TeamProjectTab.Trash && <TeamTrash projectId={projectId} />}
            </Box>
          </CSSTransition>
        </SwitchTransition>
      </Container>
    </PageContainer>
  );
};

export default TeamProject;

/**
 * 兼容旧链接：文档曾经是可独立访问的路由（/team/:id/docs），
 * 现已并入项目页的 tab。这里把旧地址永久重定向到 ?tab=docs，
 * 避免历史书签落到 404。
 */
export const TeamDocsRedirect = () => {
  const params = useParams();
  return <Navigate to={`/team/${params.projectId}?tab=${TeamProjectTab.Docs}`} replace />;
};
