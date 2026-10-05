import { useMemo } from "react";
import { FileResponse, FileType, Metadata, NavigatorCapability } from "../../../api/explorer.ts";
import { GroupPermission } from "../../../api/user.ts";
import { defaultPath } from "../../../hooks/useNavigation.tsx";
import { ContextMenuTypes } from "../../../redux/fileManagerSlice.ts";
import { Viewers, ViewersByID } from "../../../redux/siteConfigSlice.ts";
import { ExpandedViewerSetting } from "../../../redux/thunks/viewer.ts";
import SessionManager from "../../../session";
import { fileExtension } from "../../../util";
import Boolset from "../../../util/boolset.ts";
import CrUri, { Filesystem, mayWrite } from "../../../util/uri.ts";
import { FileManagerIndex } from "../FileManager.tsx";

const supportedArchiveTypes = ["zip", "gz", "xz", "tar", "rar", "7z", "bz2"];

export const canManageVersion = (file: FileResponse, bs: Boolset) => {
  return (
    file.type == FileType.file &&
    (!file.metadata || !file.metadata[Metadata.share_redirect]) &&
    bs.enabled(NavigatorCapability.version_control)
  );
};

export const canShowInfo = (cap: Boolset) => {
  return cap.enabled(NavigatorCapability.info);
};

export const canUpdate = (opt: DisplayOption) => {
  return !!(
    opt.allUpdatable &&
    opt.hasFile &&
    opt.orCapability?.enabled(NavigatorCapability.upload_file) &&
    opt.allUpdatable
  );
};

export interface DisplayOption {
  allReadable: boolean;
  allUpdatable: boolean;

  hasReadable?: boolean;
  hasUpdatable?: boolean;

  hasTrashFile?: boolean;
  hasFile?: boolean;
  hasFolder?: boolean;
  hasOwned?: boolean;
  hasFailedThumb?: boolean;

  showEnter?: boolean;
  showOpen?: boolean;
  showOpenWithCascading?: () => boolean;
  showOpenWith?: () => boolean;
  showDownload?: boolean;
  showGoToSharedLink?: boolean;
  showExtractArchive?: boolean;
  showTorrentRemoteDownload?: boolean;
  showGoToParent?: boolean;

  showDelete?: boolean;
  showRestore?: boolean;
  showRename?: boolean;
  showPin?: boolean;
  showOrganize?: boolean;
  showCopy?: boolean;
  showShare?: boolean;
  showFileAcl?: boolean;
  showInfo?: boolean;
  showDirectLink?: boolean;

  showMove?: boolean;
  showTags?: boolean;
  showChangeFolderColor?: boolean;
  showChangeIcon?: boolean;
  showCustomProps?: boolean;

  showMore?: boolean;
  showVersionControl?: boolean;
  showDirectLinkManagement?: boolean;
  showManageShares?: boolean;
  showCreateArchive?: boolean;
  showResetThumb?: boolean;

  andCapability?: Boolset;
  orCapability?: Boolset;

  showCreateFolder?: boolean;
  showCreateFile?: boolean;
  showRefresh?: boolean;
  showNewFileFromTemplate?: boolean;
  showUpload?: boolean;
  showRemoteDownload?: boolean;
}

const capabilityMap: { [key: string]: Boolset } = {};

