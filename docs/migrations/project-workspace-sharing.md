# Project Workspace Sharing — 实施 Project Workspace Sharing Migration（决策 / 集成记录）

> 状态：**IMPLEMENTED**（Step 3；用户 2026-09-21 批准 plan `kind-juggling-crown` 后实施）
> 分支 `workspace合并`，起点 HEAD=`8ea7d11`，实施完成后提交 `feat: share projects within workspace`。
> `main` 保持 `1c5b9b4` 未动（未 merge、未 push、未进入 Final Main Sync）。
> 关联 ADR：`specs/decisions/cloud/collaboration-workspace/20260921-project-workspace-sharing.md`（PS1–PS7）。
> 基础：[[workspace-sharing-model]]（Step 2B 冻结模型，本阶段将 SS1/SS2 在 Project 资源上落地）。
> 结束标记：`PROJECT WORKSPACE SHARING: COMPLETE` · `WORKSPACE MEMBER PROJECT ACCESS: COMPLETE` ·
> `PROJECT DELETE — CREATOR: PASS` · `PROJECT DELETE — WORKSPACE ADMIN: PASS` ·
> `PROJECT DELETE — ORDINARY MEMBER OTHER RESOURCE: DENIED` ·
> `RUNTIME WORKSPACE INHERITANCE: COMPLETE` · `PER-PROJECT MEMBERSHIP: NOT USED` ·
> `LEGACY UNSCOPED PROJECT: OWNER-ONLY` · `ISSUE WORKSPACE SCOPING: NOT IMPLEMENTED — NEXT STEP` ·
> `DOCUMENTATION: SYNCED`

---

## 0. 背景与动机

Step 2B（[[workspace-sharing-model]]）冻结了模型并落地了权限 foundation，但**没有迁移实际资源访问**
—— `project()` / `workspace()` / 空间项目列表仍按 owner 过滤（编码为「当前实现，migration pending」）。
本阶段实施真正的 **Project Workspace Sharing migration**：把 owner-only 的 Project / Runtime Workspace
访问切换为 workspace-shared。不迁移 Issue / Agent / Team / Workflow / MCP / Skill（各自留作后续步骤），
完成后 STOP。

## 1. 决策结论（来自已批准 ADR）

| 决策 | 选择 | 结论 |
| --- | --- | --- |
| PS1 — 访问公式 | **`CanAccessProject(user, P) = P.space_id = W AND user 是 W 活跃成员`** | Project creator 保留（`owner_user_id` 继续作为 creator 身份，无命名洁癖迁移）。unscoped（`space_id IS NULL`）→ owner-only。 |
| PS2 — 删除规则 | **`creator OR Workspace owner/admin`（复用 `CanDeleteWorkspaceResource` 公式）** | 非成员：`project()` 就过不了 → 404（存在性隐藏）。普通 member（可见、非 creator/非 owner/admin）→ **403 `space_role_required`**（比 404 诚实且不泄漏新信息）。legacy：`project()` 已强制 owner → 自动允许。 |
| PS3 — legacy 兼容 | **不自动回填 / 不 NOT NULL / 本阶段不迁移 legacy** | `space_id = NULL` 保持 owner-only，无 scope widening（§16 回归证明）。 |
| PS4 — Runtime 继承 | **`workspace()` 解析父 Project 的 `space_id` 判定** | scoped → workspace 成员可访问（含 `access()` node 准入、start/stop、列表、detail）；unscoped → owner-only。不新增 `runtime_workspace_members`，不发明独立 Runtime ACL。 |
| PS5 — 列表语义 | **空间级列表 workspace-shared；tenant 级列表保持 owner 过滤** | `/spaces/:sid/projects` 返回该空间全部活跃 Project；`/projects/:pid/workspaces` 对 scoped 返回全部运行时；`/projects`（tenant 级）保持 owner 过滤（空间视图才是目标，前端不使用 tenant 级列表）。 |
| PS6 — 前端 | **零代码改动** | space 查询 key 已含 tenant+spaceId（切换重建、无跨区泄漏）；无 tenant 级列表在用；无 per-project membership 缓存；无 Share/Members/Add Project Member UI；无 owner-only/私有措辞。删除按钮 `canDeleteProject`（space role admin\|\|owner）保持不动 —— 已知 UI gap（member 创建者在 UI 看不到自己的删除按钮，API 允许）。 |
| PS7 — rollout | **诚实记录** | Project Workspace Sharing：**IMPLEMENTED**；Issue / Agent / Team / Workflow / MCP / Skill：**NOT IMPLEMENTED — PENDING**；Per-project membership：**NOT USED**；Legacy `space_id=NULL`：owner-only compatibility。 |

## 2. 实施内容

