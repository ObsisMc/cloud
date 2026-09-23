# Workspace Integration Stage B — 决策已实施（ADR / 集成记录）

> 状态：**COMPLETE — 已实施**
> 与 [workspace-integration-stage-a.md](./workspace-integration-stage-a.md) 同级，属集成过程记录。
> 分支 `workspace合并` 的 merge（HEAD=`56385b3` + MERGE_HEAD=`aeea9fd`）已通过 merge commit 完成，`main` 保持 `1c5b9b4` 未动。
> 所有 CLASS D（设计）冲突已按下列决策解决，CLASS M/S 已完成。
> 结束标记：`WORKSPACE INTEGRATION STAGE B: COMPLETE`

> **⚠️ 迁移序列状态（2026-09-23 reconciliation 后）**：§2 描述的 `0011_collab_spaces` /
> `0012_project_space_scope`（Stage A Issues 前移重编号）已被**当前 upstream-ready 序列取代**：
> `0006_collab_spaces` / `0007_project_space_scope` 恢复 upstream 原样，Issue 迁移为 `0008–0012`，
> `0013_project_space_optional` 恢复 `projects.space_id` 可空（`0013` 不改动 `0007` 已绑定的项目）。
> D2=C（`space_id` 可空）产品语义不变；迁移载体改变。当前权威见 `docs/development/agent/database.md`。

---

## 1. 决策结论（CLASS D — 已决）

| 决策 | 选择 | 结论 |
| --- | --- | --- |
| D1 — Project 可见性边界 | **A** | 保留 `owner_user_id` 主边界。Space 成员资格**不得**授予对他人 Project / Runtime Workspace 的访问。`project()`、`workspace()`、project 列表、workspace 列表恢复 owner 授权；另加 Space 成员≠owner 的显式隔离测试。 |
| D2 — Space 是否强制 | **C** | 两阶段可选关系：`projects.space_id` 可空、不回填既有项目；保留同租户复合 FK `(space_id, tenant_id)` 与索引；租户级 `POST /tenants/:tid/projects` 不依赖 `defaultSpace()`。 |
| D3 — 术语 | **A** | Space=协作空间，Workspace=运行时工作区。UI 不把 Space 显示为 Workspace；内部表名（`collab_workspaces`）可保留。 |
| D4 — Issue 路由 | **a（维持已批准）** | Issues 保留 `/{tenantId}/...`；Space 用独立路由命名空间 `/spaces/...`，不包裹 `/:workspaceSlug/...`。 |
| D5 — 认证/会话 | **A** | ora-web cookie 会话是前端权威会话基础。前端不切 devgateway 双 JWT。排除 `cmd/devgateway/*`、`frontend/src/lib/cloud-session.ts`(+test)、`frontend/src/test/cloud-handlers.ts`，但不删除后端双 JWT 契约。 |
| D6 — 现有行为削弱 | **A** | 拒绝削弱既有隔离：未授权的 Project / Runtime Workspace 保持 404。新增 Space 隔离测试，不改既有 404 断言。 |
| D7 — default Space | **A** | 默认 Space（slug=`default`）不可归档/删除 → 409 `default_space_protected`，配套测试。 |

> **⚠️ 后续 superseded（2026-09-21，Step 2B — [workspace-sharing-model.md](./workspace-sharing-model.md)）**：
> 上表 D1「Space 成员资格**不得**授予对他人 Project / Runtime Workspace 的访问」曾作为**产品决策**
> 记录。该产品语义已被取代 —— **Workspace 现为资源共享边界**，member 最终可访问该 Workspace 的共享
> 资源；owner-only Project 访问只是**当前实现**（project workspace-sharing migration pending），不再
> 是产品规则。本记录保留为历史决策记录，不作修改。

---

## 2. 实施内容

- **迁移**：`0011_collab_spaces.sql`（保留 `version DEFAULT 1` 语义、成员表）、`0012_project_space_scope.sql`（default space 回填成员 + `projects.space_id` 可空 + 复合 FK + 索引）。Stage A Issues 的 `0006–0010` 未动。
- **后端**：`internal/core/space.go`（Space 领域：create/patch/archive/list/member upsert + last-owner 不变量）、`internal/core/hub.go`（SSE SpaceHub 扇出，publish-after-commit）、`internal/core/public.go`（owner 授权 + space 路由分派）、`internal/core/store.go`（`Events *SpaceHub` seam）。
- **路由**：`internal/api/router/router.go` 注册 `/spaces` 系列与 `/spaces/:spaceId/events` SSE（双 JWT 校验复用 REST 授权）。
- **契约**：`internal/contract/openapi.go`（authoritative）新增 Space/SpaceMember/SpaceEvent schema 与 `/spaces` tag；重新生成 `api/openapi.json` 与前端客户端。
- **前端（D4/Aux/D5=A）**：`frontend/src/features/spaces/**`（api / create-space-dialog / use-space-events / spaces-page / tests）、`routes.tsx`、`app-sidebar`、`paths.ts`；删 `cmd/devgateway/*`、`cloud-session.ts`(+test)、`cloud-handlers.ts`、`current-space.tsx` 等 devgateway/mock 残留。
- **集成测试**：`space_test.go`（生命周期/成员/owner 不变量 + D7 default 不可归档）、`space_events_test.go`（SSE 授权与 commit 顺序）、`project_space_test.go`（D1 owner 隔离 + D2 可选 space + last-owner 竞态）。

---

## 3. 验证

- `go build ./...` 通过；`go vet ./integration/` 通过。
- `go test -count=1 ./...`（REQUIRE_POSTGRES=1）全部通过，含 integration 25s。
- 前端 gates 通过：`typecheck` / `lint` / `build` / `check:modules` / `check:docs` / `check:dup` / `format:check`（`test`/`check:dead` 受 Node 20 vs engines>=24 环境阻断，见 Stage A 记录）。
- `main` 保持 `1c5b9b4`，未 push。

---

WORKSPACE INTEGRATION STAGE B: COMPLETE