export const getActionOpt = (
  targets: FileResponse[],
  viewerSetting?: ExpandedViewerSetting,
  type?: string,
  parent?: FileResponse,
  fmIndex: number = 0,
): DisplayOption => {
  const currentUser = SessionManager.currentLoginOrNull();
  const currentUserAnonymous = SessionManager.currentUser();
  const groupBs = SessionManager.currentUserGroupPermission();
  const display: DisplayOption = {
    allReadable: true,
    allUpdatable: true,
  };
  if (type == ContextMenuTypes.empty || type == ContextMenuTypes.new) {
    display.showRefresh = type == ContextMenuTypes.empty;
    display.showRemoteDownload = groupBs.enabled(GroupPermission.remote_download) && !!currentUser;

    if (!parent || parent.type != FileType.folder) {
      display.showRemoteDownload = display.showRemoteDownload && type == ContextMenuTypes.new;
      return display;
    }

    const parentCap = new Boolset(parent.capability);
    display.showCreateFolder = parentCap.enabled(NavigatorCapability.create_file) && mayWrite(parent.owned, parent.path || defaultPath);
    display.showCreateFile = display.showCreateFolder && fmIndex == FileManagerIndex.main;
    display.showUpload = display.showCreateFile;
    if (display.showCreateFile) {
      const allViewers = Object.entries(ViewersByID);
      for (let i = 0; i < allViewers.length; i++) {
        if (allViewers[i][1] && allViewers[i][1].templates) {
          display.showNewFileFromTemplate = true;
          break;
        }
      }
    }

    return display;
  }

  if (type == ContextMenuTypes.searchResult) {
    display.showGoToParent = true;
  }

  const parentUrl = new CrUri(targets?.[0]?.path ?? defaultPath);
  targets.forEach((target) => {
    let readable = true;
    let updatable = mayWrite(target.owned, target.path || parentUrl) && parentUrl.fs() != Filesystem.share;

    if (display.allReadable && !readable) {
      display.allReadable = false;
    }
    if (display.allUpdatable && !updatable) {
      display.allUpdatable = false;
    }

    if (!display.hasReadable && readable) {
      display.hasReadable = true;
    }
    if (!display.hasUpdatable && updatable) {
      display.hasUpdatable = true;
    }

    if (target.metadata) {
      if (target.metadata[Metadata.restore_uri]) {
        display.hasTrashFile = true;
      }
      if (target.metadata[Metadata.thumbDisabled] !== undefined) {
        display.hasFailedThumb = true;
      }
    }

    if (target.type == FileType.file) {
      display.hasFile = true;
    }

    if (target.type == FileType.folder) {
      display.hasFolder = true;
    }

    // ⚠️ 这里**刻意不用 mayWrite**。`hasOwned` 唯一的下游是 showDirectLink（:238），
    // 而直链是「把文件发布成一个公开下载 URL」—— 那是**发布权**，不是**写权**。
    // 团队文件不归你，团队空间也没说过要对外发布；把 mayWrite 用在这里等于
    // 顺手把「对外发布」也开了，属于**没有边界的开关**（正是我不让后端改 owned 的理由）。
    // 要开放也是产品决定，应由能力位显式表达，不能靠一个可写判定夹带。
    if (target.owned) {
      display.hasOwned = true;
    }

    if (target.capability) {
      let bs = capabilityMap[target.capability];
      if (!bs) {
        bs = new Boolset(target.capability);
        capabilityMap[target.capability] = bs;
      }

      if (!display.andCapability) {
        display.andCapability = bs;
      }

      display.andCapability = display.andCapability.and(bs);

      if (!display.orCapability) {
        display.orCapability = bs;
      }
      display.orCapability = display.orCapability.or(bs);
    }
  });

  const firstFileSuffix = fileExtension(targets[0]?.name ?? "");
  display.showPin = !display.hasTrashFile && targets.length == 1 && display.hasFolder;
  display.showDelete =
    display.hasUpdatable &&
    display.orCapability &&
    (display.orCapability.enabled(NavigatorCapability.soft_delete) ||
      display.orCapability.enabled(NavigatorCapability.delete_file));
  display.showRestore = display.andCapability?.enabled(NavigatorCapability.restore);
  display.showRename =
    targets.length == 1 &&
    display.allUpdatable &&
    display.orCapability &&
    display.orCapability.enabled(NavigatorCapability.rename_file);
  display.showCopy = display.hasUpdatable && !!display.orCapability;
  display.showShare =
    targets.length == 1 &&
    !!currentUser &&
    groupBs.enabled(GroupPermission.share) &&
    display.allUpdatable &&
    (targets[0].owned || groupBs.enabled(GroupPermission.is_admin)) &&
    display.orCapability &&
    display.orCapability.enabled(NavigatorCapability.share) &&
    (!targets[0].metadata ||
      (!targets[0].metadata[Metadata.share_redirect] && !targets[0].metadata[Metadata.restore_uri]));
  display.showMove = display.hasUpdatable && !!display.orCapability;
  // 文件访问控制（ACL）入口：
  //  个人文件 -> 自己的文件（mayWrite 的 owned 分支）；
  //  团队文件 -> 所有成员都出入口（mayWrite 的 collective 分支），
  //              服务端按角色门决定 403/404，对话框内联呈现。
  //  这样"按钮在、点了才拒"的割裂被对话框的内联态消化，而不是点了才弹错。
  //  排除回收站/分享重定向文件（与 showShare 同款排除）。
  display.showFileAcl =
    targets.length == 1 &&
    !!currentUser &&
    display.allUpdatable &&
    !display.hasTrashFile &&
    mayWrite(targets[0].owned, targets[0].path || defaultPath) &&
    (!targets[0].metadata ||
      (!targets[0].metadata[Metadata.share_redirect] &&
        !targets[0].metadata[Metadata.restore_uri]));
  display.showTags =
    display.hasUpdatable && display.orCapability && display.orCapability.enabled(NavigatorCapability.update_metadata);
  display.showChangeFolderColor =
    display.hasUpdatable &&
    !display.hasFile &&
    display.orCapability &&
    display.orCapability.enabled(NavigatorCapability.update_metadata);
  display.showChangeIcon =
    display.hasUpdatable && display.orCapability && display.orCapability.enabled(NavigatorCapability.update_metadata);
  display.showCustomProps = display.showChangeIcon;
  display.showDownload =
    display.hasReadable && display.orCapability && display.orCapability.enabled(NavigatorCapability.download_file);
  display.showDirectLink =
    (display.hasOwned || groupBs.enabled(GroupPermission.is_admin)) &&
    display.orCapability &&
    (currentUserAnonymous?.group?.direct_link_batch_size ?? 0) >= targets.length &&
    display.orCapability.enabled(NavigatorCapability.download_file);
  display.showDirectLinkManagement = display.showDirectLink && targets.length == 1 && display.hasFile;
  display.showOpen =
    targets.length == 1 &&
    display.hasFile &&
    display.showDownload &&
    !!viewerSetting &&
    !!firstFileSuffix &&
    !!viewerSetting?.[firstFileSuffix];
  display.showEnter =
    targets.length == 1 &&
    display.hasFolder &&
    display.orCapability?.enabled(NavigatorCapability.enter_folder) &&
    display.allReadable;
  display.showExtractArchive =
    targets.length == 1 &&
    display.hasFile &&
    display.showDownload &&
    !!currentUser &&
    groupBs.enabled(GroupPermission.archive_task) &&
    supportedArchiveTypes.includes(firstFileSuffix ?? "");
  display.showTorrentRemoteDownload =
    targets.length == 1 &&
    display.hasFile &&
    display.showDownload &&
    !!currentUser &&
    groupBs.enabled(GroupPermission.remote_download) &&
    firstFileSuffix == "torrent";

  display.showOpenWithCascading = () => false;
  display.showOpenWith = () => targets.length == 1 && !!display.hasFile && !!display.showDownload;
  if (display.showOpen) {
    display.showOpenWithCascading = () =>
      !!(display.showOpen && viewerSetting && viewerSetting[firstFileSuffix ?? ""]?.length >= 1);
    display.showOpenWith = () =>
      !!(display.showOpen && viewerSetting && viewerSetting[firstFileSuffix ?? ""]?.length < 1);
  }
  display.showOrganize = display.showPin || display.showMove || display.showChangeFolderColor || display.showChangeIcon;
  display.showGoToSharedLink =
    targets.length == 1 && display.hasFile && targets[0].metadata && !!targets[0].metadata[Metadata.share_redirect];
  display.showInfo = targets.length == 1 && display.orCapability && canShowInfo(display.orCapability);
  display.showVersionControl =
    targets.length == 1 &&
    display.orCapability &&
    display.hasReadable &&
    canManageVersion(targets[0], display.orCapability);
  display.showManageShares =
    targets.length == 1 &&
    targets[0].shared &&
    display.orCapability &&
    !!currentUser &&
    groupBs.enabled(GroupPermission.share) &&
    display.orCapability.enabled(NavigatorCapability.share);
  display.showCreateArchive =
    display.hasReadable &&
    !!currentUser &&
    groupBs.enabled(GroupPermission.archive_task) &&
    display.orCapability &&
    display.orCapability.enabled(NavigatorCapability.download_file);
  display.showResetThumb =
    display.hasFile &&
    !display.hasFolder &&
    display.hasFailedThumb &&
    display.allUpdatable &&
    display.orCapability &&
    display.orCapability.enabled(NavigatorCapability.update_metadata);

  display.showMore =
    display.showVersionControl ||
    display.showManageShares ||
    display.showCreateArchive ||
    display.showDirectLinkManagement ||
    display.showResetThumb;
  return display;
};

const useActionDisplayOpt = (targets: FileResponse[], type?: string, parent?: FileResponse, fmIndex: number = 0) => {
  const opt = useMemo(() => {
    return getActionOpt(targets, Viewers, type, parent, fmIndex);
  }, [targets, type, parent, fmIndex]);

  return opt;
};

export default useActionDisplayOpt;
