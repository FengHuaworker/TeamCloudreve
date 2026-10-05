import { AppError, defaultOpts, send, ThunkResponse } from "./request.ts";

/**
 * 团队协作模块 API。
 *
 * 约定（与 Cloudreve 保持一致）：
 *  - 认证复用 Cloudreve 的会话，不另建登录体系；
 *  - 团队内部实体（项目/任务/附件）使用普通整型 ID；
 *  - 用户与文件 ID 使用 Cloudreve 的 hashid。
 */

// ============================ 类型 ============================

export interface TeamUserBrief {
  id: string;
  nickname: string;
  avatar: string;
  email?: string;
}

export interface TeamProject {
  id: number;
  name: string;
  description: string;
  status: string;
  owner?: TeamUserBrief;
  my_role?: string;
  task_total: number;
  done_total: number;
  member_total: number;
  created_at: string;
  updated_at: string;
}

export interface TeamTask {
  id: number;
  project_id: number;
  parent_id?: number;
  title: string;
  description: string;
  status: TeamTaskStatus;
  priority: TeamPriority;
  progress: number;
  sort_order: number;
  tags: string[];
  assignee?: TeamUserBrief;
  creator?: TeamUserBrief;
  collaborators?: TeamUserBrief[];
  start_at?: string;
  due_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  comment_total: number;
  attachment_total: number;
  sub_task_total: number;
  overdue: boolean;
}

export const TeamTaskStatus = {
  todo: "todo",
  doing: "doing",
  review: "review",
  done: "done",
  archived: "archived",
} as const;
export type TeamTaskStatus = (typeof TeamTaskStatus)[keyof typeof TeamTaskStatus];

/** 看板列顺序 */
export const BOARD_STATUSES: TeamTaskStatus[] = [
  TeamTaskStatus.todo,
  TeamTaskStatus.doing,
  TeamTaskStatus.review,
  TeamTaskStatus.done,
];

export const TeamPriority = {
  low: "low",
  normal: "normal",
  high: "high",
  urgent: "urgent",
} as const;
export type TeamPriority = (typeof TeamPriority)[keyof typeof TeamPriority];

export interface TeamBoardColumn {
  status: TeamTaskStatus;
  tasks: TeamTask[];
}

export interface TeamBoard {
  project: TeamProject;
  columns: TeamBoardColumn[];
  members: TeamUserBrief[];
}

export interface TeamComment {
  id: number;
  task_id: number;
  user?: TeamUserBrief;
  content: string;
  created_at: string;
  updated_at: string;
}

export interface TeamAttachment {
  id: number;
  task_id: number;
  file_id: string;
  name: string;
  size: number;
  user?: TeamUserBrief;
  created_at: string;
}

export interface TeamMember {
  user: TeamUserBrief;
  role: string;
  joined_at: string;
}

export interface TeamActivity {
  id: number;
  task_id?: number;
  user?: TeamUserBrief;
  action: string;
  payload?: Record<string, any>;
  created_at: string;
}

export interface TeamAssigneeStat {
  user: TeamUserBrief;
  total: number;
  done: number;
  doing: number;
  todo: number;
  overdue: number;
  done_rate: number;
}

export interface TeamStats {
  total: number;
  done: number;
  doing: number;
  todo: number;
  overdue: number;
  by_status: Record<string, number>;
  by_assignee: TeamAssigneeStat[];
}

// ============================ 请求体 ============================

export interface CreateProjectService {
  name: string;
  description?: string;
}

export interface UpdateProjectService {
  name?: string;
  description?: string;
  status?: string;
}

export interface CreateTaskService {
  project_id: number;
  parent_id?: number;
  title: string;
  description?: string;
  status?: TeamTaskStatus;
  priority?: TeamPriority;
  /**
   * 负责人，Cloudreve **hashid**（与响应里 `assignee.id` 同一口径）。
   *
   * 这里曾经声明成 number，于是调用方写了 `Number(user.id)` ——
   * 而 `user.id` 是 "pBiM" 这类 hashid，`Number("pBiM")` 得到 NaN，
   * 序列化后变成 null，后端收不到人，导致**任务指派从来没有生效过**。
   * 现在统一按 hashid 字符串传递；省略或空串表示不指派。
   */
  assignee_id?: string;
  due_at?: string;
  tags?: string[];
}

