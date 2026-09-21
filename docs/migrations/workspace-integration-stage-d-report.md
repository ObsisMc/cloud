# Coworker Login + Workspace UX Restoration Report

> Stage D 最终报告（§38）。分支 `workspace合并`，起点 HEAD=`39815387`，结束 HEAD=`c570b9e`。
> `main` 保持 `1c5b9b4` 未动（未 push、未 merge、未进入 Final Main Sync）。
> 决策/集成过程记录：`docs/migrations/workspace-integration-stage-d-coworker-login-workspace.md`（SD1–SD7）。

---

## 1. Reference inspected

- **Reference tree**：`C:\Users\zsl\Desktop\cloud`（HEAD `302dc9f`）— 同事的完整代码副本。
  本阶段以它作为 Login UX + Workspace UX 的 **authoritative reference**，直接读两棵树对比，
  不再依赖旧 `zpc001/feat/collab-spaces` commit diff 推断产品行为。
- 关键结论：两棵树同 Go module（`github.com/wanglongan587/cloud`），**数据 API 面 100% 兼容**
  （`/me`、spaces CRUD+members+projects+SSE、runtime workspaces）。唯一真实分歧：登录传输 —
  参考 = `cmd/devgateway` 双 JWT（sessionStorage）；当前 = `cmd/ora-web` HttpOnly `ora_subject`
  cookie + 服务端签名。按用户决策**适配 ora-web cookie 会话**，不恢复 devgateway。

## 2. Login — before / after

- **Before（本树）**：无同事登录产品面。登录走既有 email 表单 + ora-web cookie，落地到旧的
  dashboard（无工作区概念），无双 tab、无演示账号平面、无会话/工作区恢复的产品语义。
- **After**：恢复参考双 tab 登录页（居中卡片、monogram、登录 Ora）。
  - **真实账号 tab**：email 表单 → `POST /auth/login {email}`（ora-web 设 HttpOnly
    `ora_subject` cookie）→ `setCloudSession`（persist `ora-auth`：
    `{user, tenantId, tenantName}`）→ 重定向 `/{首个空间 slug}/projects`。
  - **演示账号 tab**：参考表单（默认邮箱）→ `useDemoLogin`（mockApi `/auth/login`）→
    `setDemoSession`（persist `ora-mock-auth`：`{token, user}`）→ 重定向
    `/${db.workspace.slug}/issues`。
  - 已登录重定向：真实（有 `tenantId`）→ 默认工作区；演示（有 token）→ 其 issue 板；均不显示登录页。

## 3. Workspace — before / after

- **Before**：无 Current Workspace 概念。Stage B 只合入了 Space 后端 + 独立 space 页面；
  产品外壳（provider、左上选择器/创建/切换、workspace 驱动的资源上下文）被 D4/D5 排除。
- **After**：完整恢复参考外壳。
  - `features/spaces/current-space.tsx`（新）：`cloudMode = tenantId != null`；tenantId 来自
    auth-store（cookie 会话，无需 `/me/tenants`）；spaces 经 `useSpaces(tenantId)`；
    `space = spaces?.find(slug)`。值 `{cloudMode, tenantId, spaces, space}`。
  - `dashboard-layout.tsx`：门禁（cloud 需 tenantId / demo 需会话，否则 `/login`）；未知/已归档
    slug 重定向（cloud → 首个加入空间 `/projects`，demo → `/ora-demo/issues`）；空工作区态；
    shell 级挂 `useSpaceEvents(tenantId, space?.id)`，SSE 跟随当前空间（§23 退订旧/订阅新）。
  - `app-sidebar.tsx`：左上 `SidebarHeader` DropdownMenu — 当前工作区 + 列表（cloud=已加入空间 /
    demo=`db.workspaces`）带选中勾；**新建工作区**（cloud）→ `CreateSpaceDialog`，
    `onCreated={(slug) => navigate(`/${slug}/projects`)}`（§11 创建后自动选中并跳转）；切换
    `navigate(`/${ws.slug}/${target}`)`，`target = cloudMode ? 'projects' : 'issues'`（§12）。
    登出：`clear()` + `/login`。nav 按 cloudMode 门控（SD7：真实后端功能在 demo 模式隐藏）。

