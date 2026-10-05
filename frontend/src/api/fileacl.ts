import { AppError, defaultOpts, send, ThunkResponse } from "./request.ts";

/**
 * 文件访问控制（ACL）API。
 *
 * 后端 service/fileacl（契约补遗二 §4）：
 *   GET    /file/permission?uri=…          查看（读门）
 *   PUT    /file/permission                设置/覆盖一条规则（管理门）
 *   DELETE /file/permission                删除一条规则（管理门）
 *
 * 权限口径（文件级）：
 *   个人文件 = 文件所有者或全局管理员；
 *   团队文件 = 项目 owner ∥ 角色 ∈ {owner, admin} ∥ 全局管理员（乙方案）。
 * 因此 403/404 在【查看】时是权限答案而非故障 —— 对话框渲染内联提示，
 * 不弹全局 toast（与团队回收站同口径）。
 */

export type AclSubjectType = "user" | "group";
export type AclPermission = "read" | "write";

export interface AclSubjectBrief {
  id: string;
  nickname: string;
}

export interface AclEntry {
  id: number;
  file_id: number;
  subject_type: AclSubjectType;
  subject_id: number;
  subject?: AclSubjectBrief;
  permission: AclPermission;
  inherit: boolean;
  created_by: number;
  created_at: string;
}

export interface AclEffective {
  permission: AclPermission;
  source_file_id: number;
  direct: boolean;
  inherited_from?: string;
}

export interface FileAclResponse {
  self: AclEntry[];
  effective: AclEffective[];
}

export interface SetAclService {
  uri: string;
  subject_type: AclSubjectType;
  subject_id: string;
  permission: AclPermission;
  inherit?: boolean;
}

export interface DeleteAclService {
  uri: string;
  subject_type: AclSubjectType;
  subject_id: string;
}

/** 查看文件 ACL；403/404 静默抛出，由对话框渲染"无权管理"。 */
export function getFileAcl(uri: string): ThunkResponse<FileAclResponse> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        "/file/permission?uri=" + encodeURIComponent(uri),
        { method: "GET" },
        {
          ...defaultOpts,
          bypassSnackbar: (e) => e instanceof AppError && (e.code === 403 || e.code === 404),
        },
      ),
    );
  };
}

/** 设置/覆盖一条 ACL（owner 锁死保护在后端，失败走默认 toast）。 */
export function sendSetFileAcl(req: SetAclService): ThunkResponse<FileAclResponse> {
  return async (dispatch, _getState) => {
    return await dispatch(send("/file/permission", { data: req, method: "PUT" }, { ...defaultOpts }));
  };
}

/** 删除一条 ACL（幂等）。 */
export function sendDeleteFileAcl(req: DeleteAclService): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send("/file/permission", { data: req, method: "DELETE" }, { ...defaultOpts }),
    );
  };
}