export interface UpdateTaskService {
  title?: string;
  description?: string;
  status?: TeamTaskStatus;
  priority?: TeamPriority;
  progress?: number;
  /** 负责人 hashid；空串表示取消指派 */
  assignee_id?: string;
  due_at?: string;
  tags?: string[];
}

export interface MoveTaskService {
  status: TeamTaskStatus;
  sort_order: number;
}

export interface AddMemberService {
  user_id: string;
  role?: string;
}

// ---- 项目级团队回收站 ----

/**
 * 回收站里的一条记录。
 *
 * 权限口径（与后端一致）：可见 = 可恢复 = 能删。
 * 所以列表接口返回 403/404 时，组件直接把整个入口渲染成"无权访问"，
 * 而不是弹全局报错 —— 那两个码在这里是【权限答案】，不是故障。
 */
export interface TeamTrashFile {
  hash_id: string;
  project_id: number;
  project_name: string;
  /** 原路径（sys:restore_uri 原值，含 team 项目段） */
  original_path: string;
  original_name: string;
  trashed_at: string;
  size: number;
  is_folder: boolean;
}

export interface TeamTrashList {
  files: TeamTrashFile[];
  total: number;
}

export interface RestoreTrashService {
  file_hash_ids: string[];
}

// ============================ API ============================

/** 列出当前用户可见的项目（全局管理员可见全部） */
export function getTeamProjects(): ThunkResponse<TeamProject[]> {
  return async (dispatch, _getState) => {
    return await dispatch(send("/team/project", { method: "GET" }, { ...defaultOpts }));
  };
}

export function sendCreateProject(req: CreateProjectService): ThunkResponse<TeamProject> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send("/team/project", { data: req, method: "PUT" }, { ...defaultOpts }),
    );
  };
}

/** 获取看板数据（项目 + 分列任务 + 成员） */
export function getTeamBoard(projectId: number): ThunkResponse<TeamBoard> {
  return async (dispatch, _getState) => {
    return await dispatch(send(`/team/project/${projectId}`, { method: "GET" }, { ...defaultOpts }));
  };
}

export function sendUpdateProject(projectId: number, req: UpdateProjectService): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/project/${projectId}`, { data: req, method: "POST" }, { ...defaultOpts }),
    );
  };
}

export function sendDeleteProject(projectId: number): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/project/${projectId}`, { method: "DELETE" }, { ...defaultOpts }),
    );
  };
}

/**
 * 列出项目回收站。
 *
 * 403（对项目无写权限）/ 404（非成员或项目不存在）静默抛出：
 * 这两个码是权限口径的一部分（与"能删"同口径），组件据此渲染
 * "无权访问"并隐藏一切恢复入口；只有真正的故障才弹全局 toast。
 */
export function getTeamTrash(projectId: number): ThunkResponse<TeamTrashList> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/project/${projectId}/trash`,
        { method: "GET" },
        {
          ...defaultOpts,
          bypassSnackbar: (e) => e instanceof AppError && (e.code === 403 || e.code === 404),
        },
      ),
    );
  };
}

/**
 * 恢复回收站中的文件（批量，全有或全无）。
 *
 * 失败走默认 toast（这里是用户动作的反馈；入口本身已由列表的
 * 403/404 决定是否展示，走不到"点了才发现没权限"）。
 */
export function sendRestoreTeamTrash(
  projectId: number,
  req: RestoreTrashService,
): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/project/${projectId}/trash/restore`,
        { data: req, method: "PUT" },
        { ...defaultOpts },
      ),
    );
  };
}

