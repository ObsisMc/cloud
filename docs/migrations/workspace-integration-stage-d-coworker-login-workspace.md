# Workspace Integration Stage D — 同事 Login + Workspace UX 恢复（决策 / 集成记录）

> 状态：**COMPLETE**（决策经 plan `distributed-wishing-ritchie` 用户批准后实施完成）
> 与 Stage A/B/C 同级，属集成过程记录。分支 `workspace合并`，起点 HEAD=`39815387`，结束 HEAD=`c570b9e`，
> `main` 保持 `1c5b9b4` 未动（未 push、未进入 Final Main Sync）。
> 关联 ADR：`specs/decisions/cloud/collaboration-workspace/0-frontend-workspace-product-shell.md`（SD1–SD7）。
> 结束标记：`WORKSPACE INTEGRATION STAGE D: COMPLETE`

---

## 0. 背景与动机

Stage B/C 合并了同事的 **Space 后端**（`collab_workspaces`、`/spaces` API、SSE、space 页面），但
**有意排除了同事的完整产品外壳**：Login、会话引导、Current Workspace provider/context、左上
Workspace 创建入口、左上 Workspace 选择器、Workspace 切换、Workspace 驱动的资源上下文。当时的
D4/D5 决策把登录与 Current Workspace shell 排除在外。复盘后确认这是**不完整的集成**：后端功能已合并，
同事的产品交互未合并。本轮恢复。

- **Reference tree**：同事的完整代码副本（本地 checkout，HEAD `302dc9f`），Login UX +
  Workspace UX 的 authoritative reference。不再依赖旧 `zpc001/feat/collab-spaces` commit diff 推断
  产品行为，直接读两棵树对比。
- **数据面**：两棵树同 Go module（`github.com/wanglongan587/cloud`），数据 API 100% 兼容
  （`/me`、`/me/tenants`、spaces CRUD+members+projects+SSE、runtime workspaces）。唯一真实分歧：
  **登录传输**。参考 = `cmd/devgateway` 双 JWT（sessionStorage）；当前 = `cmd/ora-web` HttpOnly
  `ora_subject` cookie + 服务端签名，浏览器不持 key。

---

## 1. 决策结论（CLASS D — 已决）

| 决策 | 选择 | 结论 |
| --- | --- | --- |
| SD1 — 产品外壳恢复 | **恢复** | 恢复同事登录页 + Current Workspace shell + 左上 Workspace 选择器/创建/切换。`:workspaceSlug` 路由参数改为**协作空间 slug**（如 `/default`），真实后端页面经 CurrentWorkspaceProvider 解析 `tenantId`。**部分推翻 Stage B D4**（Issues 现在挂在 `/:workspaceSlug/...` 之下）。 |
| SD2 — 会话适配 | **维持 D5=A 并扩展** | ora-web cookie 会话为前端权威会话；`cloudMode` 由持久化 `tenantId` 判定；不恢复 `cmd/devgateway`、`cloud-session.ts`(+test)、`cloud-handlers.ts`、JWT axios interceptor。`lib/api-client.ts` 保持无拦截器。auth-store 扩展 demo 会话槽。 |
| SD3 — 双平面 | **完整双 tab** | 登录页双 tab：真实账号 = ora-web email/cookie 登录；演示账号 = mock store 登录（seed 工作区 `ora-demo`/`ora-labs`/`personal`）。演示平面纯前端 mock。 |
| SD4 — Issues 作用域 | **tenant 级（后端不变量）** | Issues 保持 tenant 级，**不引入 `issues.space_id` 迁移**。同 tenant 内切换 Workspace 不改变 issue 列表（诚实）。记录 **WORKSPACE SCOPING GAP**；禁止伪造空间作用域的大 schema 迁移。 |
| SD5 — 项目作用域 | **真实 space 级** | 项目创建走 `POST /tenants/:tid/spaces/:spaceId/projects`（202 异步，立即关 dialog，失效 space 项目查询）；列表维持 owner 隔离（D1 —— 2026-09-21 superseded，owner-only 为当前实现，migration pending，见 [workspace-sharing-model.md](./workspace-sharing-model.md)）；`projects.space_id` 保持可空（D2=C）。 |
| SD6 — 切换语义 | **全链恢复** | 路由更新 + CurrentWorkspace context 更新 + 空间级 query 失效（key 内嵌 space id）+ SSE 退订旧空间、订阅当前空间。 |
| SD7 — 演示模式隐藏真实功能 | **隐藏** | 任务/我的任务/空间/成员（真实后端功能）在 demo 模式隐藏；demo 只展示 mock 预览功能。参考 Issues 是 mock、我们是真实后端——不做数据伪造。 |

---

## 2. 实施内容（checkpoints）

1. **会话 + 登录**：auth-store 双会话（cloud `{user,tenantId,tenantName}` + demo `{token,user}`）；
   `features/auth/api.ts` 保留 cloud `useLogin`/`useLogout` + 新增 `useDemoLogin`（mock `/auth/login`）；
   恢复双 tab `login-page.tsx`（真实账号 = email/cookie → `/default/projects`；演示账号 → `/${db.workspace.slug}/issues`）。
