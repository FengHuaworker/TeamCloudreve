// 团队协作模块的懒加载出口。
// 与 Cloudreve 的 Pages.tsx / AdminBundle.tsx 保持同样的分包策略：
// 只有访问 /team 时才会下载这部分代码。
//
// 看板 / 文档 / 讨论 三个视图不再是独立路由，而是 TeamProject 内部的 tab，
// 因此不从这里导出，避免被误当成页面直接挂到路由上。
export { default as TeamProjects } from "./TeamProjects.tsx";
export { default as TeamProject, TeamDocsRedirect } from "./TeamProject.tsx";
export { default as TeamDiscussionAll } from "./TeamDiscussionAll.tsx";