export function getTeamMembers(projectId: number): ThunkResponse<TeamMember[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/project/${projectId}/member`, { method: "GET" }, { ...defaultOpts }),
    );
  };
}

export function sendAddMember(projectId: number, req: AddMemberService): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/project/${projectId}/member`, { data: req, method: "PUT" }, { ...defaultOpts }),
    );
  };
}

export function sendRemoveMember(projectId: number, userId: string): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/project/${projectId}/member`,
        { params: { user_id: userId }, method: "DELETE" },
        { ...defaultOpts },
      ),
    );
  };
}

export function getTeamActivities(projectId: number, limit = 50): ThunkResponse<TeamActivity[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/project/${projectId}/activity`,
        { params: { limit }, method: "GET" },
        { ...defaultOpts },
      ),
    );
  };
}

export function getTeamStats(projectId: number): ThunkResponse<TeamStats> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/project/${projectId}/stats`, { method: "GET" }, { ...defaultOpts }),
    );
  };
}

export function sendCreateTask(req: CreateTaskService): ThunkResponse<TeamTask> {
  return async (dispatch, _getState) => {
    return await dispatch(send("/team/task", { data: req, method: "PUT" }, { ...defaultOpts }));
  };
}

export function getTeamTask(taskId: number): ThunkResponse<TeamTask> {
  return async (dispatch, _getState) => {
    return await dispatch(send(`/team/task/${taskId}`, { method: "GET" }, { ...defaultOpts }));
  };
}

export function sendUpdateTask(taskId: number, req: UpdateTaskService): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/task/${taskId}`, { data: req, method: "POST" }, { ...defaultOpts }),
    );
  };
}

export function sendDeleteTask(taskId: number): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(send(`/team/task/${taskId}`, { method: "DELETE" }, { ...defaultOpts }));
  };
}

/** 看板拖拽：仅改状态与排序，是最轻量的写操作 */
export function sendMoveTask(taskId: number, req: MoveTaskService): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/task/${taskId}/move`, { data: req, method: "PATCH" }, { ...defaultOpts }),
    );
  };
}

export function getTeamComments(taskId: number): ThunkResponse<TeamComment[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/task/${taskId}/comment`, { method: "GET" }, { ...defaultOpts }),
    );
  };
}

export function sendCreateComment(taskId: number, content: string): ThunkResponse<TeamComment[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/task/${taskId}/comment`, { data: { content }, method: "PUT" }, { ...defaultOpts }),
    );
  };
}

export function getTeamAttachments(taskId: number): ThunkResponse<TeamAttachment[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/task/${taskId}/attachment`, { method: "GET" }, { ...defaultOpts }),
    );
  };
}

/** 把 Cloudreve 中的文件挂到任务上（只存引用，不复制文件） */
export function sendAttachFile(taskId: number, fileId: string): ThunkResponse<TeamAttachment[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/task/${taskId}/attachment`,
        { data: { file_id: fileId }, method: "PUT" },
        { ...defaultOpts },
      ),
    );
  };
}

export function sendDeleteAttachment(attachmentId: number): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/attachment/${attachmentId}`, { method: "DELETE" }, { ...defaultOpts }),
    );
  };
}

/**
 * 任务协作者（任务看板的「团队协作」）。
 *
 * 一个任务只有一个负责人，但可以有多名协作者共同参与；
 * 与「附件只存引用」同样的思路，协作者只引用 User，不复制任何数据。
 * 两个接口都返回刷新后的完整任务，便于调用方直接替换本地状态。
 */
export function sendAddTaskCollaborator(taskId: number, userId: string): ThunkResponse<TeamTask> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/task/${taskId}/collaborator`,
        { data: { user_id: userId }, method: "PUT" },
        { ...defaultOpts },
      ),
    );
  };
}

export function sendRemoveTaskCollaborator(taskId: number, userId: string): ThunkResponse<TeamTask> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/task/${taskId}/collaborator`,
        { params: { user_id: userId }, method: "DELETE" },
        { ...defaultOpts },
      ),
    );
  };
}

