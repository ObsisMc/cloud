# Workspace Integration — Stage A（origin/main → workspace合并）设计决策报告

> 记录 2026-09-21 将 `origin/main`（`766d5350`）受控融合进工作分支 `workspace合并` 的完整过程与决策。
> 集成顺序固定为：**Stage A（origin/main → workspace合并）先行，Stage B（zpc001/feat/collab-spaces → workspace合并）后续**。
> 本报告只覆盖 Stage A；Stage B 的已知候选决策见文末预告。

> **⚠️ 迁移序列状态（2026-09-23 reconciliation 后）**：本文 §4 的编号是**当时的历史开发序列**。
> 当前 upstream-ready 序列已重组为 append-only：`0001–0007` = upstream 原样（含 `0006_collab_spaces`
> / `0007_project_space_scope`），本地 Issue 迁移前移至 `0008–0012`，`0013_project_space_optional`
> 恢复 `projects.space_id` 可空。见 `docs/development/agent/database.md` 的迁移表（当前权威）。

## 0. 约束（铁律）

- **main 只读**：整个过程中 `main` 指针不动（Stage A 前后均为 `1c5b9b4`，SHA 已校验）。
- **所有变更只落在 `workspace合并`**。
- 涉及产品/领域/授权/数据模型/API 语义/架构方向的冲突**不自动裁决** → 停下问用户。
- 生成的产物（`api/openapi.json`、前端客户端）**不是权威**：先解决 `internal/contract/openapi.go` 再重新生成。
- 迁移编号必须全局唯一；应用的迁移不编辑。

## 1. 冲突分类

| 类别 | 含义 | 处理 |
| --- | --- | --- |
| CLASS M | 机械（重命名、编号、格式） | 自动解决 |
| CLASS S | 语义但共存明显 | 自动解决 + 记录 |
| CLASS D | 设计（产品/领域/授权/数据模型/API/架构/安全/数据库） | **STOP → 问用户** |

## 2. 用户决策（CLASS D，已问用户）

| ID | 议题 | 决策 |
| --- | --- | --- |
| **D-Auth** | 前端认证/会话基座选哪套 | **A: ora-web cookie 会话（推荐）** — 保留 HEAD 的 cookie 会话模型；22 个 auth/shared + 12 个 Issues 冲突文件向 HEAD 的会话模型收敛 |
| **D4** | 顶层路由参数 `:workspaceSlug` 的语义 | **a: Issues 保留 tenantId（推荐）** — Issues 保持 `/{tenantId}/...`，Spaces（Stage B）使用独立前缀 |

## 3. 架构决策（CLASS S，自动解决 + 记录）

| ID | 决策 | 说明 |
| --- | --- | --- |
| A1 | 后端统一 auth 契约 = **dual-JWT**：`Authorization: Bearer <serviceToken>` + `X-Ora-User-Token`（`user.Caller == service.Subject`），用于 public/access/admit 路由 | 合并后 router 契约；origin 单 token 与 HEAD 本地实现均向此收敛 |
| A2 | 前端会话基座 = HEAD 的 `cmd/ora-web` edge server（:8080，`ora_subject` HttpOnly cookie，edge 每请求签 gateway token + 重签 user token，进程内转发） | 由用户 D-Auth 定案；origin/main 的 mock token 基座（`ora-mock-auth`/MSW）丢弃 |
| A3 | migration 冲突 `0005_issues` vs `0005_gateway_auth` → **保留上游编号稳定**，本地 Issues 迁移前移重编号 `0005→0006 … 0009→0010` | CLASS M（编号机械），按规格 step 21 |
| A4 | `openapi.go` 契约生成器 staged 合并：保留 HEAD 的 `contextRefTypeEnum()`/`optional()`/`pathParamSchema()`（formRef 特例）+ origin/main 的 `tag()` | 契约权威先于生成物 |
| A5 | 前端 members 形状 = `TenantMember`（`userId`/`displayName`，`role admin\|member`，`status active\|disabled`） | origin 的 owner/invited/joinedAt 与后端数据不兼容 |
| A6 | 前端 project-detail 用 mock-store issue 列表（`db.issues.filter(i => i.projectId === project.id)`） | origin 的 `useIssues(slug,{projectId})` 与后端接口不兼容；mock 数据侧记录 rationale |
| A7 | origin 的 mock workspace-switcher（`db.workspaces`）丢弃 | HEAD 侧无对应数据/语义 |
| A8 | 15 个 HEAD Issues 文件 `git checkout --ours`（api/api.test/issues-board/issue-detail/issues-list/create-issue-dialog/issue-card/issue-row + 测试） | HEAD 功能保持优先；冲突文件均为 HEAD 会话模型下的实现 |

