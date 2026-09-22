# Workspace Member Management & Onboarding — 收口 Workspace foundation（决策 / 集成记录）

> 状态：**IMPLEMENTED**（Step 3A；用户 2026-09-22 批准 plan `kind-juggling-crown`；角色权限经
> AskUserQuestion 决策为「严格 §10+§13」后实施）
> 分支 `workspace合并`，起点 HEAD=`23fc3d7`，实施完成后提交 `feat: add workspace member management` +
> `feat: add workspace onboarding and member UI` + `docs: record workspace member management (Step 3A)`。
> `main` 保持 `1c5b9b4` 未动（未 merge、未 push、未进入 Final Main Sync）。
> 关联 ADR：`specs/decisions/cloud/collaboration-workspace/20260922-workspace-member-management.md`（MM1–MM9）。
> 基础：[[project-workspace-sharing]]（Step 3，Project 共享已落地；本阶段修前端 delete UI gap）、
> [[workspace-sharing-model]]（Step 2B）、[[workspace-membership]]（Add Member 保留）。
> 结束标记：`WORKSPACE ONBOARDING: COMPLETE` · `ZERO-WORKSPACE STATE: LEGAL` ·
> `WORKSPACE MEMBER ROLE MANAGEMENT: OWNER-ONLY` · `OWNER ROLE IMMUTABLE: PASS` ·
> `WORKSPACE MEMBER REMOVAL: OWNER-ONLY` · `REMOVAL IS MEMBERSHIP-ONLY: PASS` ·
> `PROJECT DELETE UI: CREATOR-OR-OWNER/ADMIN` · `PROJECT WORKSPACE SHARING: UNCHANGED` ·
> `ISSUE WORKSPACE SCOPING: NOT IMPLEMENTED — NEXT STEP` · `DOCUMENTATION: SYNCED`

---

## 0. 背景与动机

Step 3（[[project-workspace-sharing]]）完成 Project Workspace Sharing 后，Workspace foundation 仍
有三个收口缺口：

- **(A) 注册用户 0 Workspace 是死胡同**：注册后前端只有「退出登录」空态，没有「创建工作区」入口。
  但 Workspace 创建必须是**可选**的——有些用户注册只为接受邀请加入已有 Workspace，0 Workspace 是
  合法状态。
- **(B) 成员管理不完整**：只有 Add Member（email 添加）。缺 owner 的 Change role 权限收紧与 Remove
  member；且既有 `putSpaceMember` 允许 **admin 改角色、owner 授予/降级 owner**（有测试），与本阶段
  最小能力矩阵冲突。
- **(C) Step 3 已知 UI gap**：前端 `canDeleteProject = admin||owner` 比后端
  `creator OR owner/admin` 更严（member 创建者在 UI 看不到自己的删除按钮，API 允许）。

本阶段收口这三个缺口，之后才进入 Issue Workspace Scoping。**不迁移** Issue / Agent / Team / Workflow /
MCP / Skill；**不引入** password/credential/JWT/OAuth；不改注册系统本身。

## 1. 决策结论（来自已批准 ADR；角色权限按用户决策「严格 §10+§13」）

| 决策 | 选择 | 结论 |
| --- | --- | --- |
| MM1 — Workspace 创建可选 | **复用现有 `POST /spaces`** | 注册成功 → onboarding 状态；用户可选「创建工作区」或等待被添加。不强制自动建 W，不建第二套 creation backend；creator 同一 tx 内成为 `('owner','active')`（复用 `createSpace`）。 |
| MM2 — 0-Workspace 是合法状态 | **onboarding/empty state，非错误** | registered user + tenant member + 0 workspace membership 合法非错；登录后不 crash / 白屏 / redirect loop / fake demo workspace；显示「创建工作区」+「请工作区所有者通过邮箱添加你」。新注册用户**不**自动加入 default space（0012 seed 只对迁移时既有活跃成员生效）。 |
| MM3 — 角色变更 = owner-only | **`putSpaceMember` actor 收紧为 owner** | 原 `admin\|owner` → **owner**。Admin 保留 Add Member，不扩大为改角色/删成员。 |
| MM4 — owner 角色 immutable | **任何所有权转移 → 409 `ownership_transfer_not_supported`** | `role→owner`（member→owner、admin→owner）或对 owner 行的写（owner→admin、owner→member、owner 行 body 注入）一律 409。last-owner 409 `space_last_owner` 守卫随 owner-immutable 成为死代码并删除（错误契约同步）。所有权转移单独设计，本阶段不做。 |
| MM5 — Remove member = owner-only 硬删 | **新 `DELETE /spaces/:sid/members/:uid`** | actor 必须 owner；目标须同租户活跃成员（404 无存在性泄漏）；目标为 owner（含 self）→ **409 `cannot_remove_workspace_owner`**；`version` 乐观锁前置 + Idempotency-Key。硬删 `collab_workspace_members` 行（该表无 deleted_at）。**不级联删资源**：`projects.owner_user_id` 保留历史 creator 身份，W 内资源继续存在、其余成员仍可访问。 |
| MM6 — 移除后访问自然撤销 | **无需额外 revocation** | `listSpaces` / `spaceMember` / `workspaceRole` 均基于活跃 membership 行；成员行删除后 W / P / Runtime Workspace 访问自动隐藏。被移除的 creator 也不能利用 `owner_user_id` 绕过 Workspace 成员资格重新访问/删除 P（scoped resource 授权先满足活跃成员资格）。 |
| MM7 — 能力矩阵（§13 最小目标） | **Owner {Add ✓, Change role ✓, Remove ✓}** | Admin {Add ✓（保留）, Change role ✕, Remove ✕}；Member {Add ✕, Change role ✕, Remove ✕}。前端 UI 与后端独立强制一致。 |
| MM8 — Project delete UI 对齐 | **`canDeleteProject = role∈{owner,admin} OR currentUser == project.ownerUserId`** | member+creator 显示；member+非creator 隐藏；admin/owner 非creator 显示。**不改后端** Project delete 授权（Step 3 模型 UNCHANGED）。 |
| MM9 — 认证措辞保持准确 | **business-level authorization foundation** | User identity / Registration IMPLEMENTED；dev/session 认证 partial；production-grade 认证 NOT implemented；本阶段不写成 full RBAC。 |