/** 我负责的任务（跨项目工作台） */
export function getMyTeamTasks(status?: TeamTaskStatus): ThunkResponse<TeamTask[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send("/team/my-tasks", { params: { status }, method: "GET" }, { ...defaultOpts }),
    );
  };
}

// ============================ 文档（Wiki）============================

export interface TeamDocNode {
  id: number;
  project_id: number;
  parent_id?: number;
  title: string;
  icon: string;
  is_folder: boolean;
  sort_order: number;
  creator?: TeamUserBrief;
  editor?: TeamUserBrief;
  updated_at: string;
  children?: TeamDocNode[];
}

export interface TeamDocDetail extends TeamDocNode {
  content: string;
}

export interface CreateDocService {
  project_id: number;
  parent_id?: number;
  title?: string;
  content?: string;
  is_folder?: boolean;
  icon?: string;
}

export interface UpdateDocService {
  title?: string;
  content?: string;
  parent_id?: number;
  icon?: string;
  sort_order?: number;
}

/** 文档树 */
export function getTeamDocTree(projectId: number): ThunkResponse<TeamDocNode[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/project/${projectId}/doc`, { method: "GET" }, { ...defaultOpts }),
    );
  };
}

export function getTeamDoc(docId: number): ThunkResponse<TeamDocDetail> {
  return async (dispatch, _getState) => {
    return await dispatch(send(`/team/doc/${docId}`, { method: "GET" }, { ...defaultOpts }));
  };
}

export function sendCreateDoc(req: CreateDocService): ThunkResponse<TeamDocDetail> {
  return async (dispatch, _getState) => {
    return await dispatch(send("/team/doc", { data: req, method: "PUT" }, { ...defaultOpts }));
  };
}

export function sendUpdateDoc(docId: number, req: UpdateDocService): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/doc/${docId}`, { data: req, method: "POST" }, { ...defaultOpts }),
    );
  };
}

export function sendDeleteDoc(docId: number): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(send(`/team/doc/${docId}`, { method: "DELETE" }, { ...defaultOpts }));
  };
}

// ============================ 通知 ============================

export interface TeamNotification {
  id: number;
  type: string;
  actor?: TeamUserBrief;
  project_id?: number;
  task_id?: number;
  doc_id?: number;
  /** 讨论区通知带的话题 id，用于「点通知跳到话题」 */
  topic_id?: number;
  title: string;
  body: string;
  is_read: boolean;
  created_at: string;
}

export function getTeamNotifications(unreadOnly = false, limit = 50): ThunkResponse<TeamNotification[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        "/team/notification",
        { params: { unread_only: unreadOnly, limit }, method: "GET" },
        { ...defaultOpts },
      ),
    );
  };
}

export function getTeamUnreadCount(): ThunkResponse<{ unread: number }> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send("/team/notification/unread-count", { method: "GET" }, { ...defaultOpts }),
    );
  };
}

export function sendReadNotifications(opts: { ids?: number[]; all?: boolean }): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send("/team/notification/read", { data: opts, method: "POST" }, { ...defaultOpts }),
    );
  };
}

// ============================ @提及 ============================

/**
 * 生成一个 @提及 的 Markdown 片段。
 * 后端用 mention:<hashid> 精确定位用户，避免重名歧义。
 */
export function buildMention(user: TeamUserBrief): string {
  return `@[${user.nickname}](mention:${user.id})`;
}

// ============================ 讨论区 ============================

/**
 * 话题分类。
 *
 * 设计参考了 Discourse / GitHub Discussions 的信息层级：
 * 列表页要能一眼看出「这是提问还是闲聊」，因此分类是话题的必备属性而非可选标签。
 */
export const TeamTopicCategory = {
  discuss: "discuss",
  announce: "announce",
  question: "question",
  share: "share",
} as const;
export type TeamTopicCategory = (typeof TeamTopicCategory)[keyof typeof TeamTopicCategory];

export const TOPIC_CATEGORIES: TeamTopicCategory[] = [
  TeamTopicCategory.discuss,
  TeamTopicCategory.announce,
  TeamTopicCategory.question,
  TeamTopicCategory.share,
];