## 4. 迁移重编号（A3 细节）

`git mv` 后的完整序列：

```
0001_core.sql
0002_aggregate_guards.sql
0003_resource_versions.sql
0004_effect_intent_and_ticket_scope.sql
0005_gateway_auth.sql                ← 上游（origin/main）新增
0006_issues.sql                      ← formerly 0005_issues.sql
0007_issue_extensions.sql            ← formerly 0006_issue_extensions.sql
0008_issue_collaboration.sql         ← formerly 0007_issue_collaboration.sql
0009_issue_interactions.sql          ← formerly 0008_issue_interactions.sql
0010_issue_interaction_input.sql     ← formerly 0009_issue_interaction_input.sql
```

校验：重编号后全量后端测试通过（integration 23.9s，含 `TestIssueCollaborationMigrationBackfills` 的新版本清单）。

## 5. 前端验证

合并分支须通过上游 `frontend.yml` 门禁（format:check / lint / typecheck / test）。

| 门禁 | 结果 | 说明 |
| --- | --- | --- |
| format:check | ✅ | 含 `npm run format` 对 4 个已编辑文件的 prettier 重排（app-sidebar/issue-badges/login-page/actor-avatar，均为有意） |
| lint | ✅ | 修复 HEAD 预存在的 8 处 lint 债（行为保持）：actor-avatar `as const`/去嵌套三元；issues-list complexity；create-issue-dialog 数组默认 prop → 模块常量；context-refs-panel + 2 个测试文件 unsafe assertion → 运行时类型守卫 / `Object.entries` 免断言改写；issues-board max-lines |
| typecheck | ✅ | `tsc -b` 干净 |
| build | ✅ | vite build 成功（Node 版本与 chunk-size 警告为预存在、非阻塞） |
| check:modules / check:docs / check:dup | ✅ | |
| test / check:dead | ⚠️ **环境阻断** | Node v20.18.1 vs `engines >=24`：`html-encoding-sniffer`（test）/ `oxc-parser`（knip）要求 ESM/新 Node → `ERR_REQUIRE_ESM`。**预存在，非合并引入**（package.json/lockfile 合并前后未变），无替代 Node 可用 |

## 6. 未纳入 / 保持不动

- `docs/migrations/multica-issue-board/*` 冻结的历史归档不重写。
- `cmd/gateway`、`internal/gateway/*`、`configs/gateway.yaml`、`docs/gateway.md`、`integration/gateway_test.go`、`.github/workflows/frontend.yml` 等为上游新增，照收。
- PR #13（`aeea9fd`）**不合并**（规格 step 4）。

## 7. Stage B 预告（未开始）

下一步：`zpc001/feat/collab-spaces` → `workspace合并`。已知 CLASS D 候选（须停下问用户）：

- **D1** project 可见性（2026-09-21 已被 [workspace-sharing-model.md](./workspace-sharing-model.md) superseded：owner-only 为当前实现，migration pending）
- **D2** 层级 `Tenant → Space → Project → Runtime Workspace`
- **D3** Space 命名
- **D5** devgateway vs ora-web（两个 edge 桥接同一 dual-JWT 核心）
- **D6** 现有行为移除
- Space 默认归档
- **新 migration 冲突**：coworker `0006_collab_spaces` / `0007_project_space_scope` vs 重编号后的 `0006_issues` / `0007_issue_extensions`
- 安全冲突一律 CLASS D；数据库设计冲突一律 CLASS D

## 8. 结论

合并已完整解决并验证。唯一未跑通的门禁（前端 test / knip）由 Node 版本环境阻断，非合并缺陷，已如实记录。

**WORKSPACE INTEGRATION STAGE A: COMPLETE**