## 2. 实施内容

**后端**：

- `internal/core/space.go`：
  - `putSpaceMember` 收紧：actor `requireSpaceRole(actor, "owner")`（原 `"admin","owner"`）；在 role/status
    校验之后、UPDATE/INSERT 之前加 owner-immutable 门 —— 任何 `role == "owner"` 或 target 已是 owner
    （`old.S("role") == "owner"`）→ `409 ownership_transfer_not_supported`；删除已死的
    `space_last_owner` 分支。保留 role/status 校验（`invalid_member`）、version 乐观锁、target 活跃
    tenant member 校验（404）、archived space 校验。
  - 新增 `removeSpaceMember(t, r, uid)`：actor 必须 owner（§13 admin Remove ✕）；target 同租户活跃成员
    校验（404 无存在性泄漏）；目标为 owner → `409 cannot_remove_workspace_owner`（§12，含 owner 自移除）；
    `version(old, ...)` 前置；`DELETE FROM collab_workspace_members` 硬删；返回被移除 membership 快照。
- `internal/core/public.go`：dispatch 在 archive-DELETE 分支**之前**加
  `r.SpaceID != "" && r.UserID != "" && r.Method == "DELETE"` case → `removeSpaceMember` + emit
  `space.member_updated`。DELETE 自动继承全局 Idempotency-Key 约定（`idempotent := POST||DELETE`）；
  该路径 `isAdmin = false` → `membership(t, tid, uid, false)` 要求 actor 活跃 tenant 成员（跨 tenant 无
  存在性泄漏）。
- `internal/api/router/router.go`：allowlist 新增
  `DELETE /api/v1/tenants/:tid/spaces/:spaceId/members/:uid`（fields: `version`）。
- 契约：`internal/contract/openapi.go` `description()` 拆分 members GET（列表，成员可见）/ POST（add，
  admin|owner，保留）/ PUT（**owner-only** 角色变更 admin↔member；owner 转移 → 409
  `ownership_transfer_not_supported`；说明 last-owner 不变量由 owner-immutable 保证）/ DELETE（**owner-only**
  移除；不可移除 owner → 409 `cannot_remove_workspace_owner`；version + Idempotency-Key）；`go run
  ./cmd/openapi` 重生成 `api/openapi.json`（字节比对测试通过）。不加纯授权新端点。

**前端**：

- `frontend/src/components/layout/dashboard-layout.tsx`：`EmptyWorkspaceState` 升级为 onboarding ——
  h1「你还没有加入任何工作区」+ 说明「创建一个新的工作区，或请工作区所有者通过邮箱把你添加进来。」+
  主 CTA「创建工作区」（复用 `CreateSpaceDialog`，onCreated → `navigate(\`/${slug}/projects\`)`）+
  保留「退出登录」。0 工作区 = 合法状态，无 fake workspace、无死循环。
- `frontend/src/features/members/`：
  - `api.ts` 新增 `useRemoveSpaceMember`（DELETE `/spaces/:sid/members/:uid` + `mutationHeaders` +
    members key + spaces key 双 invalidate）。
  - `members-page.tsx`：`CloudMembersView` — `canAdd = admin||owner`（Add 保留）、`isOwner = owner`（角色
    Select + 移除 + 操作列）；`CloudMemberRow` — 角色 Select 仅 `isOwner && !ownerRow` 显示，选项固定
    `GRANTABLE_ROLES = ['admin','member']`（owner 不可授予）；owner 行只渲染「所有者」标签、无任何管理
    操作；新增 `RemoveMemberDialog`（AlertDialog 确认，标题「从工作区移除「{name}」？」，文案明确
    **membership-only**：「该成员将无法再访问此工作区及其项目。其创建的资源会保留在工作区中，账号与租户
    成员关系不受影响。」）。