export interface TeamTopic {
  id: number;
  project_id: number;
  title: string;
  content: string;
  category: TeamTopicCategory;
  user?: TeamUserBrief;
  is_pinned: boolean;
  is_locked: boolean;
  is_resolved: boolean;
  reply_total: number;
  view_total: number;
  last_reply_at?: string;
  last_reply_user?: TeamUserBrief;
  created_at: string;
  updated_at: string;
}

export interface TeamPost {
  id: number;
  topic_id: number;
  user?: TeamUserBrief;
  content: string;
  /** 楼层号：首帖为 1，回复从 2 开始 */
  floor: number;
  parent_id?: number;
  created_at: string;
  updated_at: string;
}

export interface TeamTopicList {
  topics: TeamTopic[];
  total: number;
  page: number;
  page_size: number;
}

export interface TeamTopicDetail {
  topic: TeamTopic;
  posts: TeamPost[];
  total: number;
  page: number;
  page_size: number;
}

export interface ListTopicService {
  keyword?: string;
  category?: TeamTopicCategory;
  page?: number;
  page_size?: number;
}

export interface CreateTopicService {
  project_id: number;
  title: string;
  content?: string;
  category?: TeamTopicCategory;
}

export interface UpdateTopicService {
  title?: string;
  content?: string;
  category?: TeamTopicCategory;
}

export interface TopicStateService {
  is_pinned?: boolean;
  is_locked?: boolean;
  is_resolved?: boolean;
}

export function getTeamTopics(projectId: number, req: ListTopicService = {}): ThunkResponse<TeamTopicList> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/project/${projectId}/topic`,
        { params: { ...req }, method: "GET" },
        { ...defaultOpts },
      ),
    );
  };
}

export function sendCreateTopic(req: CreateTopicService): ThunkResponse<TeamTopic> {
  return async (dispatch, _getState) => {
    return await dispatch(send("/team/topic", { data: req, method: "PUT" }, { ...defaultOpts }));
  };
}

export function getTeamTopic(
  topicId: number,
  page = 1,
  pageSize = 20,
): ThunkResponse<TeamTopicDetail> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/topic/${topicId}`,
        { params: { page, page_size: pageSize }, method: "GET" },
        { ...defaultOpts },
      ),
    );
  };
}

export function sendUpdateTopic(topicId: number, req: UpdateTopicService): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/topic/${topicId}`, { data: req, method: "POST" }, { ...defaultOpts }),
    );
  };
}

export function sendDeleteTopic(topicId: number): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(send(`/team/topic/${topicId}`, { method: "DELETE" }, { ...defaultOpts }));
  };
}

/** 回复话题；返回刷新后的当前页楼层 */
export function sendReplyTopic(
  topicId: number,
  content: string,
  parentId?: number,
): ThunkResponse<TeamTopicDetail> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/topic/${topicId}/reply`,
        { data: { content, parent_id: parentId }, method: "POST" },
        { ...defaultOpts },
      ),
    );
  };
}

export function sendDeletePost(postId: number): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(send(`/team/post/${postId}`, { method: "DELETE" }, { ...defaultOpts }));
  };
}

/** 置顶 / 锁定 / 标记已解决（需 moderate_topic 能力） */
export function sendTopicState(topicId: number, req: TopicStateService): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/topic/${topicId}/state`, { data: req, method: "POST" }, { ...defaultOpts }),
    );
  };
}

// ============================ 讨论区附件 ============================

/**
 * 讨论区附件。与任务附件同样的思路：**只引用 Cloudreve 文件，不复制**。
 *
 * `file_id` 收发都是 Cloudreve hashid；后端内部存整型并复用 Entity 引用计数。
 */
export interface TopicAttachment {
  id: number;
  topic_id: number;
  file_id: string;
  name: string;
  size: number;
  user?: TeamUserBrief;
  created_at: string;
}

export function getTopicAttachments(topicId: number): ThunkResponse<TopicAttachment[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/topic/${topicId}/attachment`, { method: "GET" }, { ...defaultOpts }),
    );
  };
}

