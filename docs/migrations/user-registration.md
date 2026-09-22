# User Registration — 创建 User Identity（决策 / 集成记录）

> 状态：**IMPLEMENTED**（决策经用户批准后实施完成）
> 分支 `workspace合并`，起点 HEAD=`b54dc32`，实施完成后提交 `feat: add user registration`。
> `main` 保持 `1c5b9b4` 未动（未 merge、未 push、未进入 Final Main Sync）。
> 关联 ADR：`specs/decisions/cloud/identity-access/0-user-registration.md`（SD1–SD5）。
> 结束标记：`USER REGISTRATION: COMPLETE` · `AUTHENTICATION: NOT FULLY IMPLEMENTED` ·
> `WORKSPACE ADD MEMBER: NOT IMPLEMENTED — NEXT STEP` · `PROJECT SHARING: NOT IMPLEMENTED`
> ⚠️ **后续步骤已关闭部分缺口**：Workspace Add Member（Step 2）、Project Workspace Sharing（Step 3）、
> **注册后可选创建 Workspace（Step 3A，[[workspace-member-management]] MM1/MM2）** 均已实现 —— 新注册
> 用户 0 Workspace 是合法状态，前端显示 onboarding（「创建工作区」或等待被添加），不再只是死胡同。
> AUTHENTICATION 措辞保持原样（NOT FULLY IMPLEMENTED）。

---

## 0. 背景与动机

Stage D 恢复了同事的**登录页 + 会话**：登录页真实账号 tab 用 email 走 `cmd/ora-web` cookie 会话，
`EnsureMember` 幂等地把身份放进 bootstrap 租户。但它**只能登录已有身份**——没有「注册」入口。
本次实现 **User Registration = 创建 User Identity**（姓名 + 邮箱 → 新用户直接进入既有 current-user
流程），**不是**密码认证系统。

**明确不做**：Workspace 添加成员、Project 分享、邮件邀请、扩展 Auth/RBAC、`workspace_members`、
租户成员 enrollment UI、Add Employee。也**不引入** `password` / `password_hash` / `credential` /
bcrypt / JWT 认证 / OAuth / SSO。

## 1. 决策结论（来自已批准 ADR）

| 决策 | 选择 | 结论 |
| --- | --- | --- |
| SD1 — API 归属 | **ora-web 边界** | 注册与登录同居 `cmd/ora-web`（浏览器会话边界），`POST /auth/register`；不改公开 API / OpenAPI 契约。 |
| SD2 — Store 语义 | **严格创建** | `Store.RegisterIdentity` 只在邮箱未注册时创建；重复 → 稳定 `409 user_already_exists`；复用既有 `users` / `user_identities` / `tenant_memberships` 三表，无新迁移。 |
| SD3 — 邮箱规范化与唯一性 | **trim + lowercase** | `normalizeEmail` = trim + lowercase；大小写变体是同一身份（`Alice@Example.com` == `alice@example.com`），唯一性由 `user_identities(PK(source,subject))` 兜底；`invalid_email`(400) / `name_required`(400)。 |
| SD4 — 注册后行为 | **直接进入流程** | 注册成功即设置 `ora_subject` 会话 cookie 并返回与登录相同的 `{user, tenantId, tenantName}`，前端 `setSession` + 跳 `/default/projects`。 |
| SD5 — 前端入口 | **既有 Login 页加注册模式** | 不动 Stage D 恢复的登录样式；真实账号 tab 内加「注册」切换，表单 = 姓名 + 邮箱；`409` → 「该邮箱已经注册。」 |

## 2. 实施内容

1. **Store**（`internal/core/store.go`）：`validEmail`（轻量格式门）与 `normalizeEmail`；新增
   `RegisterIdentity(ctx, tid, source, subject, name)`：校验 → 查重（`user_identities` PK）→ 建
   `users(active)` + `user_identities` + `tenant_memberships(member, active)` → 返回用户行。在
   全局 advisory lock 事务内执行。重复邮箱在 `user_identities` 层返回 `409 user_already_exists`。
