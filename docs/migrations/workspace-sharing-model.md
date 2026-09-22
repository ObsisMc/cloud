# Workspace Sharing Model — 对齐 Workspace 资源共享模型（决策 / 集成记录）

> 状态：**IMPLEMENTED**（Step 2B 对齐；用户 2026-09-21 批准 plan 后实施）
> 分支 `workspace合并`，起点 HEAD=`4b481f2`，实施完成后提交 `refactor: align workspace resource sharing model`。
> `main` 保持 `1c5b9b4` 未动（未 merge、未 push、未进入 Final Main Sync）。
> 关联 ADR：`specs/decisions/cloud/collaboration-workspace/20260921-workspace-sharing-model.md`（SS1–SS5）。
> 取代：[[workspace-membership]] 的 SD6 产品语义（旧 D1 owner 隔离由产品规则降级为当前实现）。
> **superseded-by（Step 3）**：Project Workspace Sharing 已实施为 **IMPLEMENTED**，见
> [[project-workspace-sharing]]（PS1/PS2 落地；SS1/SS2 公式保持不变，作为模型决策记录）。
> 结束标记：`WORKSPACE ADD MEMBER: PRESERVED` · `WORKSPACE RESOURCE SHARING MODEL: ALIGNED` ·
> `OLD D1 OWNER-ISOLATION PRODUCT RULE: SUPERSEDED` · `PER-RESOURCE MEMBERSHIP MODEL: NOT USED` ·
> `PROJECT CURRENT IMPLEMENTATION: OWNER-ONLY / EXISTING STATE` ·
> `PROJECT WORKSPACE SHARING: NOT IMPLEMENTED — NEXT STEP` ·
> `ISSUE WORKSPACE SCOPING: NOT IMPLEMENTED` ·
> `AGENT/TEAM/WORKFLOW/MCP/SKILL WORKSPACE SCOPING: NOT IMPLEMENTED` ·
> `DOCUMENTATION: SYNCED`
> ⚠️ **Step 3A rollout**（[[workspace-member-management]] MM3–MM7）：member 角色管理 + 成员移除已实现 ——
> `putSpaceMember` 收紧为 owner-only、owner 角色 immutable、新增 owner-only `DELETE /members/:uid` 硬删。
> 本模型的共享边界 / 统一删除规则 / 禁止逐资源 membership 不变。

---

## 0. 背景与动机

旧 Step 2（[[workspace-membership]]，Workspace Add Member）把「Workspace Member 不能访问他人
Project」当作**产品目标**（SD6「Workspace Membership ≠ Project Access」、owner 隔离「刻意保留 /
预期行为，非 bug」、状态行 `WORKSPACE MEMBER AUTO-PROJECT ACCESS: DISABLED BY DESIGN` /
`PROJECT OWNER ISOLATION: PASS`）。用户 2026-09-21 冻结了新的资源共享模型，取代这一产品语义：

> **Collaboration Workspace = resource sharing boundary**。Workspace Member 最终可访问该 Workspace
> 内共享的资源：Projects / Issues / Agents / Teams / Workflows / MCPs / Skills。
> **禁止**逐资源 membership（`project_members` / `issue_members` / `agent_members` / `team_members`
> 等一律不设计）。

**当前实现仍是 Project owner-only** —— 这是事实，只能描述为「当前实现，project workspace-sharing
migration pending」，**不再是**产品规则。本阶段（Step 2B）**只对齐**：

- 改写措辞/文档/测试标签：从「永久产品目标」→「当前实现 + migration pending」。
- 落地最小 Workspace 权限 foundation（boolean 谓词 + 统一删除规则），不接入真实资源授权。
- **不迁移实际资源**（不改 Project list/detail/runtime 访问、不改 Issue/Agent/Team/Workflow/MCP/
  Skill 作用域）；**不重新实现** Workspace Add Member。

## 1. 决策结论（来自已批准 ADR）