## 4. Files — restored verbatim / semantically merged / not restored

**Restored verbatim from reference**（适配仅限 import 重命名 / 可空字段）：
- `frontend/src/features/projects/create-project-dialog.tsx`
- `frontend/src/features/projects/projects-page.tsx`
- `frontend/src/features/members/members-page.tsx`
- `frontend/src/features/settings/general-settings-page.tsx`
- `frontend/src/features/projects/project-detail-page.tsx`（外壳逐字；body 保留我们真实的
  issues-under-project 集成，见 §9）

**Semantically merged**（参考结构 + 本树适配）：
- `frontend/src/features/spaces/current-space.tsx`（新，cookie 适配器版）
- `frontend/src/components/layout/dashboard-layout.tsx` / `app-sidebar.tsx`
- `frontend/src/features/auth/login-page.tsx`（email 适配 + 双 tab）与 `features/auth/api.ts`
  （保留 cloud `useLogin`/`useLogout` + 新增 `useDemoLogin`）
- `frontend/src/features/projects/api.ts`（双模：cloud `useSpaceProjects` → `cloudProjectToUI`；
  `workspaceId: p.spaceId ?? ''`，`space_id` 可空 D2 注释）
- `frontend/src/features/members/api.ts`（双模 + `cloudMemberToUI`）
- `frontend/src/routes.tsx`（space-slug 模型 + `WithSlug`/`CloudScope` 拆分）
- `frontend/src/lib/mock-api-client.ts`（mock token 头 + cloud-mode slug 重写）
- `frontend/src/mocks/data`、`frontend/src/mocks/handlers`（seed `ora-demo`/`ora-labs`/`personal`）
- `frontend/src/state/auth-store.ts`（双会话槽）

**NOT restored（Q1 决策的必然结果）**：
- `lib/cloud-session.ts`、`test/cloud-handlers.ts`、`cmd/devgateway`、JWT axios interceptor —
  **均不恢复**。`cmd/ora-web` 保持。`lib/api-client.ts` 保持无拦截器。
- `issues.space_id` schema 迁移 — **不做**（§14：不伪造不支持的 scoping）。

## 5. Session / auth flow

```
真实：POST /auth/login {email} ── ora-web ──▶ 200 {user,tenantId,tenantName} + HttpOnly ora_subject cookie
      ──▶ setCloudSession (persist ora-auth) ──▶ /{spaceSlug}/projects
演示：useDemoLogin → mockApi /auth/login ──▶ setDemoSession (persist ora-mock-auth) ──▶ /ora-demo/issues
cloudMode = tenantId != null  （持久化 tenantId 判定，与 cookie 会话一致）
登出：clear() 双会话 → /login
```

- ora-web 单口 edge（127.0.0.1:8080）：serve `frontend/dist`、处理 `/auth/login|logout`、
  反代 `/api/`、`/internal/`、`/healthz`。cookie 由浏览器持有，服务端签名。

## 6. Workspace state model

- URL：`:workspaceSlug` = **协作空间 slug**（如 `/default`）。
- 解析：`CurrentSpaceProvider` `space = spaces.find(slug)`；内部 API 用 `tenantId` + `space.id`。
- 演示平面：space = `db.workspace`（`workspaceBySlug`），无网络。
- 术语：前端产品名 Workspace/工作区 == Collaboration Space；runtime 对象 = Runtime Workspace（不动）。

## 7. Selector / create / switch behavior

- **Selector**：cloud 列已加入空间（`GET /tenants/:tid/spaces`），当前项勾选；demo 列 seed
  工作区。DropdownMenu 由 `SidebarHeader` 承载。
- **Create**：新建工作区 → `CreateSpaceDialog` → `onCreated(slug)` → `navigate('/'+slug+'/projects')`
  （**创建后自动选中并跳转**，§11 修复——不落回 `() => undefined`）。
