import {
  Box,
  Button,
  Container,
  DialogContent,
  Grid,
  LinearProgress,
  Skeleton,
  Stack,
  Typography,
} from "@mui/material";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { getTeamProjects, sendCreateProject, TeamProject } from "../../api/team.ts";
import { useAppDispatch } from "../../redux/hooks.ts";
import Nothing from "../Common/Nothing.tsx";
import { DenseFilledTextField } from "../Common/StyledComponents.tsx";
import TimeBadge from "../Common/TimeBadge.tsx";
import DraggableDialog from "../Dialogs/DraggableDialog.tsx";
import Add from "../Icons/Add.tsx";
import People from "../Icons/People.tsx";
import PageContainer from "../Pages/PageContainer.tsx";
import PageHeader from "../Pages/PageHeader.tsx";
import { SummaryButton } from "../Pages/Tasks/TaskCard.tsx";

/**
 * 项目卡片。
 *
 * 刻意复用 Cloudreve 自己的 SummaryButton（自带
 * `transition: all 250ms cubic-bezier(0.4, 0, 0.2, 1)` 与 hover 变色），
 * 保证交互手感与「我的分享」「后台任务」等页面完全一致。
 */
const TeamProjectCard = ({ project, loading }: { project?: TeamProject; loading?: boolean }) => {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const percent =
    project && project.task_total > 0 ? Math.round((project.done_total / project.task_total) * 100) : 0;

  return (
    <Grid item xs={12} sm={6} md={4}>
      <SummaryButton
        expanded={false}
        percentage={percent}
        sx={{ p: 0, minHeight: 0, width: "100%", textAlign: "left", display: "block" }}
        onClick={() => project && navigate(`/team/${project.id}`)}
        disabled={loading}
      >
        <Box sx={{ p: 2, width: "100%" }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Typography variant="subtitle1" fontWeight={600} noWrap sx={{ flexGrow: 1, minWidth: 0 }}>
              {loading ? <Skeleton variant="text" width={140} /> : project?.name}
            </Typography>
            {!loading && project?.status === "archived" && (
              <Typography variant="caption" color="text.secondary">
                {t("team.archived")}
              </Typography>
            )}
          </Stack>

          <Typography
            variant="body2"
            color="text.secondary"
            sx={{
              mt: 0.5,
              minHeight: 40,
              display: "-webkit-box",
              WebkitLineClamp: 2,
              WebkitBoxOrient: "vertical",
              overflow: "hidden",
            }}
          >
            {loading ? (
              <>
                <Skeleton variant="text" width="90%" />
                <Skeleton variant="text" width="60%" />
              </>
            ) : (
              project?.description || t("team.noDescription")
            )}
          </Typography>

          <Box sx={{ mt: 1.5 }}>
            {loading ? (
              <Skeleton variant="text" width="40%" />
            ) : (
              <LinearProgress variant="determinate" value={percent} sx={{ height: 6, borderRadius: 3 }} />
            )}
          </Box>

          <Stack direction="row" alignItems="center" spacing={1} sx={{ mt: 1.5 }}>
            <People fontSize="small" sx={{ color: "text.disabled" }} />
            <Typography variant="caption" color="text.secondary">
              {loading ? <Skeleton variant="text" width={60} /> : `${project?.member_total ?? 0}`}
            </Typography>
            <Box sx={{ flexGrow: 1 }} />
            {loading ? (
              <Skeleton variant="text" width={70} />
            ) : project ? (
              <TimeBadge variant="caption" datetime={project.updated_at} />
            ) : null}
          </Stack>
        </Box>
      </SummaryButton>
    </Grid>
  );
};

const TeamProjects = () => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();

  const [projects, setProjects] = useState<TeamProject[]>([]);
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newDesc, setNewDesc] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    dispatch(getTeamProjects())
      .then((res) => setProjects(res ?? []))
      .catch(() => setProjects([]))
      .finally(() => setLoading(false));
  }, [dispatch]);

  useEffect(() => {
    load();
  }, [load]);

  const onCreate = () => {
    if (!newName.trim()) return;
    setSubmitting(true);
    dispatch(sendCreateProject({ name: newName.trim(), description: newDesc }))
      .then(() => {
        setCreateOpen(false);
        setNewName("");
        setNewDesc("");
        load();
      })
      .finally(() => setSubmitting(false));
  };

  return (
    <PageContainer>
      <Container>
        <PageHeader
          title={t("team.projects")}
          loading={loading}
          onRefresh={load}
          secondaryAction={
            <Button variant="contained" startIcon={<Add />} onClick={() => setCreateOpen(true)}>
              {t("team.newProject")}
            </Button>
          }
        />

        <Grid container spacing={1}>
          {projects.map((p) => (
            <TeamProjectCard key={p.id} project={p} />
          ))}
          {loading && [...Array(3)].map((_, i) => <TeamProjectCard key={`sk-${i}`} loading />)}
        </Grid>

        {!loading && projects.length === 0 && (
          <Box sx={{ p: 1, width: "100%", textAlign: "center" }}>
            <Nothing size={0.8} top={63} primary={t("team.noProject")} secondary={t("team.noProjectDes")} />
          </Box>
        )}
      </Container>

      <DraggableDialog
        title={t("team.newProject")}
        showActions
        showCancel
        loading={submitting}
        disabled={!newName.trim()}
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
              label={t("team.projectName")}
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
            />
            <DenseFilledTextField
              fullWidth
              multiline
              rows={3}
              label={t("team.projectDescription")}
              value={newDesc}
              onChange={(e) => setNewDesc(e.target.value)}
            />
          </Stack>
        </DialogContent>
      </DraggableDialog>
    </PageContainer>
  );
};

export default TeamProjects;
