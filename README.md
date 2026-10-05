# TeamCloudreve

> 基于 [Cloudreve](https://github.com/cloudreve/Cloudreve) v4.19.1 的团队协作增强版：
> 项目空间、文件级访问控制（可读/可写）、团队回收站与恢复、项目角色（owner/admin/member/viewer）。

**版本**：4.19.1 · Commit `a8becb9-s0b-v22`

## 仓库结构

```
backend/    服务端（Go，基于 cloudreve/Cloudreve v4.19.1）
frontend/   Web 前端（基于 cloudreve/frontend）
LICENSE     GPLv3
```

## 快速开始（零配置）

1. 下载对应平台的 zip，解压到任意目录，直接运行 `cloudreve`（Windows 为 `cloudreve.exe`）。
   - 首次启动会在【exe 所在目录】生成 `conf.ini` 与 `data/`（含数据库），
     **不依赖当前工作目录**；路径请避免中文与空格。
   - 默认监听 `http://127.0.0.1:5212`。
2. 浏览器打开后**注册第一个账号 —— 它会自动成为管理员**
   （服务端日志会记录这次提权：`First human user registered ... granted admin`）；
   随后请立刻修改密码。
3. 服务 / 计划任务部署：必须加 `-w`，并把工作目录设为数据目录
   （当 `conf.ini` 中 `DBFile` 为相对路径时，数据库落在工作目录）。

## 校验下载的文件

| 文件 | SHA256 |
|---|---|
| `cloudreve-windows-amd64.zip` | `74BEF812B7545075796288C1412768BFA3008A1AD37A4874CA8AB24A8147A822` |
| `cloudreve-linux-amd64.zip` | `0E39DA623CFD498A51D1A2237E4B8B55595B130BF7C4607CCF08475F3A0602E4` |
| `cloudreve-linux-arm64.zip` | `FEDF47D1600D9A3861E65CEF2959F6A78D30415CE1C385C1B22E2C1B0C685736` |

Windows 可执行文件本体 MD5：`225CAE0FF0967689B54FCE431EB095AC`（与发布者自测实例一致）。

每个 zip 内附 `说明.txt`，含可执行文件本体的 MD5/SHA256。
Release 页会附同表；构建采用 `-trimpath`，源码相同则哈希可复现（见下方"从源码构建"）。

## ★ 关于杀毒软件误报（Linux）

部分杀毒软件（如火绒）可能将 Linux 二进制报告为
`HackTool/Linux.Proxytool` 并隔离。**这是针对所有 Cloudreve 官方 Linux
发行版的通用启发式误报，并非本项目引入**（已实测：官方 4.19.1 同签名、
同隔离行为）。处理方式：

1. 核对上表 SHA256（或 zip 内 `说明.txt` 的哈希）；
2. 将文件加入杀软信任区；或
3. 按下方"从源码构建"自行编译。

（Windows 产物实测未触发该误报；发布仅上传 zip，不提供裸 ELF。）

## 从源码构建

依赖：**Node ≥ 18 + Yarn**（仓库的依赖树以 `yarn.lock` 为准 —— 发布产物即由此
锁构建；`package-lock.json` 为上游遗留，未使用）、Go ≥ 1.21。

```bash
git clone https://github.com/FengHuaworker/TeamCloudreve.git
cd TeamCloudreve

# 1) 前端
cd frontend
export NODE_OPTIONS="--max-old-space-size=4608"   # 8GB 内存机器实测值；内存充裕可设 8192
yarn install --frozen-lockfile
yarn run build                                     # 产物在 build/
cd ..

# 2) 打包为后端嵌入的 assets.zip（必须，zip 内部前缀须为 assets/build/）
#    脚本自带两条断言：新鲜度链 + 前缀零错（前缀错了会白屏），不满足即退出
python scripts/repack_assets.py repack

# 3) 后端
cd backend
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o cloudreve .
```

（Windows PowerShell 对应：`$env:NODE_OPTIONS="--max-old-space-size=4608"`；
交叉编译：`GOOS=linux|windows GOARCH=amd64|arm64`；
`-trimpath` 与 Release 产物构建参数一致，源码与依赖相同则哈希可复现。）

## 团队协作功能速览

- **项目空间**：`/team` 创建项目，成员按角色分工（owner / admin / member / viewer，
  管理员可在 后台 → 团队设置 中配置角色能力矩阵）。
- **文件权限**：文件管理器中右键任意文件/目录 → 「权限」，
  为指定用户设置可读 / 可写（含"对子文件夹生效"）。
  - 团队文件的权限仅项目 owner/admin/全局管理员可管理；
  - 项目根目录也可直接设置（等于整项目只读/可写）；
  - 请求路径必须精确存在（严格模式），不会静默落在父目录上；
  - 项目所有者不会被权限锁在自己的项目之外。
- **团队回收站**：项目页 → 「回收站」tab 查看/恢复本项目删除的文件；
  恢复回原位置、保留原对象与内容。
  个人空间的回收站行为与上游一致。

## 已知限制

1. 权限目前仅支持**按用户**设置；用户组级权限可经 API 直接调用
   （`subject_type=group`），界面支持待后续版本。
2. 团队回收站恢复不推送 websocket 文件事件（恢复后刷新页面即可见）。
3. 团队回收站只包含团队空间删除的文件；个人回收站只包含个人文件（互不可见）。
4. 上传/新建沿用上游"解析到最近已存在祖先"的落位约定（属上游设计）。
5. Linux 两个架构（amd64/arm64）未在发布机上冒烟，欢迎反馈启动日志。

## 许可证与修改声明（GPLv3 §5a）

本作品是基于上游 Cloudreve 的修改版本（derivative work），按 GPLv3 分发：

- 服务端基于 [cloudreve/Cloudreve](https://github.com/cloudreve/Cloudreve)
  **v4.19.1（commit `a8becb9`）** 修改；
- 前端基于 [cloudreve/frontend](https://github.com/cloudreve/frontend)
  **v4.19.1（commit `19da0fe`）** 修改；
- 修改始于 2026-10 月，完整修改内容以本仓库 git 历史为准，上游原始源码见上述链接；
- LICENSE（GPLv3 全文）见仓库根目录。

### 主要修改清单

- 团队协作模块（项目/任务/文档/讨论/成员角色），独立表结构与迁移；
- 文件 ACL（`file_permissions`）与节点级能力位计算；
- 团队空间文件系统 `cloudreve://team` 及其导航器；
- 团队回收站（含恢复、权限严格模式、owner 锁死保护）；
- 首个注册者自动成为管理员的判据修复（系统账号占位后仍成立）；
- 前端：团队 UI、回收站/权限对话框、空目录文案的节点级能力位修正。