2. **Shell**：新建 `features/spaces/current-space.tsx`（`cloudMode = tenantId != null`；tenantId 来自
   auth-store；spaces 经 `useSpaces`；space = `spaces.find(slug)`）；恢复 `dashboard-layout.tsx`（门禁、
   未知/已归档 slug 重定向、空工作区态、shell 级挂 `useSpaceEvents(tenantId, space?.id)` 使 SSE 跟随当前
   空间）；恢复 `app-sidebar.tsx`（左上选择器 + 新建工作区 + 切换 + 登出；nav 按 cloudMode 门控）；
   `routes.tsx` 改 space-slug 模型 + `WithSlug`/`CloudScope` 拆分；`paths.ts`。
3. **Projects**：恢复双模式 `features/projects/api.ts`（cloud `useSpaceProjects` → `cloudProjectToUI`，
   demo mock）+ `create-project-dialog.tsx`（`onCreated → navigate('/${slug}/projects')`）+ 新建按钮 +
   空状态；detail 保留我们真实的 issues-under-project 集成。
4. **重挂接**：Issues/Workflow/Spaces/members/settings 经 `CloudScope`（转发 `tenantId`）挂回 shell
   （cloud 模式）；SSE 跟随当前空间；demo 模式 nav 门控（隐藏真实后端功能）。
5. **测试 + 文档**：登录/选择器/切换/缓存无泄漏/回归测试；`docs/INDEX.md` 产品结构更新；本记录更新为
   COMPLETE；最终报告。

提交（checkpoint → commit SHA，分支 `workspace合并`）：
1. `c4a7510` — 决策记录 + ADR（编码前先写 ADR，铁律）
2. `b1f472d` — 会话 + 登录（双会话 auth-store、useDemoLogin、双 tab login-page）
3. `f09ae06` — Shell（current-space / dashboard-layout / app-sidebar 选择器 / routes space-slug）
4. `5a13a13` — 工作区项目（双模 useProjects、新建对话框、云感知详情页）
5. `ba7d78b` — 设置/成员并入外壳（云感知成员管理、空间设置 + 归档危险区）
6. `c570b9e` — 测试集（登录双 tab、选择器切换、资源不跨区泄漏）+ docs

---

## 3. 验证（实施完成，实测通过）

- [x] `go build ./...` 通过；`go test -count=1 ./...`（REQUIRE_POSTGRES=1 +
      `TEST_DATABASE_URL=postgres://ora:ora-local@127.0.0.1:55432/ora?sslmode=disable`）通过
      （integration 26.5s ok）。关键 space 测试保持绿：
      `TestSpaceMutationsRequireIdempotencyKey`、`TestArchivedSpaceSlugStaysReserved`、
      `TestDefaultSpaceCannotBeArchived`、`TestProjectSpaceScopingAndOwnerIsolation`。
- [x] 前端 gates 全部绿：`format:check`/`lint`/`typecheck`/`build`/`check:modules`/`check:docs`/`check:dup`
      （`test`/`check:dead` 受 Node v20.18.1 vs engines>=24 ESM 环境阻断，记录不重跑）。
- [x] 新增测试（§30）：登录（渲染/成功/失败提示/会话恢复/重定向——真实与演示两 tab）、选择器（demo
      列表/切换到 issues、cloud 列真实空间/切换落 projects/新建入口）、Workspace 资源切换（A→B 查询
      重建且 A 缓存不渗入 B，`spaces/api.test` 查询键断言）、缓存无跨工作区泄漏、回归（members 云/
      演示双模、settings demo 渲染、spaces 归档权限）。环境阻断未运行，编码与类型/门禁已验证。
- [x] Smoke path（真实 PG + 构建 dist + `go run ./cmd/ora-web` :8080）：根与 healthz 200 → 登录
      `POST /auth/login` 返回 `{tenantId,tenantName,user}` → 携带 HttpOnly `ora_subject` cookie 的
      `/api/v1/me` 200 会话恢复 → 空空间列表 → 创建工作区（创建者为 owner）→ 项目创建 202 异步
      （`spaceId` 绑定）→ 项目列表 200 → Issues 状态/列表/创建（`projectRef` 绑定）→ 登出后 `/me` 401。
      已清理 smoke 数据（删 issue、归档 space，列表复空）。演示账号平面经 `npm run dev`（MSW）核对
      逻辑（mock auth handler + 双 tab）。
- [x] `main` 保持 `1c5b9b4`；未 push；未进入 Final Main Sync；工作树干净。

### 作用域矩阵（实施后确认）

| 资源 | 作用域 | 说明 |
| --- | --- | --- |
| Projects | **space 级**（真实） | `useSpaceProjects(tenantId, space.id)`；创建走 spaces 端点（202）；列表 owner 隔离（D1 —— 当前实现，migration pending）；`space_id` 保持可空（D2）。 |
| Runtime Workspaces | 项目派生 | 项目创建时随 `operation`/`workspace` 202 返回，未在 shell 直接展示。 |
| Members | **space 级**（真实） | 空间成员列表 + role/status 管理（admin/owner），owner 可加成员。 |
| Settings | **space 级**（真实） | 改名（版本守卫）+ owner 归档危险区（S3 owner-only）。 |
| Issues / Workflow | **tenant 级**（真实） | 保持 `useIssues(tenantId)`；同 tenant 内切换 Workspace 不改变 issue 列表（诚实）。**WORKSPACE SCOPING GAP**：后端 issues 无 `space_id`，前端不伪造、无大 schema 迁移。 |
| 收件箱/聊天/AI 团队等 | demo 平面（mock） | 云模式下 mock 页经 interceptor slug 重写展示演示数据；真实 Issues/空间等表面在 demo 模式隐藏（SD7）。 |

---

WORKSPACE INTEGRATION STAGE D: COMPLETE
