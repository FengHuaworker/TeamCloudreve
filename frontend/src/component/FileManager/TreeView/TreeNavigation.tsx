import { Box, Collapse, Fade } from "@mui/material";

import { ChevronRight, ExpandMore } from "@mui/icons-material";
import { TreeView } from "@mui/x-tree-view";
import React, { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { TransitionGroup } from "react-transition-group";
import { defaultPath, defaultSharedWithMePath, defaultTrashPath } from "../../../hooks/useNavigation.tsx";
import { getTeamProjects } from "../../../api/team.ts";
import { useAppDispatch, useAppSelector } from "../../../redux/hooks.ts";
import SessionManager, { UserSettings } from "../../../session";
import CrUri, { Filesystem } from "../../../util/uri.ts";
import { FileManagerIndex } from "../FileManager.tsx";
import { FmIndexContext } from "../FmIndexContext.tsx";
import Pinned, { usePinned } from "./Pinned.tsx";
import TreeFiles from "./TreeFiles.tsx";

export interface TreeNavigationProps {
  scrollRef?: React.MutableRefObject<HTMLElement | undefined>;
  index?: number;
  hideWithDrawer?: boolean;
  disableSharedWithMe?: boolean;
  disableTrash?: boolean;
}

const TreeNavigation = React.memo(
  ({ index = 0, scrollRef, hideWithDrawer, disableSharedWithMe, disableTrash }: TreeNavigationProps) => {
    const dispatch = useAppDispatch();
    const { t } = useTranslation();
    /**
     * 是否显示「团队空间」入口。
     *
     * 前端这里没有「列出可用文件系统」的接口，所以入口是写死的；
     * 可见性靠既有的 `GET /team/project` 判断 —— **不参与任何项目的用户不显示这一项**，
     * 而不是显示一个空目录。这样不需要后端新增枚举接口。
     */
    const [hasTeamSpace, setHasTeamSpace] = React.useState(false);
    useEffect(() => {
      dispatch(getTeamProjects())
        .then((res) => setHasTeamSpace((res ?? []).length > 0))
        .catch(() => setHasTeamSpace(false));
    }, [dispatch]);

    const base = useAppSelector((s) => s.fileManager[index].path_root);
    const path = useAppSelector((s) => s.fileManager[index].pure_path_with_category);
    const currentFs = useAppSelector((s) => s.fileManager[index].current_fs);
    const elements = useAppSelector((s) => s.fileManager[index].path_elements);
    const drawerOpen = useAppSelector((s) => s.globalState.drawerOpen);
    const [expanded, setExpanded] = React.useState<string[]>([]);

    useEffect(() => {
      const res: string[] = [];
      if (path) {
        const p = new CrUri(path);
        if (p.is_search()) {
          return;
        }
      }
      if (base && elements) {
        const b = new CrUri(base);
        res.push(base);
        elements.forEach((element) => {
          b.join(element);
          res.push(b.toString());
        });
      }
      if (SessionManager.getWithFallback(UserSettings.TreeViewAutoExpand)) {
        setExpanded((e) => [...new Set([...e, ...res])]);
      }
    }, [path, base, elements]);

    const pinned = usePinned();
    const alreadyPinned = base && pinned && pinned.find((p) => p.uri == base);
    const showShareTree = base && currentFs && currentFs == Filesystem.share && !alreadyPinned;

    useEffect(() => {
      if (showShareTree && scrollRef && scrollRef.current) {
        scrollRef.current?.scrollTo({ top: 0, behavior: "smooth" });
      }
    }, [showShareTree]);

    const isLogin = !!SessionManager.currentLoginOrNull();

    return (
      <FmIndexContext.Provider value={index}>
        <Box sx={{ width: "100%" }}>
          <Fade in={drawerOpen || !hideWithDrawer} unmountOnExit>
            <TreeView
              selected={path ?? ""}
              defaultCollapseIcon={<ExpandMore />}
              defaultExpandIcon={<ChevronRight />}
              expanded={expanded}
              onNodeToggle={(_event, nodeIds: string[]) => {
                setExpanded(nodeIds);
              }}
            >
              <TransitionGroup>
                {showShareTree && (
                  <Collapse key={"share"}>
                    <TreeFiles level={0} path={base} elements={elements} />
                  </Collapse>
                )}
                {isLogin && (
                  <>
                    <TreeFiles
                      canDrop
                      level={0}
                      path={defaultPath}
                      key={defaultPath}
                      elements={currentFs == Filesystem.my ? elements : undefined}
                    />
                    {index == FileManagerIndex.main && (
                      <>
                        <TreeFiles
                          flatten
                          level={0}
                          path={"cloudreve://my/?category=image"}
                          key={"cloudreve://my/?category=image"}
                        />
                        <TreeFiles
                          flatten
                          level={0}
                          path={"cloudreve://my/?category=video"}
                          key={"cloudreve://my/?category=video"}
                        />
                        <TreeFiles
                          flatten
                          level={0}
                          path={"cloudreve://my/?category=audio"}
                          key={"cloudreve://my/?category=audio"}
                        />
                        <TreeFiles
                          flatten
                          level={0}
                          path={"cloudreve://my/?category=document"}
                          key={"cloudreve://my/?category=document"}
                        />
                      </>
                    )}
                    {!disableSharedWithMe && (
                      <TreeFiles level={0} path={defaultSharedWithMePath} key={defaultSharedWithMePath} />
                    )}
                    {hasTeamSpace && (
                      <TreeFiles
                        level={0}
                        path={"cloudreve://team"}
                        key={"cloudreve://team"}
                        labelOverwrite={t("navbar.teamSpace")}
                      />
                    )}
                    {!disableTrash && (
                      <TreeFiles
                        level={0}
                        flatten
                        canDrop
                        key={defaultTrashPath}
                        path={defaultTrashPath}
                        elements={currentFs == Filesystem.trash ? elements : undefined}
                      />
                    )}
                    <Pinned />
                  </>
                )}
              </TransitionGroup>
            </TreeView>
          </Fade>
        </Box>
      </FmIndexContext.Provider>
    );
  },
);

export default TreeNavigation;