**后端**：
- `internal/core/space_permission.go`：新增事务内谓词 `workspaceCanDelete(t, spaceID, uid, creator)`
  （SS2 公式镜像）；`CanDeleteWorkspaceResource` 改为调用它（语义不变，Step 2B 测试照过）。
- `internal/core/store.go`：
  - `project()`：删除 SQL 里的 `AND owner_user_id`，改为「scoped → `workspaceRole(t, sid, uid) != ""`；
    unscoped → `ownerUserId == uid`」判定（404 隐藏）。
  - `workspace()`：非 admin 改为「先取行、解析父 Project 的 `space_id`、按 PS4 判定」（legacy 行为与
    现状逐字节一致）；admin 分支不变。
- `internal/core/public.go`：
  - 空间项目列表删 `AND p.owner_user_id=$2`（`spaceMember` 门保留）。
  - project workspaces 列表：scoped 删 `AND w.owner_user_id=$3`；unscoped 保持。
  - DELETE `default:` 分支加 PS2 门（scoped 才判，403 `space_role_required`，在 `version()` 之前）。
  - tenant 级列表 / createProject / PATCH / createWorkspace：不动。
- 契约：`internal/contract/openapi.go` 三处 `description()` 措辞 → workspace-shared；
  `go run ./cmd/openapi` 重生成 `api/openapi.json`。不加新端点。

**测试**（真实 PG，`integration/`）：
- 新 `integration/project_workspace_sharing_test.go` 5 组：`TestScopedProjectSharedWithWorkspaceMembers`
  （§14）、`TestScopedProjectDeletePolicy`（§15）、`TestLegacyProjectNotSharedWithWorkspaceMembers`（§16）、
  `TestRuntimeWorkspaceInheritsProjectAccess`（§17）、`TestScopedProjectCrossTenantIsolation`（§18）。
- 删除（Step 2B 的 owner-only 回归，被新语义取代）：
  `TestProjectAccessOwnerOnlyUntilWorkspaceSharing` / `TestRuntimeWorkspaceOwnerOnlyUntilWorkspaceSharing`
  → 其断言并入 §14 / §17 测试。
- 改名：`TestProjectSpaceScopingAndOwnerIsolation` → `TestProjectSpaceScopingAndSharing`（member 读
  200 / 删 403；non-member 全部 404；tenant 列表 owner 过滤保持）。
- MUST STAY 回归：`cloud_test.go` 跨 tenant 403 / legacy 404+空列表、`security_test.go` DB 跨 tenant
  约束、`issues_test.go` / `endpoints_test.go` 等 —— 全部保持绿。

**前端**：无改动（PS6）。

## 3. 验证

- **后端**：`go build ./...`、`go vet ./...` 干净；`go test -count=1 ./...`（REQUIRE_POSTGRES=1 +
  TEST_DATABASE_URL）全绿（含契约逐字节、集成 35s）。
- **集成套件**（真实 PG）：新增 5 组共享测试 + 改名后的 `TestProjectSpaceScopingAndSharing` +
  Step 2B 8 个权限谓词测试全部 PASS。
- **前端**：零改动；全套 gates 复跑（既有 2 个 pre-existing failures 记录不修，无新增失败）。
- **Smoke**（真实 PG + dist + ora-web）：A/B 注册 → A 建 W → A Add Member B → A 在 W 建 P →
  B 重进 W 看到 P、打开 P、打开 P 的运行时 Workspace → B 删 A 的 P → 403 →
  A 删自己的 P → 202 → admin 删他人 P → 202 → C 不可见 P。已清理 smoke 数据。

## 4. 已知缺口（诚实记录）

- **Tenant 级 `/projects` 列表保持 owner 过滤**：共享项目只在空间视图可见（PS5 最小化；前端不使用
  tenant 级列表）。
- **前端删除按钮 UI 门**：`canDeleteProject` = space role admin||owner（比后端规则更严）；member 创建者
  在 UI 看不到自己的删除按钮（API 允许删除）。
- **无 editor/viewer 角色区分**：共享项目 member 的 mutation 能力 = 当前 Project mutation 语义
  （PATCH 改名 / 建运行时经 `project()` 放行），未细分角色。
- **Operations 访问保持 owner 作用域**：`ownedOperation` 仍按 `p.owner_user_id`（本项目范围外，随
  Issue/Operations migration 再议）。
- **ISSUE WORKSPACE SCOPING: NOT IMPLEMENTED — NEXT STEP**：`issues.space_id` 未引入。
- **AGENT/TEAM/WORKFLOW/MCP/SKILL WORKSPACE SCOPING: NOT IMPLEMENTED**：各资源仍无 Workspace 共享。

---

> 关联测试镜像：`specs/test-cases/cloud/collaboration-workspace/20260921-project-workspace-sharing.md`
>（#1–#5 全部 Covered）。