/** 上传附件；返回**刷新后的全量列表**，因此调用方无需再 GET 一次 */
export function sendAddTopicAttachment(
  topicId: number,
  fileId: string,
): ThunkResponse<TopicAttachment[]> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        `/team/topic/${topicId}/attachment`,
        { data: { file_id: fileId }, method: "PUT" },
        { ...defaultOpts },
      ),
    );
  };
}

export function sendDeleteTopicAttachment(attachmentId: number): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(`/team/topic-attachment/${attachmentId}`, { method: "DELETE" }, { ...defaultOpts }),
    );
  };
}

// ============================ 管理端：团队概览 ============================

export interface AdminTeamProject {
  id: number;
  name: string;
  description: string;
  status: string;
  owner?: TeamUserBrief;
  task_total: number;
  done_total: number;
  member_total: number;
  topic_total: number;
  created_at: string;
  updated_at: string;
}

export interface AdminTeamMember {
  project_id: number;
  project_name: string;
  user?: TeamUserBrief;
  role: string;
  joined_at: string;
}

export interface Paged<T> {
  total: number;
  page: number;
  page_size: number;
}

export interface AdminTeamProjects extends Paged<AdminTeamProject> {
  projects: AdminTeamProject[];
}

export interface AdminTeamMembers extends Paged<AdminTeamMember> {
  members: AdminTeamMember[];
}

export interface AdminTeamStats {
  project_total: number;
  task_total: number;
  done_total: number;
  member_total: number;
  topic_total: number;
  post_total: number;
}

export function getAdminTeamProjects(
  keyword = "",
  page = 1,
  pageSize = 20,
): ThunkResponse<AdminTeamProjects> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        "/admin/team/project",
        { params: { keyword, page, page_size: pageSize }, method: "GET" },
        { ...defaultOpts },
      ),
    );
  };
}

export function getAdminTeamMembers(
  keyword = "",
  page = 1,
  pageSize = 20,
): ThunkResponse<AdminTeamMembers> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send(
        "/admin/team/member",
        { params: { keyword, page, page_size: pageSize }, method: "GET" },
        { ...defaultOpts },
      ),
    );
  };
}

export function getAdminTeamStats(): ThunkResponse<AdminTeamStats> {
  return async (dispatch, _getState) => {
    return await dispatch(send("/admin/team/stats", { method: "GET" }, { ...defaultOpts }));
  };
}

// ============================ 管理端：权限矩阵 ============================

/** 可配置的能力项。顺序即前端权限表格的列顺序，与后端保持一致。 */
export const TEAM_CAPABILITIES = [
  "view_project",
  "create_task",
  "edit_task",
  "delete_task",
  "manage_member",
  "manage_project",
  "post_topic",
  "reply_topic",
  "moderate_topic",
] as const;
export type TeamCapability = (typeof TEAM_CAPABILITIES)[number];

/** 可配置的角色。owner 恒为全部能力，不参与配置。 */
export const TEAM_CONFIGURABLE_ROLES = ["admin", "member", "viewer"] as const;
export type TeamConfigurableRole = (typeof TEAM_CONFIGURABLE_ROLES)[number];

export type TeamRoleMatrix = Record<TeamConfigurableRole, Record<TeamCapability, boolean>>;

export interface TeamPermissionPayload {
  matrix: TeamRoleMatrix;
  capabilities: string[];
  roles: string[];
}

export function getAdminTeamPermission(): ThunkResponse<TeamPermissionPayload> {
  return async (dispatch, _getState) => {
    return await dispatch(send("/admin/team/permission", { method: "GET" }, { ...defaultOpts }));
  };
}

export function sendAdminTeamPermission(matrix: TeamRoleMatrix): ThunkResponse<void> {
  return async (dispatch, _getState) => {
    return await dispatch(
      send("/admin/team/permission", { data: { matrix }, method: "POST" }, { ...defaultOpts }),
    );
  };
}
