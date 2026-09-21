# Workspace Add Member — 用 email 添加已注册用户到协作空间（决策 / 集成记录）

> 状态：**IMPLEMENTED**（决策经用户批准后实施完成）
> 分支 `workspace合并`，起点 HEAD=`662e211`，实施完成后提交 `feat: add workspace member enrollment`。
> `main` 保持 `1c5b9b4` 未动（未 merge、未 push、未进入 Final Main Sync）。
> 关联 ADR：`specs/decisions/cloud/collaboration-workspace/20260921-workspace-add-member.md`（SD1–SD6）。
> 结束标记：`WORKSPACE ADD MEMBER: COMPLETE` · `NEW MEMBER WORKSPACE VISIBILITY: COMPLETE` ·
> `WORKSPACE MEMBER AUTO-PROJECT ACCESS: DISABLED BY DESIGN` · `PROJECT OWNER ISOLATION: PASS` ·
> `PROJECT SHARING: NOT IMPLEMENTED — NEXT STEP` · `EMAIL INVITATION: NOT IMPLEMENTED` ·
> `DOCUMENTATION: SYNCED`

---

## 0. 背景与动机

Step 1（User Registration）之后，注册用户只能进入 bootstrap 租户、看到默认 Workspace，**无法被加入
别人的 Workspace**。本次实现 **Workspace Add Member**：Workspace 管理界面 → 添加成员 → 输入 email →
建立 Workspace Membership → 成员列表立即出现该用户 → 该用户 refresh/re-enter 后能在 Workspace
selector 看到并切换到该 Workspace。

**明确不做**：Project Sharing、Email Invitation、角色管理（新成员固定 `member`，request 不接受
owner/admin）、租户成员 enrollment UI、全局用户目录（`GET /users?q=email`）。**不修改** User
Registration；**不新建**第二套 membership 系统 —— 复用 Stage B 的 `collab_workspace_members`。
**不引入** `issues.space_id`；不改 runtime `workspaces` 授权。

## 1. 决策结论（来自已批准 ADR）

| 决策 | 选择 | 结论 |
| --- | --- | --- |
| SD1 — API | **`POST /api/v1/tenants/:tid/spaces/:spaceId/members`（body `{email}`）** | `enrollSpaceMemberByEmail`：actor 需 space owner/admin（member → 403 `space_role_required`）；邮箱 normalize + `invalid_email`(400)；严格 `users JOIN user_identities`（source=`r.Identity.Source`，subject=normalized email）查找——不存在 / 非 active → **404 `user_not_registered`**（无创建、无邀请、无 pending member）。 |
| SD2 — 原子性 | **同一 `transact` 双写** | 先 `tenant_memberships INSERT … role='member',status='active' ON CONFLICT DO NOTHING`（已存在则保留原角色），再 workspace membership；无半状态、无外部副作用。模型足以支撑安全跨租户 enrollment，**无 ARCHITECTURAL GAP**。 |
| SD3 — 角色 | **新成员固定 `member`** | request 不接受 owner/admin；已存在 → **幂等返回 existing（200，不改 role/status/version）**；不改 `putSpaceMember`（角色/状态管理保留）。 |
| SD4 — 分发与路由 | **`public.go` POST case + router allowlist** | POST 自动继承全局 Idempotency-Key 约定；OpenAPI `description()` 的 `/members` 分支区分 POST；`go run ./cmd/openapi` 重生成 `api/openapi.json`。 |
| SD5 — 前端 | **email Dialog 替换 userId 内联表单** | `useAddSpaceMemberByEmail`（POST + `mutationHeaders` + members key invalidation）；`AddMemberDialog`（「添加成员」按钮仅 `canManage` 显示；本地校验；成功关 dialog + 列表刷新；`user_not_registered` → 「该邮箱尚未注册，请先完成注册。」）。 |
| SD6 — 不变量 | **Workspace Membership ≠ Project Access** | owner 隔离保留：B 可见/可切换 W，但看不到/打不开 A 的 Project P（预期行为，非 bug）；不改 runtime `workspaces` 授权；Selector 靠内存缓存 refresh 反射新可见 Workspace，不重写 Current Workspace provider。 |

## 2. 实施内容

1. **后端核心**（`internal/core/space.go`）：新增 `enrollSpaceMemberByEmail(t, r, uid)` ——
   `spaceMember` + `requireSpaceRole(actor, "admin", "owner")` → 邮箱 `normalizeEmail` + `validEmail`
   门 → 严格 `users JOIN user_identities` 查找（不存在 / 非 active → 404 `user_not_registered`）→
   原子双写（`tenant_memberships` ON CONFLICT DO NOTHING + `collab_workspace_members` 不存在才 INSERT，
   固定 `member`/`active`/`created_by=actor`；已存在则幂等返回）→ 返回 member 行。
