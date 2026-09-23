# Restore Pre-Merge Product Capabilities on Current Upstream-Compatible Architecture

> **LOCAL-ONLY DOC — DO NOT STAGE, DO NOT COMMIT, DO NOT INCLUDE IN ANY FUTURE PR.**
> Record of the restoration work kept in the working tree for review; it documents a
> set of working-tree changes that must not be committed as part of the upstream sync.

- Status: IMPLEMENTED (working tree only, no commits)
- Start HEAD: `9f0f86e1ad0eefc5a05722d9c13ee8862863e050` (branch `workspace合并`)
- Today: 2026-09-22
- Upstream merge under review: `eebcdc1`

## 背景

上游合并（`eebcdc1`）把 `cmd/ora-web`（唯一非测试协作端口接线入口）删除，并把本地无
邮箱登录路径的 demo 桥迁成新的 `cmd/demo-issue-board-web`，导致三个产品能力回归：

1. **Workspace 无法通过邮箱添加用户** —— add-by-email 依赖 `user_identities` 里存在
   `(source, email)` 身份；本地只有 `source=demo` 的固定演示用户，任何陌生邮箱都解析失败
   返回 404 `user_not_registered`。
2. **Issue @ 选择器目标消失** —— 合并后 `store.Directory` 等协作端口保持 nil，目标列表
   只剩 humans（`collaborationTargetList` 在 `t.directory == nil` 时只返回租户成员）。
3. **Workflow 表单交互消失** —— `store.Forms`/`store.Assist` 为 nil，表单/辅助接口 503，
   InteractionDescriptor → FormDescriptor → form → Assist → Review → Confirm → Interaction
   → Run 链路不可用。

## 审计（Restoration Audit）

- 后端协作/issue 全部代码（`internal/collab` fixture、`collaboration_targets.go`、
  `form_descriptor.go`、space.go add-by-email、前端 picker/form 页面）在上游合并中**保持
  完整**，回归纯由“本地接线入口被删 + 端口默认 nil”造成。
- add-by-email（`enrollSpaceMemberByEmail`，space.go）与核心 `identity()`
  （store.go）均为通用能力，不需要改造；缺的只是一个在本地产生 email-subject 身份的受信
  路径。
- 结论：**恢复能力，不盲目恢复旧架构**。在现有上游架构上加一个小而隔离的适配器 +
  fixture 接线，不动核心、不加第二套生产认证。

## 决策（ADR 摘要）

1. **Temporary Development Email Auth（DEV ONLY）适配器** `internal/gateway/devemail`：
   - 注册走核心 `identity()` JIT 路径自建账号（邮箱归一化小写、重复注册 409、不自动建
     Workspace）；登录未知邮箱 401，绝不自动创建；会话 cookie `ora_dev_session` 只在桥内
     解析为 `source=dev-email` 身份，再签发双 JWT 注入。
   - 不存密码、不做 OAuth/JWT 平台、不回收凭据、不合并账号；生产入口仍是 `cmd/gateway`
     （IDaaS）。`DEMO_DEV_AUTH=0` → 注册/登录 404 `dev_auth_disabled`，fail-closed，无
     自动回退。
2. **协作端口接线恢复**：`cmd/demo-issue-board-web` 在 `store.Migrate` 后挂载
   `FixtureCollaborationDirectory` / `DeterministicContextBuilder` /
   `MockExecutionDispatcher` / `FixtureFormDescriptorProvider` / `MockInputAssistProvider`，
   等价于被删的 `cmd/ora-web` 的接线；生产服务器端口保持 nil（Unavailable）接真实适配器。
3. **add-by-email 零后端改动**：复用 `enrollSpaceMemberByEmail`，集成测试经 dev 会话做
   E2E 证明。

## 改动清单（全部留在工作区，未 commit）

| 文件 | 状态 | 内容 |
|---|---|---|
| `internal/gateway/devemail/devemail.go` | 新增 | Dev Email Auth 适配器 |
| `cmd/demo-issue-board-web/main.go` | 修改 | 协作 fixture 接线 + dev auth 挂载 + session-aware 双 JWT 注入 |
| `integration/dev_email_auth_test.go` | 新增 | 8 个真实 PG 集成测试 |
| `frontend/src/features/auth/api.ts` | 修改 | `useDevRegister`/`useDevLogin`/`refetchCurrentUser` |
| `frontend/src/features/auth/login-page.tsx` | 修改 | 登录页三平面 Tabs（真实/开发/演示）+ `DevEmailPanel` |
| `docs/authentication.md` | 修改（tracked） | DEV ONLY 临时邮箱认证一节 |
| `docs/gateway.md` | 修改（tracked） | 本地演示桥（非生产网关）一节 |

## 验证

- 后端：`go build ./... && go vet ./...` 通过；`go test ./internal/...` 通过。
- 集成（真实 PG）：`(cd integration && REQUIRE_POSTGRES=1 TEST_DATABASE_URL=… ../.local/gotmp/integration.test.exe)` → **80 PASS / 0 FAIL**（含 8 个新 dev email 测试）。
- 前端门禁：format:check / lint / typecheck / test / build / check:modules / check:docs /
  check:dead (knip) / check:dup (jscpd) 全部通过；仅 1 个既有 baseline 失败保持不变
  （`src/mocks/handlers/handlers.test.ts` 未知 slug 404-vs-200，非本次引入）。
- 覆盖回归 1–3：
  - 1：`TestDevEmailMultiUserAddByEmail` —— Alice 注册建 Workspace，`BOB@example.test`
    （大小写变体）被添加，Bob 用自己的 dev 会话登录后可见共享 Workspace。
  - 2：`TestDevEmailSessionCollaborationTargetsAndForms` —— @ 目录含 user/agent/team/workflow。
  - 3：同测试 —— `security-review` 表单描述符含 fields，可解析。

## 上游冲突风险（O）

- LOW。新增/修改均落在合并后已存在或新引入的局部文件；未改核心、未改
  `internal/core/migrations/*`、未改 router allowlist、未改契约（OpenAPI 无变更）。
- 冲突点仅可能出现在未来再次触碰 `cmd/demo-issue-board-web/main.go` 或两个前端文件时，
  规模小、语义独立。

## 已知边界

- 演示桥会话仅内存态；重启 demo 后需重新注册/登录。
- devemail 适配器 DEV ONLY：任何把 `/auth/dev/*` 暴露到生产网关的行为都不被支持。
- 演示桥仍以 alice 为无会话回退身份（post-clone 预览保留）；注销后回落 alice。
- 前端测试 baseline `handlers.test.ts` 404-vs-200 保持未动（任务约束）。
