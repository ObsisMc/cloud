# Workspace Integration Stage D — 同事 Login + Workspace UX 恢复（决策 / 集成记录）

> 状态：**决策已批准 — 实施中**（决策经 plan `distributed-wishing-ritchie` 用户批准）
> 与 Stage A/B/C 同级，属集成过程记录。分支 `workspace合并`，HEAD=`39815387`，`main` 保持 `1c5b9b4` 未动。
> 关联 ADR：`specs/decisions/cloud/collaboration-workspace/0-frontend-workspace-product-shell.md`（SD1–SD7）。
> 结束标记（实施完成后更新）：`WORKSPACE INTEGRATION STAGE D: COMPLETE`

---

## 0. 背景与动机

Stage B/C 合并了同事的 **Space 后端**（`collab_workspaces`、`/spaces` API、SSE、space 页面），但
**有意排除了同事的完整产品外壳**：Login、会话引导、Current Workspace provider/context、左上
Workspace 创建入口、左上 Workspace 选择器、Workspace 切换、Workspace 驱动的资源上下文。当时的
D4/D5 决策把登录与 Current Workspace shell 排除在外。复盘后确认这是**不完整的集成**：后端功能已合并，
同事的产品交互未合并。本轮恢复。

- **Reference tree**：`C:\Users\zsl\Desktop\cloud`（HEAD `302dc9f`）— 同事的完整代码副本，Login UX +
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
| SD5 — 项目作用域 | **真实 space 级** | 项目创建走 `POST /tenants/:tid/spaces/:spaceId/projects`（202 异步，立即关 dialog，失效 space 项目查询）；列表维持 owner 隔离（D1）；`projects.space_id` 保持可空（D2=C）。 |
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

---

## 3. 验证（实施完成后填写）

- [ ] `go build ./...`；`go test -count=1 ./...`（REQUIRE_POSTGRES=1）通过，space 测试保持绿。
- [ ] 前端 gates：`format:check`/`lint`/`typecheck`/`build`/`check:modules`/`check:docs`/`check:dup`
      （`test`/`check:dead` 受 Node 20 vs engines>=24 环境阻断，记录不重跑）。
- [ ] 新增测试：登录（成功/失败/会话恢复/登出/重定向）、选择器（列表/选中/切换/创建→自动选中）、
      Workspace 资源切换（A→B 查询消失、B 加载）、缓存无跨工作区泄漏、回归。
- [ ] Smoke path：build dist → `go run ./cmd/ora-web`（:8080，PG 起）→ 登录 → shell → 左上选择器 →
      创建工作区 → 出现 → 选中 → 在其中创建/列表项目 → 切换工作区 → 资源上下文变化 → 打开 Issues 仍可用 →
      刷新 → 会话 + 工作区恢复。演示账号平面经 `npm run dev`（MSW）核对。
- [ ] `main` 保持 `1c5b9b4`；不 push；不进入 Final Main Sync。

---

WORKSPACE INTEGRATION STAGE D: 决策已批准 — 实施中