- **Switch**：`navigate('/'+ws.slug+'/'+target)`，cloud 落 projects / demo 落 issues（§12 全链语义）。
  空间级 query 因 key 内嵌 space id 自动重建；SSE 退订旧/订阅新（§23）。

## 8. Resource scoping matrix（WORKSPACE RESOURCE SCOPING）

| 资源 | 作用域 | 说明 |
| --- | --- | --- |
| Projects | **space 级**（真实） | `useSpaceProjects(tenantId, space.id)`；创建 `POST /tenants/:tid/spaces/:spaceId/projects`（202 异步，关 dialog，失效 space-projects key）；列表 owner 隔离（D1）；`space_id` 可空（D2）。 |
| Runtime Workspaces | 项目派生 | 随创建 operation 202 返回，未在 shell 直接展示。 |
| Members | **space 级**（真实） | 空间成员 + role/status 管理；owner 可加成员（PUT upsert）。 |
| Settings | **space 级**（真实） | 改名（版本守卫）+ owner 归档危险区（S3 owner-only）。 |
| Issues / Workflow | **tenant 级**（真实） | 保持 `useIssues(tenantId, q?)`；同 tenant 切换 Workspace 不改变 issue 列表（诚实）。**WORKSPACE SCOPING GAP**（见 §11）。 |
| 收件箱/聊天/AI 团队等 | demo 平面（mock） | 云模式 mock 页经 interceptor slug 重写展示演示数据；真实表面在 demo 隐藏（SD7）。 |

## 9. Issues integration（真实 backend）

- Issues 挂回外壳（cloud 模式）经 `CloudScope` 转发 `tenantId`；路由 `/:workspaceSlug/issues`、
  `issues/:issueId`、`my-issues`。
- 项目详情页内 issues-under-project：云模式 `useIssues(tenantId ?? '', '')` 过滤
  `projectRef === project.id` 渲染 `IssueRow`；demo 模式渲染 mock store 行。后端 issues 列表无
  项目过滤器，仅 search term — 前端诚实过滤（文档化，非伪造）。
- `IssueRow` 需 `members: ReadonlyMap<string,string>`（`useMembers` + `memberNameById`）。
- Issues 保持 tenant 级：**ISSUES INTEGRATION: COMPLETE**（集成完成），但**作用域为 tenant 而非
  workspace**，如实记录为 WORKSPACE SCOPING GAP。

## 10. Query / cache / SSE

- 查询 key 内嵌 space id：`['space-projects', tenantId, spaceId]`、`['cloud-project', tenantId, pid]`、
  `['space-members', ...]`；切换工作区后 A 的缓存仍在、B 独立加载，**无跨工作区泄漏**
  （`spaces/api.test.tsx` 用查询键断言验证 A→B 重建 + 互不渗入）。
- SSE：shell 级 `useSpaceEvents(tenantId, space?.id)`；空间切换退订旧空间、订阅当前空间（§23）。
- 会话恢复：持久化 `ora-auth`/`ora-mock-auth` + cookie → 刷新后 shell/工作区/资源上下文恢复。

## 11. Backend adaptations & architectural gaps

- **Backend：零代码改动**。数据面与参考 100% 兼容；Stage B 的 S1（Idempotency-Key）/S2（归档 slug
  409）/S3（归档 owner-only）/默认空间保护/SSE publish-after-commit 全部保留。验证仅
  `go build` + integration 套件（全绿，26.5s）。
- **Architectural gaps**：
  1. **WORKSPACE SCOPING GAP**：后端 Issues 无 `space_id`，只有 tenant + 可选 project ref。
     前端不伪造、不做大 schema 迁移；同 tenant 内切换 Workspace 不改变 issue 列表。
  2. demo 模式隐藏真实后端功能（任务/我的任务/空间/成员）— 参考是 mock、我们是真实后端，
     不伪造数据（SD7 记录）。
  3. 登录传输差异（cookie vs 双 JWT）已按决策适配，参考的 devgateway 语义（source/subject/
     display 字段）不迁移。