| 决策 | 选择 | 结论 |
| --- | --- | --- |
| SS1 — 共享边界 | **Workspace = resource sharing boundary** | 资源共享的原子单位是 Workspace（`collab_workspaces`），不是租户、不是单资源。资源归属某 Workspace 后（如 `projects.space_id`），共享范围 = 该 Workspace 的活跃成员。**不设计** `project_members` / `issue_members` / `agent_members` / `team_members`；未来 Project 权限直接来自 `projects.space_id` + Workspace Membership。 |
| SS2 — 删除规则 | **`CanDeleteWorkspaceResource(user, workspace, creator) = (user == creator) OR (user 是 owner/admin)`** | creator 永远可删自己创建的资源；workspace owner/admin 可删任意成员创建的资源；普通 member 不能删他人资源；非成员无权限。本阶段只落地可复用谓词，不接入真实资源的删除授权。 |
| SS3 — 旧 D1 superseded | **不改写历史，只加 superseded 记录** | 旧 D1「Space membership must NOT grant access to others' Projects / Runtime Workspaces」是 Stage B 产品决策，现被本 ADR 取代。相关文档/测试标签/状态行一律从「预期行为，非 bug / 刻意保留 / DISABLED BY DESIGN / PASS」改写为「CURRENT IMPLEMENTATION: owner-only — temporary until the project workspace-sharing migration (next step)」。 |
| SS4 — 权限 foundation | **3 个可复用谓词，无通用 RBAC engine** | `IsWorkspaceMember(userID, workspaceID)`（活跃成员）；`IsWorkspaceAdmin(userID, workspaceID)`（owner OR admin）；`CanDeleteWorkspaceResource(userID, workspaceID, creatorUserID)`（SS2 公式）。事务内以非 panic 谓词 `workspaceRole(t, spaceID, uid)`（返回 `""`/`owner`/`admin`/`member`）为基础，供未来 delete-authorization 在同一 `transact` 内复用；对外暴露为 **Store 方法**（集成测试直调 `f.store.*`）。 |
| SS5 — rollout 状态 | **诚实记录** | Workspace Membership：**IMPLEMENTED**（Step 2，旧能力原样保留）；**member 角色管理 / 移除：IMPLEMENTED（Step 3A）** —— 角色变更 owner-only、owner immutable、移除 owner-only 硬删。Project Workspace Scoping：**IMPLEMENTED（Step 3）**。Issue / Agent / Team / Workflow / MCP / Skill Workspace Scoping：**PENDING**（各自列出 NOT IMPLEMENTED / NOT YET MIGRATED）。 |

## 2. 实施内容

1. **权限 foundation**（新 `internal/core/space_permission.go`）：
   - `workspaceRole(t *transaction, spaceID, uid string) string` —— 复用 `spaceMember` 的 join SQL
     （活跃成员 + 活跃租户成员 + 未归档 space + 活跃 tenant），非 panic，返回 `""`/`owner`/`admin`/`member`。
   - Store 方法（均返回 `(bool, error)`，DB 错误 fail-closed 到 false）：
     `IsWorkspaceMember` / `IsWorkspaceAdmin` / `CanDeleteWorkspaceResource`。
   - **不改** `spaceMember` / `requireSpaceRole` / `putSpaceMember` / `enrollSpaceMemberByEmail`；
     **不改**任何资源访问控制；无新表/迁移。
2. **测试重标签**（断言全保留，仅当前状态化）：
   - `TestWorkspaceMembershipDoesNotGrantProjectAccess` → `TestProjectAccessOwnerOnlyUntilWorkspaceSharing`。
   - `TestWorkspaceMembershipDoesNotGrantRuntimeAccess` → `TestRuntimeWorkspaceOwnerOnlyUntilWorkspaceSharing`。
   - `project_space_test.go` / `space_events_test.go` 的 D1 注释 → current-state 措辞。
   - `internal/core`（space.go/public.go/store.go）与 `internal/contract/openapi.go` 描述中的
     `(D1)` 决策标签 → current-state 措辞；`go run ./cmd/openapi` 重生成 `api/openapi.json`（1 行）。
3. **文档对齐**：本文件 + `workspace-membership.md` 状态行改写 + `docs/INDEX.md` +
   `progress.md` + Stage B 根 D1 superseded 记录。

## 3. 验证

- **后端**：`go build ./...`、`go vet ./...` 干净；契约测试（OpenAPI 逐字节）通过（重生成仅 1 行）。
- **集成套件**（真实 PG，`integration/space_permission_test.go` 8+1 用例 + 改名后的既有回归）全 PASS。
- **前端**：本阶段无前端代码改动；全套 gates 复跑（既有 2 个 pre-existing failures 记录不修）。

## 4. 已知缺口（诚实记录，沿用 §SS5）

- **PROJECT WORKSPACE SHARING: IMPLEMENTED（Step 3）**：本缺口已被 [[project-workspace-sharing]] 关闭。
  Project list/detail/runtime Workspace 访问已切换为 workspace-shared（PS1–PS5）；删除规则
  （creator OR owner/admin）已接入真实资源（PS2）。**不建** `project_members`。
- **ISSUE WORKSPACE SCOPING: NOT IMPLEMENTED**：`issues.space_id` 未引入 —— NEXT STEP。
- **AGENT/TEAM/WORKFLOW/MCP/SKILL WORKSPACE SCOPING: NOT IMPLEMENTED**：各资源仍无 Workspace 共享。
- **删除新规则其余资源未接入**：`CanDeleteWorkspaceResource` 公式已在 Project 接入（PS2）；Issue /
  Agent / Team / Workflow / MCP / Skill 的删除授权随各自 Workspace Scoping 迁移进行。

---

> 关联测试镜像：`specs/test-cases/cloud/collaboration-workspace/20260921-workspace-sharing-model.md`
>（#1–#8 全部 Covered）。