2. **分发与路由**：`internal/core/public.go` `Public` switch 在 PUT case 之后加
   `r.SpaceID != "" && r.UserID == "" && strings.HasSuffix(r.Path,"/members") && r.Method == "POST"`
   case（emit `space.member_updated`）；`internal/api/router/router.go` allowlist 加
   `{"POST", "/api/v1/tenants/:tid/spaces/:spaceId/members", "", []string{"email"}}`。
3. **OpenAPI / 契约**：`internal/contract/openapi.go` `description()` 的 `/members` 分支区分 POST
   （enrollment 语义：email、固定 member、404 `user_not_registered`、原子 ensure tenant membership、
   幂等返回 existing、admin/owner 才能添加）；`go run ./cmd/openapi` 重生成 `api/openapi.json`
   （纯新增 POST operation block，无意外 churn）。
4. **前端**（`frontend/src/features/members/`）：`api.ts` 新增 `AddMemberByEmailInput` +
   `useAddSpaceMemberByEmail(tenantId, spaceId)`（`AXIOS_INSTANCE.post` + `mutationHeaders` +
   members key invalidation）；`members-page.tsx` 用 `AddMemberDialog`（email 字段 + 本地/服务端提示）
   替换 `AddMemberForm`（userId + role select），触发器「添加成员」仅 `canManage = owner|admin` 显示；
   `CloudMembersView` / `CloudMemberRow` 结构保留不动。
5. **测试**（见 §3）。

## 3. 验证

- **后端**：`go build ./...`、`go vet ./...` 干净；契约测试（OpenAPI 逐字节）通过；
  集成套件（真实 PG，`integration/space_member_enrollment_test.go` 8 用例 + 既有 space 回归）全 PASS：
  已注册用户按 email 入队（大小写变体归一）/ 未知邮箱 404 无脏行 / 重复添加幂等（existing 不改
  role/status/version，owner 不被降级）/ 角色门（owner/admin 200、member 403）/ 非租户成员原子
  双写 / 跨租户无存在泄漏（403 `membership_required` 非 404）/ **Workspace Membership 不授予
  Project Access**（B 的 `/spaces/:sid/projects` 空、`/projects/:pid` 404）/ **不授予 Runtime
  Access**（B 的 `/workspaces/:wid` 404）。全量 74 PASS / 0 FAIL。
- **前端**（Node 24）：`format:check` / `lint` / `typecheck` / `build` / `check:modules` /
  `check:docs` / `check:dead` / `check:dup` 全绿（check:dup 0 clones）；members 测试全通过：
  member 只读无「添加成员」按钮 / owner 按 email 添加（POST body=`{email}` + 非空 Idempotency-Key +
  dialog 关闭）/ email 本地校验（空 / 非法提示）/ 未知邮箱 `user_not_registered` 提示 / 重复添加
  幂等关 dialog / **selector 可见性**（B refresh/re-enter 后新 QueryClient 重取 `/spaces`，selector
  出现新 Workspace 并可切换）。全量 `test` 套件 117 通过、2 失败——**均为分支既有失败**
  （`app-sidebar.test.tsx` 与 `handlers.test.ts`，与 members 无关，未触碰）。
- **Smoke（真实 HTTP）**：A 注册/进入 → 建 Workspace W → 建 Project P；B 注册/进入 → B 看不到 W；
  A 在 W → Members → Add Member（B email）→ success，member list 出现 B；B refresh/re-enter →
  selector 出现 W → 切 W → shell 正常；**关键**：B 看不到/打不开 A 的 Project P，B 经
  `/workspaces/:wid` 也无法绕过 project isolation；未知邮箱 → `user_not_registered` + 无脏
  tenant/workspace membership。

## 4. 已知缺口（诚实记录）

- **PROJECT SHARING NOT IMPLEMENTED — NEXT STEP**：Workspace 成员目前**不会**自动获得空间内其他
  owner 的 Project 访问权（D1 owner 隔离，刻意保留）；项目级分享/授权不在本期。
- **EMAIL INVITATION NOT IMPLEMENTED**：只接受**已注册**用户（未知邮箱 → 404 `user_not_registered`），
  无邀请邮件 / pending member / 自动建号。
- **WORKSPACE MEMBER AUTO-PROJECT ACCESS: DISABLED BY DESIGN**：空间成员身份不派生任何 Project
  visibility（见 §0/SD6）。
- **角色管理不在本期**：新成员固定 `member`，添加时不能指定 owner/admin；角色调整仍走既有
  `PUT /members/:uid`。

---

> 关联测试镜像：`specs/test-cases/cloud/collaboration-workspace/20260921-workspace-add-member.md`
>（#1–#15 全部 Covered）。