- **ADAPTER FIX**：无（smoke 通过未发现需后端补字段）。

## 12. Tests / gates / smoke

- **新增测试（§30）**：`login-page.test.tsx`（双 tab 渲染、真实登录存会话、失败提示、demo 登录、
  会话恢复重定向）；`app-sidebar.test.tsx`（demo 列表/切换、cloud 列空间/切换落 projects/新建
  入口）；`spaces/api.test.tsx`（space projects A→B 不跨区泄漏，查询键断言）；`members-page.test.tsx`
  （member 只读 / owner 加成员 body 断言 / demo 表格）；`settings-layout.test.tsx`、`spaces-page.test.tsx`
  回归。
- **前端 gates 全绿**：`format:check`/`lint`/`typecheck`/`build`/`check:modules`/`check:docs`/`check:dup`。
  `test`/`check:dead` **ENVIRONMENT-BLOCKED**（Node v20.18.1 vs engines>=24，ESM `ERR_REQUIRE_ESM`）—
  记录不重跑（§35）。
- **Backend**：`go build ./...` 0；`go test -count=1 ./...`（REQUIRE_POSTGRES=1 +
  `TEST_DATABASE_URL`）全绿，含 `TestSpaceMutationsRequireIdempotencyKey`、
  `TestArchivedSpaceSlugStaysReserved`、`TestDefaultSpaceCannotBeArchived`、
  `TestProjectSpaceScopingAndOwnerIsolation`。
- **Smoke path（真实 PG + dist + `go run ./cmd/ora-web` :8080）**：根/healthz 200 → 登录
  （session JSON）→ cookie 下 `/api/v1/me` 200 → 空空间 → 建空间（创建者 owner）→ 项目创建 202
  异步（`spaceId` 绑定）→ 项目列表 200 → issues 状态/列表/创建（`projectRef`）→ 登出后 `/me` 401。
  已清理 smoke 数据。演示平面经 `npm run dev`（MSW）核对逻辑。

## 13. Docs

- `docs/migrations/workspace-integration-stage-d-coworker-login-workspace.md` — 决策/集成记录
  （COMPLETE，含作用域矩阵）。
- `specs/decisions/cloud/collaboration-workspace/0-frontend-workspace-product-shell.md` — ADR
  SD1–SD7（编码前先写 ADR，铁律；plan 批准即用户同意）。
- `docs/INDEX.md` — Stage D 产品结构条目 + 当前状态已更新。
- 本报告（§38）。

## 14. Commits / main / working tree

- 提交（分支 `workspace合并`）：`c4a7510`（决策+ADR）→ `b1f472d`（会话+登录）→ `f09ae06`
  （shell）→ `5a13a13`（工作区项目）→ `ba7d78b`（设置/成员并入）→ `c570b9e`（测试集+docs）。
- `main`：保持 `1c5b9b4`（未动、未 push、未 merge、未进入 Final Main Sync）。
- 工作树：干净（`git diff --check` 无冲突标记）。

---

## 结论

- **LOGIN UX: COMPLETE**（双 tab 真实+演示、ora-web cookie 会话、会话恢复/重定向）
- **WORKSPACE UX: COMPLETE**（Current Workspace shell、选择器/创建/切换全链、SSE 跟随、slug 路由）
- **WORKSPACE RESOURCE SCOPING: COMPLETE**（Projects/Members/Settings 真实 space 级；切换重建、无缓存泄漏）
- **ISSUES INTEGRATION: COMPLETE**（真实 tenant 级 Issues 挂回外壳 + 项目详情页集成）
- **WORKSPACE SCOPING GAP: Issues 为 tenant 级而非 workspace 级 — 后端无 `issues.space_id`，
  前端不伪造、无大 schema 迁移。** 留给后续后端演进。

停止。不要进入 Final Main Sync。（本阶段结束；`main` 未动、未 push；下次集成前需重新做
Integration Review。）