- `frontend/src/features/projects/project-detail-page.tsx`：`canDeleteProject(role, currentUserId,
  projectOwnerId)` = `role==='owner' || role==='admin' || (currentUserId != null &&
  currentUserId === projectOwnerId)`；currentUserId 取 `useAuthStore((s) => s.user)?.id`，
  projectOwnerId = `project.leadId`（= ownerUserId）。为控制函数复杂度将页头 + 重命名对话框抽取为
  `ProjectChrome` 组件。

**测试**（见 §3）。

## 3. 验证

- **后端**：`go build ./...`、`go vet ./...` 干净；契约测试（OpenAPI 逐字节）通过；
  `go test ./internal/...` 全绿。
- **集成套件**（真实 PG，`integration/workspace_member_management_test.go` 8 用例 + 既有回归）全 PASS：
  `TestRegistrationZeroWorkspaceState`（§24A，0 W 合法、无 fake W）、`TestRegistrationCreateOwnWorkspace`
  （§24B，注册建 W = owner、可建 P）、`TestRegistrationJoinExistingWorkspace`（§24C，注册不建 W 被添加后
  见 W）、`TestOwnerMemberManagement`（§25，promote/demote/remove 200）、
  `TestRemovedMemberAccessRevokedAndResourcesRemain`（§17/§23/§25，移除后 W/P/Runtime 404、P 仍在 W 且
  A 可删、B 无法用 creator 身份绕过）、`TestMemberManagementAuthorization`（§26，member/admin 改角色/移除
  403、admin add 200）、`TestOwnerProtection`（§27，owner 自降 409 `ownership_transfer_not_supported`、
  owner 自移除 409 `cannot_remove_workspace_owner`、member/admin 提 owner 403、恰一 owner 不变）、
  `TestRemoveMemberCrossTenantNoLeak`（§16，跨 tenant 403 `membership_required`）。
- **迁移的既有测试**：`TestSpaceLifecycleMembershipAndOwnerInvariants` owner 授予/降级场景改写为
  owner-immutable 断言（admin 改 owner 行 403 / owner 自降 409 / owner 授予 409）；`TestSpaceMemberLastOwnerRace`
  改写为并发 owner 转移全被拒、仍恰一 owner；`TestEnrollAlreadyMemberIdempotent` 尾部改为 owner-only 语义
  （admin2 提升为 admin + 重复添加不改角色）。
- **前端**（Node 24）：`format:check` / `lint`（含 oxlint complexity）/ `typecheck` / `build` /
  `check:modules` / `check:docs` / `check:dead` / `check:dup` 全绿；Step 3A 三个测试文件 18/18 通过
  （dashboard-layout 0-W 状态 + CreateSpaceDialog、members owner-only 角色/移除/owner 行只读/admin add-only/
  移除确认文案、project-detail member+creator 显示 / member 隐藏 / admin/owner 显示）。全量 `test` 套件
  126 通过、2 失败——**均为分支既有失败**（`app-sidebar.test.tsx` 与 `handlers.test.ts`，未触碰，与本次
  无关）。
- **Smoke（真实 HTTP）**：A 注册（0 W，见 onboarding）→ A 创建工作区（=owner）→ B 注册不建 W（0 W 状态
  可用）→ A 添加 B → B 见 W 并进入 → A 提升 B→admin（验证）→ A 降级 B→member（验证）→ B 建 P（member+
  creator 见删除按钮）→ A 移除 B（B 不再见 W、不可访问 P/Runtime；P 仍在 W，A 可见可删）。已清理 smoke
  数据。

## 4. 已知缺口（诚实记录）

- **PROJECT WORKSPACE SHARING: UNCHANGED**：后端 Project 共享模型（Step 3）未动；本阶段只对齐前端删除
  按钮门（MM8）。tenant 级 `/projects` 列表仍 owner 过滤；无 editor/viewer 角色细分。
- **ISSUE WORKSPACE SCOPING: NOT IMPLEMENTED — NEXT STEP**：`issues.space_id` 未引入。
- **AGENT/TEAM/WORKFLOW/MCP/SKILL WORKSPACE SCOPING: NOT IMPLEMENTED**：各资源仍无 Workspace 共享。
- **OWNERSHIP TRANSFER NOT IMPLEMENTED**：owner 角色经 member API immutable；所有权转移单独设计，本阶段
  不做（`ownership_transfer_not_supported`）。
- **AUTHENTICATION NOT FULLY IMPLEMENTED**：注册 = 创建 User Identity + 会话，无密码；dev/session 认证
  partial，production-grade 认证未实现（沿用 User Registration 记录措辞）。
- **移除是 membership-only**：不删用户账号、不删租户成员关系、不删资源；被移除者仍是租户成员，只是失去
  该 Workspace 访问权。

---

> 关联测试镜像：`specs/test-cases/cloud/collaboration-workspace/20260922-workspace-member-management.md`
>（MM1–MM9 锚点全部 Covered）。