2. **Edge handler**（`cmd/ora-web/main.go`）：`POST /auth/register` 收 `{name, email}`；邮箱经
   `normalizeEmail`（**无** login 的空值回落 `demo` 逻辑——空邮箱必须 400）；错误按
   `core.ErrorCode` 写 `{code, params, requestId}`；成功设置会话 cookie 并返回登录同构响应。
3. **前端**（`frontend/src/features/auth/`）：`api.ts` 新增 `useRegister`（POST `/auth/register`，
   onSuccess `setSession`，与 `useLogin` 同构）；`login-page.tsx` 的 `CloudSignInForm` 拆
   `login/register` 两模式：`CloudLoginForm`（既有登录 + 「没有账号？ 注册」切换）与
   `CloudRegisterForm`（姓名 + 邮箱 + 本地可见校验「请输入姓名。」/「请输入有效的邮箱地址。」 +
   后端 `409` →「该邮箱已经注册。」；提交成功 → `/default/projects`）。注册表单 `noValidate`
   使自定义校验接管（不被浏览器原生约束吞掉）。
4. **测试**（见 §3）。

## 3. 验证

- **后端**：`go build ./...`、`go vet ./...` 干净；`go test ./internal/... ./cmd/...` 全绿；
  集成套件（真实 PG，`integration/user_registration_test.go` 6 个用例）全 PASS：
  注册成功进入租户 / 重复精确邮箱 409 / 重复大小写变体 409 / 非法邮箱 400 / 空姓名 400 /
  注册身份经既有登录路径（`EnsureMember`→`identity`）解析回同一 user。
- **前端**（Node 24）：`format:check` / `lint` / `typecheck` / `build` / `check:modules` /
  `check:docs` / `check:dead` / `check:dup` 全绿；auth 测试 11/11 通过（含 5 个注册用例：
  进入注册模式 / 校验错误可见 / 重复邮箱错误可见 / 注册成功存储会话并跳转 / loading 防重复提交）。
  全量 `test` 套件 113 通过、2 失败——**均为分支既有失败**（`app-sidebar.test.tsx` 与
  `handlers.test.ts`，不 import `features/auth`，已用 stash 移除本次改动复跑确认），与本次无关。
- **Smoke（真实 HTTP）**：注册新用户 → 200 + 会话 cookie；重复精确/大小写变体 → 409；登录路径解析回
  同一 user；注册 cookie 访问受保护 API（`/api/v1/tenants/:tid/spaces`）→ 200（刷新保持会话），
  无 cookie → 401；非法邮箱 400 `invalid_email`；空姓名 400 `name_required`。

## 4. 已知缺口（诚实记录）

- **AUTHENTICATION NOT FULLY IMPLEMENTED**：注册 = 创建 User Identity + 会话，**无密码**
  （`/auth/login` 仍是「任意 email 即登录」）。密码 / 凭据 / OAuth / SSO 均未实现。
- **WORKSPACE ADD MEMBER：已由后续步骤关闭**（当时未实现）：注册用户自动成为 bootstrap 租户普通成员，
  「向 Workspace 添加成员」已在 Step 2（[[workspace-membership]]）实现。
- **PROJECT SHARING：已由后续步骤关闭**（当时未实现）：Project 已在 Step 3（[[project-workspace-sharing]]）
  按 Workspace 共享。
- **可选创建 Workspace：已由 Step 3A 关闭**（[[workspace-member-management]] MM1/MM2）：注册后 0 Workspace
  是合法状态，前端 onboarding 提供「创建工作区」（复用 `POST /spaces`，creator 自动成为 owner）或等待
  被添加；新注册用户不自动加入 default space。
- 注册用户跨租户可见性、通知、邮箱验证邮件均不在本期范围。

---

> 关联测试镜像：`specs/test-cases/cloud/identity-access/0-user-registration.md`（#1–#11 全部 Covered）。
