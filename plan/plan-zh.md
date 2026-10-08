# AgentRunDispatcher 实施计划

> 状态：**持续维护的设计 / 执行记录**  
> 范围负责人：**B — Cloud 业务 / 编排**  
> 当前目标：**Phase 5 — Revision ADR Decision & Amendment Round**（把审计出的 B-1..B-4 与 P-1..P-3 各自收敛为**唯一**方案并写入两个 `proposed` ADR 正文，另为 G-031 / G-032 输出**精确的 amendment proposal**；**不实现任何 Revision 生产路径**）；当前指标 **`REVISION_ADR_DECISIONS_READY_FOR_HUMAN_APPROVAL`**（B-1 Revision 行身份/幂等 ⇒ 控制面在交付终态接管事务内生成 `id` + `PRIMARY KEY (id)` + `UNIQUE (run_id)` + `ON CONFLICT (run_id) DO NOTHING` 读回比较负载；B-2 Object key 第三段改为 **Cloud 生成的尝试 ID**（工作项 ID 在冻结输入时尚不可得），并写清「逻辑交付 / 交付尝试 / Revision」三种身份；B-3 `revision_ref` 由 Cloud 生成为 `refs/ora/revisions/<runId>`、Node 只回显，内容身份是行内 `final_commit` + 对象摘要；B-4 **删除**「缺 `object_store` ⇒ `skipped` 直接释放」路径，改为照常 `delivering` + 照常放出交付工作项 + 拒发授权 ⇒ 交付确定失败 ⇒ **完全由 approved D5 收口**（`failed`、`revisionId = null`），因此**不新增第四条删除触发条件**、不新增 `deliveryState` 取值、**不需要**改 IssueRun ADR；P-1 校验范围（做的两件事 + **不**校验的五项）、P-2 同授权内重复 `PUT` 是覆盖写、P-3 `unchanged` 仍登记且不复用既有 Revision）。**G-031** 输出 Option B 的精确三句 amendment（2 个 approved ADR + 1 个核心用例）；**G-032** 输出 IssueRun **D8** 的精确 amendment（上限只取不可达窗口、终态 `done` + `status` 不变 + `failure_reason = workspace_unavailable`、残留按「活的 Workspace 行 + 终结失败态 operation」登记、**不发明后台清理机制**、绝不谎报删除成功）；新增 **G-033**（IssueRun D5 需澄清「连续失败」自首个交付工作项起算 —— B-4 的收口依赖它）与 **G-034**（放弃后未登记的 `deliver_revision` 工作项仍可被认领）。**两个 Revision ADR 的 `status` 仍为 `proposed`**：本轮只把待审批的最小修订文本写入正文并明确标识，**未**自行批准、**未**改任何 approved ADR 的已批准决策、**未**改生产代码 / proto / migration / OpenAPI / frontend / 测试。上一轮目标：**Revision ADR Approval Round — 解除 G-030 并冻结 Phase 5 最终契约**（对 `status: proposed` 的 Cloud Revision ADR 做 approval-readiness 审计）；上一轮指标 `REVISION_ADR_APPROVAL_BLOCKED`（审计结论 **`REVISION_ADR_NOT_READY_FOR_APPROVAL`**：4 个阻塞项 + 3 条精度补充；**G-029 = `DEFERRED / NON-BLOCKING`**；**G-031 推荐 Option B**；**G-032 提出 IssueRun D8**；**本轮未改 ADR 文件、未改 `status`**）。更早轮次：**Phase 5 Batch 2 — Delivery → Releasing → Done**（交付/释放/终态半边**已交付**；Revision 登记半边由 G-030 受阻，指标 `PHASE_5_BATCH_2_BLOCKED`；实现并验证：`deliver_revision` 工作项经**共享派发谓词**认领与派发；交付终态经既有 `TakeOverNodeEvent` → `agent_delivery_takeover` 在单个调用方事务内提交收据 / 结果 / `DeliverySettled` 钩子 / `last_event_sequence` 推进；D5 的失败重试退避 30s × 2^(n-1) 封顶 10min 且**不产生新的逻辑交付**；D5 放弃后同事务 CAS `delivering → releasing` + D4 派生 `status` + **恰好一个** `delete_workspace` 意图；`RunWorkspaceDeleted` 把 `releasing → done`；**G-019 = CLOSED**；新增 G-029、G-030、G-031、G-032）；再上一轮：Phase 5 Batch 1 — `SessionEnded` 终态接管（`PHASE_5_BATCH_1_DONE`）；再上一轮：Phase 4C 实现完成——Thread API / SSE / Thread Commands / Lifecycle（`PHASE_4C_COMPLETE`）；再上一轮：Phase 4B 实现完成——迁移 `0021` + production 接管 + `ThreadEventsTakenOver` 钩子 + gRPC `TakeOverThreadEvents`）。
> 更新规则：**每一轮实现开始前必须阅读本文件，结束前必须更新本文件。** 本文件是 `plan.md` 的中文对照；Phase 2
> 详细设计的权威版本在 `plan.md` 的 `## Phase 2 — Workspace Settlement 详细设计`（§2.1–§2.15），Phase 3
> 详细设计的权威版本在 `plan.md` 的 `## Phase 3 — Session Start 详细设计`（§3.1–§3.17），Phase 4C
> 详细设计的权威版本在 `plan.md` 的 `## Phase 4C — Thread API / SSE / Thread Commands / Lifecycle 详细设计`（§4C.0–§4C.20），以及 §12 决策
> D-007–D-010 + D-012..D-015、§13 G-002/G-003/G-005 + G-007..G-011、§16 执行标记。中文版此处做同步标记，不重复完整翻译细节。

---

## 1. 文档目的

本文档是 AgentRunDispatcher 以及其相邻 B 侧业务编排工作的受控实施计划。

它有四个目的：

1. 在写代码之前明确实施顺序。
2. 记录代码必须遵循的架构决策与所有权边界。
3. 对实现过程中发现的新问题、新冲突进行记录，禁止在代码中静默自行解决。
4. 提供可审计的历史记录：原计划是什么、发生了什么变化、已经实现了什么、还有什么被阻塞。

这是一个**持续维护的文档**，但不是临时草稿。

任何涉及以下内容的实质性变化，都必须在代码修改之前或与代码修改同时记录到本文件：

- 架构
- 事务边界
- 模块所有权
- 状态转换
- 对外行为
- 关键依赖
- 实施范围

---

## 2. Agent 强制工作协议

每一轮 Agent 工作都必须按照以下顺序进行。

### 修改代码之前

1. 完整阅读本 `plan.md`。
2. 阅读当前作用域内所有适用的 `AGENTS.md`。
3. 对每个受影响仓库执行 `git status --short`。
4. 明确本轮正在实现 `plan.md` 中的哪一个阶段 / 条目。
5. 确认用户要求的工作位于当前阶段的 scope 内。
6. 如果发现新的架构冲突，必须暂停受影响部分，并先记录到 **§13 Open Gaps / Decisions Needed**，不得先写 workaround 再补文档。

### 实施过程中

Agent 不得静默偏离本文件。

如果实现要求改变既定设计，必须更新：

- **Decision Log**
- 受影响的架构 / 状态 / 事务章节
- 受影响的测试计划
- 当前阶段状态

完成这些更新之后，才能把新的设计视为本轮可执行设计。

### 每一轮结束时

Agent 必须更新本文件，至少包括：

- 当前阶段状态
- 本轮修改的文件
- 本轮新增或确认的设计决策
- 新发现的 gap / blocker
- 新增测试
- gate 结果
- 剩余依赖
- 推荐的下一阶段

Agent 最终报告必须明确写出：

> `plan.md updated: yes/no`

如果是 `no`，必须说明为什么本轮不需要更新计划。

### Git 政策

Agent 永远不得：

- `git commit`
- `git push`
- 创建 PR
- 未经架构师明确要求执行 stage

当工作达到稳定提交边界时，Agent 只能报告：

> `COMMIT_BOUNDARY_REACHED`

是否提交、何时提交、提交哪些仓库，由架构师决定。

---

## 3. 架构事实来源优先级

优先级如下：

1. 已批准的 specs / ADR
2. 本 `plan.md`
3. 仓库现有架构与已经建立的代码模式
4. 当前实现

如果当前代码与已批准 ADR 冲突，以 ADR 为准，除非架构师明确批准修改 ADR。

如果本文件与已批准 ADR 冲突，必须将冲突记录到 **§13**，不得静默按照本文件继续实现。

---

## 4. 已确认的设计决策

### 4.1 Agent 身份

对于真实 Space Agent Run：

- `issue_runs.executor_type = 'agent'`
- `issue_runs.executor_id` 在语义上引用 `space_agents.id`
- 不增加物理 FK，因为 `executor_id` 在 agent / team / workflow 之间是多态字段
- 被引用的 Agent 必须：
  - `status='active'`
  - 属于正确 tenant
  - 属于正确 space

### 4.2 Agent Run Snapshot

创建 Run 时，权威业务状态必须把 Agent plugin identity/version snapshot 到 run input 中。

调用方提供的 input 不得覆盖服务端确定的 Agent identity/version 字段。

后续即使 installed / desired plugin version 发生变化，历史 Run 也必须保持创建时固定的版本。

### 4.3 Project 要求

对于真实 `@agent` Task Mode 路径：

- Issue 必须拥有有效 Project
- 否则整个请求原子拒绝，返回 `409 issue_project_required`
- 被拒绝的事务不能遗留 comment、Timeline/activity、interaction 或 IssueRun

Manual Run 是否要求 Project，由其自身 API / ADR contract 决定，禁止通过推断扩大 D2 适用范围。

### 4.4 Business / Control 所有权

B 拥有业务状态，例如：

- `space_agents`
- Agent IssueRun 业务字段 / phase 转换
- Thread 业务状态

A 拥有 control-plane 机制，例如：

- workspace 控制
- session execution 控制
- operation/control machinery

B → A 通过有名字、transaction-aware 的 control-plane seam 调用。

A → B 通过 `AgentRunHooks` 调用。

处于同一个 PostgreSQL transaction 内，**不代表拥有相同的数据所有权**。

### 4.5 B → A control-plane contract

B 侧 contract 包含以下等价能力：

- `CreateRunWorkspace`
- `DeleteRunWorkspace`
- `EnqueueExecutionWork`
- `EnqueueThreadCommand`

这些 contract 必须同步执行、transaction-aware，transaction 由调用方持有。

A 的实现不得为了这些业务调用私自开启替代 transaction。

在 A 的真实实现接入之前，production/default 行为必须 **fail closed**，不得伪造成功。

### 4.6 Busy 语义

Project busy 是**可重试业务状态**，不是 terminal error。

对于 Dispatcher Slice 1：

- busy 不得复用 `idleProject` 当前的 panic / 409 行为
- busy 必须能作为正常 result 表达
- busy 的 AgentRun 保持 queued
- 不得提交 workspace / provisioning 状态

### 4.7 Dispatch Loop 所有权

当前实施方向：

**B-owned process / orchestration loop**

原因：

- 它扫描 B-owned `issue_runs`
- 它决定 queued business run 是否应该尝试 dispatch
- 它通过 control-plane seam 调 A
- 它维护 / 推进 B-owned business state

A 仍然负责实际 control-plane action。

### 4.8 并发基线

当前 Store transaction 使用 PostgreSQL advisory transaction lock，对现有 `transact` 写操作进行跨 Cloud instance 串行化。

Dispatcher 仍然必须使用显式 state guard / CAS 语义，以保证代码意图清晰、replay safety，以及未来弱化全局锁后的正确性。

全局锁**绝不意味着可以持有长事务**。

---

## 5. 当前系统状态

在 Dispatcher Slice 1 之前，以下基础已经完成：

- B skeleton / migration / AgentRun hooks skeleton
- `space_agents` lifecycle
- active-only Agent target resolution
- 真实 Agent Task Mode 的 `issue_project_required`
- plugin identity/version run snapshot
- manual AgentRun identity hardening
- B → A control-plane contract
- fail-closed unavailable control-plane implementation
- deterministic injected control-plane test fake
- A → B `AgentRunHooks`

仍未完成的 A-side 依赖：

- 真实 `CreateRunWorkspace` 实现
- 真实 `DeleteRunWorkspace` 实现
- 真实 `EnqueueExecutionWork` 实现
- 真实 `EnqueueThreadCommand` 实现
- `create_workspace` plugin step
- terminal workspace operation → `RunWorkspaceSettled` wiring

这些依赖不会阻止 Slice 1 基于已批准的 contract / fake 进行实现。

---

## 6. 目标架构

```text
真实 @agent / manual AgentRun 创建
        │
        ▼
IssueRun(status=queued, phase=NULL)
        │
        ├── post-commit 立即尝试 dispatch
        │
        └── B-owned ≤10s recovery scan
                 │
                 ▼
        AgentRunDispatcher.Dispatch(runID)
                 │
                 ├── stale / non-agent / 非 queued → no-op
                 │
                 ├── project busy → 保持 queued
                 │
                 └── project idle
                       │
                       ▼
              CreateRunWorkspace(tx, ...)
                       │
                       ├── Busy → 保持 queued
                       ├── Error → rollback；保持 queued
                       └── Accepted
                              │
                              ▼
                   phase=provisioning
                   status=dispatched
```

后续 Slice：

```text
provisioning
  → workspace terminal callback
  → RunWorkspaceSettled
  → starting
  → session work
  → running
  → delivering
  → releasing
  → done
```

这些后续状态转换明确不属于 Slice 1。

---

## 7. 阶段计划

### Phase 0 — Foundations

状态：**DONE / ARCHITECTURALLY REVIEWED**

包含：

- schema skeleton
- business hooks skeleton
- Agent roster lifecycle
- authoritative Agent resolution
- project gate
- run snapshot
- manual AgentRun hardening
- A seam contracts

### Phase 1 — Dispatcher Slice 1：Claim + Busy + Retry Loop

状态：**DONE / ARCHITECTURALLY REVIEWED**（Slice 1 已实现并复核，见 `plan.md` §8、测试 T1–T9）。

实现已交付（见 `plan.md` §8 与 T1–T9）：

- 真实 `AgentRunDispatcher.Dispatch`（资格、busy-safe、CAS 保护）
- non-panicking project-busy predicate
- authoritative `CreateRunWorkspace` Busy result 处理
- queued → provisioning/dispatched guarded transition
- replay / stale protection
- 真实 Space Agent routing，同时不破坏 legacy/team/workflow dispatch
- 单轮 bounded queued AgentRun scan
- B-owned retry loop，周期 ≤10 秒，graceful shutdown 集成
- 不使用真实 sleep 的 deterministic DB tests

明确排除（不变）：

- `RunWorkspaceSettled`
- provisioning → starting
- session work
- workspace failure cleanup
- Thread
- delivery

### Phase 2 — Workspace Settlement

状态：**PHASE 2 DONE / ARCHITECTURALLY REVIEWABLE**。Phase 2A（B-owned 结算核心）与 Phase 2B（A-side `create_workspace` plugin step + terminal wiring）均已实现并复核；证据见 `plan.md` §2.14。G-002、G-003 CLOSED；T2-3 已 DEFERRED→PASS。

目标：

在 A control-plane terminal evidence transaction 内处理 workspace terminal evidence，并调用 B business hook。

预计工作（Phase 2B 已交付列表，详见 `plan.md` §15 Phase 2B round record）：

- terminal A → B hook wiring（G-003 — 已 wire 于 A-owned `advance` terminal transaction；hook error 回滚整个 terminal write）
- `RunWorkspaceSettled`
- ready → starting
- failed/unavailable → releasing/failed（含 cancel-before-settle；plugin-unavailable `agent_plugin_unavailable` 已由 G-002 打通，T2-3 由 DEFERRED→PASS）
- delete-workspace declaration
- G-002：Agent run-workspace `create_workspace` 内部 `plugin` step（effect-based `plugin_ensure`，run-only instance writer，D-011）；snapshot 版本不可变（§6）；run-scoped 失败不扰动 `space_plugins` 聚合 / `space_agents` roster（D-011）
- rollback / replay tests

Phase 2A 已交付：`RunWorkspaceSettled` 结算核心（`businessAgentRunHooks`）、CAS `provisioning→starting`、`releasing/failed(workspace_unavailable)`、cancel-before-settle `releasing/cancelled(deliveryState=skipped)`、同事务 `DeleteRunWorkspace` 声明（B→A seam，原子）、replay/stale no-op，以及白盒 `TestPhase2A*` 套件。所有 gate 通过（build / vet / core + integration（含 race）/ `go test ./...` / `git diff --check`）。

Phase 3（session `EnqueueExecutionWork`）本轮为**实现轮，已完成 Phase 3A（B-owned Session Start Core）+ Phase 3B（A-side Execution Work Persistence + 生产化 EnqueueExecutionWork）**；当前指标 `PHASE_3B_DONE / READY_FOR_PHASE_4_DESIGN / NOT_READY_FOR_PHASE_4_IMPLEMENTATION`。G-008 CLOSED（真实 `execution_work` 落库 + DB partial-unique 恰好一次）；G-001 执行 seam 部分 CLOSED（除外 `CreateRunWorkspace`/`DeleteRunWorkspace`/`EnqueueThreadCommand` 由各自 phase 拥有，G-001 整体 PARTIAL）。

### Phase 3 — Session Start

状态：**Phase 3A IMPLEMENTED（B side）；Phase 3B IMPLEMENTED（A side real seam → PHASE_3_DONE）**；Phase 4（Thread / running）**DESIGN REVIEW（未实现）**

设计章节权威版本在 `plan.md` 的 `## Phase 3 — Session Start 详细设计`（§3.1–§3.17），以及 §12 决策
D-012..D-015、§13 G-007..G-011、§16 执行标记。核心结论：

- **结束点**：恰好声明一条 AgentSession `execution_work`；`issue_runs.phase` **保持在 `starting`**
  （D-014，IssueRun ADR D3 权威）。`running` 需首条 Thread/session-start 事件被接管
  （`ThreadEventsTakenOver`，Phase 4），不以 enqueue 声明或 workspace ready 推进。
- **first prompt**：即 Thread 首条 entry（`thread_entries(seq=1)`，`source=system,kind=user_turn`），
  由 run-create 冻结输入快照经固定模板渲染，内联为 AgentSession `initial_turn`；不在 `starting` 触发
  Thread API/SSE（D-013）。渲染器未生产化（G-007）。
- **once**：`thread_entries(seq=1)` 是 B 侧持久的 once 标记（`INSERT ON CONFLICT DO NOTHING`，0 影响即 replay no-op），与 `EnqueueExecutionWork` 同事务（D-012/D-015）。重试在独立的 post-settle B-owned **starting 扫描**（D-015，取代 D-010 的 settle 同事务释放）。

**Phase 3A 已交付（本轮）**：`AgentRunSessionStart` + `renderAgentInitialTurn`（G-007 关闭）+ `scanStartingAgentRuns`/`StartQueuedAgentSessionsOnce`（D-015 重试循环，10s 节奏，IMPLEMENTATION CHOICE）+ cmd/server 启动循环接线；once 用 `thread_entries seq=1`（非伪造 execution 表，G-008 仍 OPEN）；`phase=starting/status=dispatched` 保持到 Phase 4。白盒 `TestAgentSessionStart*` 套件覆盖 T3-1/2/3/4/5(序列化)/6/8/10/11/12/13/14。所有 gate 通过。

目标：

从 `starting` 精确释放一个 AgentSession execution work item。

预计工作（Phase 3A — B-owned 核心）：

- 根据 run snapshot 生成固定 first prompt（G-007）
- 对已批准 seam `EnqueueExecutionWork` 用确定性注入 stand-in 落声明（G-001）
- 使用 immutable pinned plugin/version（既有快照 freeze 已证明）
- exactly-once / replay behavior（D-012、D-015）

### Phase 4 — Thread / Running Lifecycle

状态：**Phase 4A IMPLEMENTED（A-side dispatch registration）→ Phase 4B IMPLEMENTED（Thread takeover + `starting→running`）→ Phase 4C IMPLEMENTED（`PHASE_4C_COMPLETE`：Thread API / SSE / commands / lifecycle，迁移 `0022`）→ Phase 5 Batch 1 IMPLEMENTED（`PHASE_5_BATCH_1_DONE`：`SessionEnded` 终态接管）**。权威详见 plan.md 的 §4B.1–§4B.15、`## Phase 4C — …`（§4C.0–§4C.20）与 §15 的各轮记录。

Phase 4A 已交付（D-020/D-021，G-012 PARTIAL）：

- 新 A-owned migration `0020_node_executions.sql`：execution_id PK、work_id UNIQUE FK→execution_work、`node_executions_pending_node`（result IS NULL）按 node 的部分索引
- production `agent_work_dispatch`（`agentWorkDispatch`）：Controller 提供 execution_id/node/input；A 在同一事务写 `node_executions` + fence `execution_work.execution_id`；replay 幂等、不同 id/node/input → `dispatch_conflict`、绝不复写
- 恢复读 `agent_work_get` / `agent_work_pending`（无 lease 纯读；C2 崩溃复用既有 execution）
- claim 与登记都不 running：`issue_runs.phase` 保持 `starting`/`dispatched`、thread seq 恒 1、无 running（T4A-9/10/16）
- 无 4B 机制：`node_event_receipts`/`TakeOverThreadEvents`/starting→running/thread seq≥2/D-023 seq 分配全部未实现且本轮禁止
- 测试 T4A-1..T4A-16（真实 PostgreSQL）+ `TestMigration0020NodeExecutionsAppliesFreshAndUpgrades`

Phase 4B 已交付（本轮，IMPLEMENTATION）：

- **迁移**：`0021_node_event_receipts.sql`（4B 唯一新表）：PK `(execution_id, sequence)`、FK→`node_executions`、
  `event jsonb`、`node_event_receipts_retention` 索引；**不改业务表**、不改既有迁移
- **production A 侧接管核心** `internal/core/agent_run_thread_takeover.go`（`agent_thread_takeover`）：校验 →
  收据幂等/冲突 → 钩子 → `last_event_sequence` fence，全程在 caller 事务内
- **production B 侧钩子** `internal/core/agent_run_thread.go`（`Store.threadEventsTakenOver`）：seq≥2 分配 + 条目写入
  + 首条真实记录 CAS `starting/dispatched → running/running/active`
- **接线**：`internal/controlgrpc/agentruns.go`（`TakeOverThreadEvents`）、`internal/controlgrpc/server.go`、
  `internal/core/agent_run_settle.go`（`businessAgentRunHooks`）、`cmd/server/main.go`（`store.AgentRunHooks`）
- **G-013 CLOSED（D-024）**：`starting→running` 唯一权威 = 首条**真实 Node Thread 记录**被接管且钩子同事务提交；
  无 synthetic session-start 事件；`thread_state` `pending`→`active`（4B 只写此转换）；echo 首 prompt 去重保 seq=1
  不变；空批次拒绝 —— **已实现并有测试证据**
- **G-009 CLOSED**：seq=1 保留、seq≥2 连续、重放安全均已由 T4B-1/4/11 证明
- **G-014 CLOSED / D-023 ACCEPTED（已实现）**：`seq` = 接管事务内 `MAX(seq)+1`（从 2 起）；串行化证明来自
  approved 事实（一运行一会话执行 + 全局 advisory lock + `PK(run_id,seq)`），**不依赖 proposed 的
  controller-session ADR**；Node `sequence` ≠ Thread `seq`
- **G-012 PARTIAL**（未拆分）：`node_event_receipts` + `TakeOverThreadEvents` 已实现；G-012 其余部分仍待后续
- **G-015 PARTIAL / NON-BLOCKING**：初值存在（单运行 event 上限 200,000；收据 `done` 后保留 30 天），但**本轮不
  enforcement**（无 approved ADR 依据）
- **G-016（4B 轮新增 OPEN；**已由 Phase 4C 设计轮 CLOSED**，见 D-4C-01）**：无写者物化字面 `thread_state='pending'`
- **测试**：T4B-1..T4B-19（真实 PostgreSQL 白盒）+ 端到端 gRPC `TestAgentRunThreadTakeoverOverGRPC` /
  `...GRPCRejections` + `TestMigration0021NodeEventReceiptsAppliesFreshAndUpgrades`；T4B-14/T4B-18 为 DEFERRED
  （依 mandate 禁令：4B 无 cancel 写入路径、200,000 上限未获批）

包含：

- thread_entries seq≥2 续接 + takeover hooks（4B — **已实现**）
- starting → running，唯一权威 = 已提交的 `ThreadEventsTakenOver` 接管事务（D-019，4B — **已实现**）
- `execution_id` 经 `RecordDispatch` 登记（`node_executions`，4A，D-020/D-021 — **已实现**）
- `node_event_receipts` 事件身份（收据）+ `thread_entries(node_execution_id, node_sequence)` 条目唯一（4B，D-022 — **已实现**）
- Thread command control seam（`EnqueueThreadCommand`，4C — **仅设计**，D-4C-05/D-4C-06）
- Thread API / SSE（4C — **仅设计**，D-4C-02/D-4C-03/D-4C-07/D-4C-08）
- `thread_state` 生命周期 `pending`/`active`/`idle`/`ending`（4C — **仅设计**，D-4C-01/D-4C-09/D-4C-10；`ended` 属 Phase 5）
- cancel/terminal 交互（4C — **仅设计**，D-4C-11；`SessionEnded` 属 Phase 5）
- migration 0022（4C — **仅设计、未创建**，D-4C-12）

> **已被后续轮次取代：** 上面四条 4C 条目在 `PHASE_4C_COMPLETE` 时**已实现**（迁移 `0022` + Thread API / SSE /
> commands / lifecycle），其「仅设计」措辞是 4C 设计轮的记录。`ending → ended` 与 `queued → discarded` 在
> `PHASE_5_BATCH_1_DONE` 时**已实现**；delivery / releasing / `done` 仍属 Phase 5 Batch 2。

核心原则（mandate §4）：`execution_work created ≠ running`、`claim ≠ running`、`dispatch ≠ running`；只有
权威接管/会话开始证据经 `ThreadEventsTakenOver` 钩子成功提交才 `starting → running`。

决策 D-019..D-026 与 D-4C-01..D-4C-12、缺口 G-009/G-012..G-021 见 §12/§13；4A/4B 的 T4-* 行已按实现证据标
Covered，T4-14/T4-15 为 DEFERRED，**T4C-1..T4C-34 为 DESIGNED / MISSING（本轮为设计轮，无测试代码）**。
当前状态指标见表头与 §15。

### Phase 5 — Delivery / Releasing / Done

状态：**IN PROGRESS — Batch 1 DONE，Batch 2 除 Revision 登记半边外已交付（受阻面 = G-030）**

**Batch 1 — `SessionEnded` 终态接管（DONE，2026-10-08）**：会话执行的终态 Node 事件走既有 `TakeOverNodeEvent`
权威（control action `agent_session_takeover`），在单个调用方事务内一并提交：收据、持久终态结果、Thread
`ending → ended`（也接受 `pending | active | idle`）、仍 `queued` 的用户轮次 `→ discarded`、`running → delivering`、
恰好一个未登记的 `deliver_revision` 工作项、以及带栅栏的 `last_event_sequence` 推进；提交后发布恰好一条
`issue_run.thread_changed`。重放为 no-op；拒绝与钩子失败零写入。测试：`integration/agent_run_session_end_test.go`。

**Batch 2 — Delivery → Releasing → Done（除 Revision 登记半边外已交付，2026-10-08）**：`deliver_revision` 工作项经
**共享的执行工作谓词**认领与派发（`agent_work_claim`/`agent_work_dispatch` 的 kind 白名单扩为
`('agent_session','deliver_revision')`；恢复读路径 `agent_work_get`/`agent_work_pending` **未**加过滤）；交付终态经
既有 `ExecutionService.TakeOverNodeEvent` → 控制动作 `agent_delivery_takeover` 接管，事务形状与 Batch 1 相同
（收据 → 结果 → `DeliverySettled` 钩子 → 带栅栏的序号推进），任一步失败整体 rollback。`DeliverySettled` 的决策顺序为
「身份校验 → 未知 kind 拒绝 → `phase != 'delivering'` 确定性 no-op → `saved`/`unchanged` 受阻拒绝 → `failed` reason 闭集 →
`deliveryGivenUp` → 释放或退避重试」，重试**不创建新的逻辑交付**。放弃后在同一事务内完成 `delivering → releasing`、
D4 派生的 `status` 与**恰好一个** `delete_workspace` 意图；`RunWorkspaceDeleted` 再把 `releasing → done`
（`done` 重放 no-op，`provisioning`/`starting`/`running`/`delivering` 与未知 run 被拒），且 `done` **不**把业务结果解释为
`completed`。两条周期补偿（`GiveUpStaleDeliveriesOnce`、`RedeclareRunWorkspaceDeletesOnce`，间隔 10s）只读 PostgreSQL
并复用既有事务路径，**不是**内存队列权威。测试：`integration/agent_run_delivery_test.go`（P5-7…P5-16）与
`internal/core/agent_run_release_db_test.go`（5 个白盒）。
**受阻半边**：Cloud Revision ADR 仍为 `status: proposed`，故 Revision 登记、对象校验与 `GrantRevisionUpload` **未实现**，
`revision_delivered`/`revision_unchanged` 一律回 `UNAVAILABLE` 且**零写入**（**G-030**）；交付成功路径因此只能经 D5 放弃或
D6 无会话取消到达 `releasing`。

**Revision ADR Approval Round（规范轮，2026-10-08，`REVISION_ADR_APPROVAL_BLOCKED`）**：对
`specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`（仍 `status: proposed`）做
approval-readiness 审计，结论 **`REVISION_ADR_NOT_READY_FOR_APPROVAL`** —— 存在 4 个**会导致两种合理实现**的
阻塞项（见 §13 G-030 的 B-1..B-4）与 3 条精度补充（P-1..P-3）。本轮**未改 ADR 正文、未改 `status`、未改任何生产代码 /
proto / migration / OpenAPI / frontend**：只登记阻塞项与**最小文本修订提案**，并把**精确的人类审批文本**交给人类裁决。
同轮裁定：G-029 = `DEFERRED / NON-BLOCKING`（**不**为其扩 schema）；G-031 推荐 **Option B**；G-032 提出 IssueRun
**D8** 最小 amendment（**未实现**）。下一实现切片仅在 ADR 获批（且 B-1..B-4 按提案收敛）后开始。

**Revision ADR Decision & Amendment Round（决策与修订轮，2026-10-08，`REVISION_ADR_DECISIONS_READY_FOR_HUMAN_APPROVAL`）**：把上一轮的
B-1..B-4 与 P-1..P-3 各自收敛为**唯一**方案，并写入两个 `proposed` ADR 的正文（文首与文末「修订记录」**明确标识为待审批**）——
B-1 ⇒ D4 的控制面身份 + `PRIMARY KEY (id)` + `UNIQUE (run_id)` + `ON CONFLICT … DO NOTHING` 读回比较负载；
B-2 ⇒ D2 的「逻辑交付 / 交付尝试 / Revision」三种身份表 + 键模板第三段改为 **Cloud 生成的尝试 ID**；
B-3 ⇒ D4 的 `revision_ref`（Cloud 生成 `refs/ora/revisions/<runId>`，Node 只回显）；B-4 ⇒ **删除**「缺 `object_store` ⇒ `skipped` 直接释放」，
改为照常 `delivering` + 拒发授权 ⇒ 交付确定失败 ⇒ **完全由 approved D5 收口**（`failed`），因此**不新增第四条删除触发条件**、
不新增 `deliveryState` 取值、**不需要**改 IssueRun ADR；P-1..P-3 ⇒ D4 的校验范围 / 重复 `PUT` / `unchanged` 三条精度。
同轮为 **G-031**（Option B）与 **G-032**（IssueRun D8）输出**精确的 amendment 提案**（**未**改写 approved ADR），
并新登记 **G-033**（IssueRun D5 的「连续失败」需澄清为自首个交付工作项起算 —— B-4 收口依赖它）与 **G-034**
（放弃后未登记的 `deliver_revision` 工作项仍可被认领）。**本轮未改生产代码 / proto / migration / OpenAPI / frontend / 测试、
未改任何 `status`、未自行批准、未实现 Revision 生产路径。**

包含：

- session end — **Batch 1 DONE**
- delivery settlement — **Batch 2 DONE（除 Revision 登记半边，G-030）**
- cleanup — **Batch 2 DONE**
- workspace deletion — **Batch 2 DONE**
- terminal states — **Batch 2 DONE**

---

## 8. Slice 1 详细技术设计

### 8.1 Eligibility

Dispatcher 只处理：

```text
executor_type = 'agent'
status = 'queued'
phase IS NULL
```

non-agent 以及已经推进过状态的 run 一律 no-op。

### 8.2 Dispatch Transaction

每次 Dispatch attempt 使用一个短 transaction。

transaction 内允许：

- load run
- validate state
- load Issue / Project identity
- 执行 non-panicking busy check
- 调用 transaction-aware `CreateRunWorkspace`
- 应用 guarded B business transition

transaction 内禁止：

- sleep
- retry delay
- long polling
- channel wait
- goroutine synchronization
- 等待任何外部异步 workflow

### 8.3 Busy Check

必须有两层保护。

#### Layer A — Database / Preflight Predicate

non-panicking predicate 检查 Project 当前是否存在 active operation。

目的：

- 避免无意义 seam call
- 保持现有业务语义

如果现有 `idleProject` 通过 panic / 409 表达 busy，则本处不得使用它。

#### Layer B — Authoritative Seam Result

即使 precheck 判断 idle，状态仍可能在之后变化，因此 `CreateRunWorkspace` 仍可能返回 Busy。

该结果是 authoritative。

如果 Busy：

```text
status 保持 queued
phase 保持 NULL
workspace_id 保持 NULL
```

除非已批准 contract 对 workspace identity 有明确不同规定。

该 transaction 正常 commit。

### 8.4 Accepted Transition

只有 `CreateRunWorkspace` 返回 accepted，才允许：

```text
phase = provisioning
status = dispatched
```

control-plane declaration 与 B-side transition 必须在同一 transaction 中原子提交。

如果 seam 返回 error 或 phase update 失败，则整个 transaction rollback。

### 8.5 Replay / Duplicate Calls

状态转换必须使用显式 state guard / CAS 等价机制。

成功 dispatch 之后，再次 Dispatch 同一 run 时，不得：

- 再次调用 workspace creation
- 创建重复 operation
- 回退 phase

数据库 uniqueness / idempotency rule 作为第二层保护。

### 8.6 Immediate Dispatch

Run creation transaction commit 后，可以立即对真实 AgentRun 尝试一次 Dispatch。

这是低延迟优化，不是正确性依赖。

如果 enqueue 后进程立即 crash，后台 scan 必须仅根据 PostgreSQL 状态恢复 queued AgentRun。

### 8.7 Retry Scan

实现一个单轮函数，语义类似：

```text
DispatchQueuedAgentRunsOnce(ctx)
```

职责：

1. 获取 bounded、deterministic 的 eligible run ID batch
2. 结束 scan / read transaction
3. 对每个 run 独立调用 Dispatch

禁止在一个 transaction 中 dispatch 整个 batch。

推荐 deterministic order：

```text
created_at, id
```

batch size 优先复用仓库已有 precedent。

如果没有 precedent，则把选择的 batch bound 记录到 Decision Log，并标记为 implementation choice。

### 8.8 Retry Loop

loop 周期执行 one-shot scanner，cadence ≤10 秒。

应尽量复用现有 loop / sleep / shutdown abstraction。

测试不得等待真实 wall-clock 时间。

sleep 永远发生在 database transaction 之外。

### 8.9 Error Handling

#### Busy

正常状态，不是 error。

不得标记 failed。

#### Control-plane unavailable

可重试 infrastructure / dependency condition。

Run 保持 queued。

Slice 1 不得标记 failed。

避免每 10 秒产生高噪声 error log，遵循现有 logger convention。

#### Unexpected Internal Error

transaction rollback。

除非已批准 ADR 明确规定，否则 Run 保持 queued。

Slice 1 不引入新的 terminal state。

---

## 9. Slice 1 测试计划

### T1 — Idle Project Accepted

- active Agent
- valid Project
- queued run
- fake `CreateRunWorkspace` → accepted

期望：

- seam 调用一次
- 传入当前 transaction
- `phase=provisioning`
- `status=dispatched`

### T2 — Busy Project Stays Queued

预置 Project active operation。

期望：

- run 保持 queued
- phase 保持 NULL
- 不产生 provisioning transition
- 不产生 terminal error

### T3 — Busy Then Retry

第一次 attempt busy。

结束 / 移除 active operation。

执行一次 deterministic scan。

期望进入 provisioning。

### T4 — Seam-level Busy Race

DB precheck 观察为 idle。

Injected `CreateRunWorkspace` 返回 Busy。

期望 run 仍保持 queued。

### T5 — Seam Error Rollback

Injected seam 返回 error。

期望：

- transaction rollback
- run queued
- 不存在 partial B transition

### T6 — Replay

第一次 accepted。

第二次 Dispatch 同一 run。

期望：

- 不再调用 seam
- state 不变化
- 无 duplicate declaration

### T7 — Non-Agent Ignored

queued team/workflow run 不得被 Agent retry scan 选中。

### T8 — Scan Eligibility / Bound

只有 eligible queued AgentRun 被 one-shot scan 返回。

验证 deterministic ordering / batch bound。

### T9 — Immediate Post-Commit Route

如果无需扩大 Slice scope 即可接入：

- 创建真实 AgentRun
- 注入 accepted control seam
- 验证 post-commit immediate Agent dispatch

如果需要 unrelated restructuring，则 defer 并记录原因。

---

## 10. Quality Gates

```bash
export PATH="$HOME/.local/go/bin:$PATH"

gofmt -w <本轮修改文件>

go build ./...
go vet ./internal/... ./integration

TEST_DATABASE_URL=<existing> \
REQUIRE_POSTGRES=1 \
go test ./integration -count=1

go test -race -count=1 ./internal/core/... ./integration

go test ./... -count=1

git diff --check
```

如果 specs 有修改：

```bash
git -C ../specs diff --check
```

不要仅为了制造无关 gate 的绿色结果而自行安装缺失的 frontend dependencies 或 buf，除非架构师明确要求。

不得 skip、weaken assertion 或用 timeout masking 掩盖失败。

---

## 11. Repository / Commit Discipline

Cloud 和 specs 是独立 repository。

修改必须保持 repository 归属清晰。

`specs` evidence 只能在 implementation / test 已经真正覆盖 obligation 后更新。

不得提前把未来 Slice 标记为 Covered。

Agent 永远不得 commit。

达到稳定边界时，只报告：

> `COMMIT_BOUNDARY_REACHED`

是否 commit、commit 哪些 repository，由架构师决定。

---

## 12. Decision Log

### D-001 — `executor_id` 是语义引用

状态：Accepted

Agent `executor_id` 保存 `space_agents.id`，不增加 physical FK。

### D-002 — Authoritative Agent Snapshot 优先于 Caller Input

状态：Accepted

plugin identity/version 在 run creation 时由 server-side authoritative business state 决定。

caller-provided input 不得伪造这些字段。

### D-003 — Project Busy 是正常 Retry Result

状态：Accepted

Dispatcher 不得复用 panic / 409 busy behavior。

### D-004 — Dispatch Loop 归 B 所有

状态：Accepted for implementation，除非新的已批准 ADR 明确推翻。

### D-005 — Immediate Dispatch 是优化，Scan 是 Recovery

状态：Accepted

Queued AgentRun 在 process restart 后必须可以仅依赖 PostgreSQL 恢复。

### D-006 — Slice 1 不增加 Retry Timestamp

状态：Accepted

Slice 1 使用固定 periodic scan，周期 ≤10 秒。

不得为了 Slice 1 新增 `next_attempt_at` / lease field / retry migration。

### D-012..D-015 — Phase 3 session-start 决策（新增，Design round；完整条款见 `plan.md`）

- **D-012 — 执行身份与 exactly-once**：唯一身份是 `execution_work (run_id, kind='agent_session')` 部分唯一行；
  B 不写控制表（D6 invariant 7）；`execution_id` 由 Controller `RecordDispatch` 生成、落在 A 行，不在 `issue_runs`。
- **D-013 — first prompt 来源与不可变性**：即 Thread 首条 entry（seq=1, system user_turn），由 run-create 冻结
  输入快照经固定模板渲染，内联为 `initial_turn`；不实时重读 issue/评论；渲染器未生产化（G-007）。
- **D-014 — starting→running 权威证据**：`running` 需首条 Thread/session-start 事件被接管（`ThreadEventsTakenOver`，
  Phase 4）；enqueue 声明与 workspace ready 都不是证据。Phase 3 结束后 `phase` 仍 `starting`（不变量 2）。
- **D-015 — Phase 3 重试归属**：声明重试由一个独立的 post-settle B-owned **starting 扫描**承担（幂等、部分唯一
  once）；取代 D-010 的「settle 同事务释放」建议。

### D-016..D-018 — Phase 3B 决策（实现完成；完整条款见 `plan.md`）

- **D-016（已实现）**：`execution_work` 是 A-owned 权威执行身份；真实 DB 部分唯一索引
  `execution_work_unregistered_once` 强制 exactly-once（关闭 G-008）。
- **D-017（已实现）**：Controller `agent_work_claim` 是纯读；**拾取 ≠ running**（`phase=running` 需 Phase 4 接管证据）。
- **D-018（已实现）**：同 work 载荷不匹配的重放是冲突，与共享事务一起回滚。

### D-019..D-025 — Phase 4 / 4A / 4B 决策（完整条款见 `plan.md`）

- **D-019（已接受，4B 实现待落地）**：`starting→running` 唯一权威 = 已提交的 `ThreadEventsTakenOver` 接管事务；
  claim / `RecordDispatch` / Node 派发 / 物理分配 **均非** running 证据。
- **D-020（已实现，Phase 4A）**：`execution_id` 由 Controller 生成；A 在 `RecordDispatch` 记录 `node_executions`
  并 fence `execution_work.execution_id`，绝不生成身份。
- **D-021（已实现，Phase 4A）**：一 `execution_work` 至多一个权威 `execution_id`（PK `execution_id` + UNIQUE `work_id`
  + CAS fence）。
- **D-022（已实现，Phase 4B）**：事件身份 = `node_event_receipts(execution_id, sequence)`；条目身份 =
  `thread_entries(node_execution_id, node_sequence)` UNIQUE。两者均已随 0021 迁移与接管路径落地。
- **D-023（ACCEPTED，Phase 4B 已实现）**：Thread `seq` 分配 = 接管事务内 `MAX(seq)+1`（从 2 起）。串行化证明来自
  approved 事实（一运行至多一会话执行 + 全局 advisory lock + `PRIMARY KEY (run_id, seq)`），**不依赖 proposed 的
  controller-session ADR**；Node `sequence` ≠ Thread `seq`；不新增计数器。
- **D-024（ACCEPTED，Phase 4B 已实现）**：Phase 4B 会话开始权威 = 首条**真实 Node Thread 记录**接管（无 synthetic
  session-start 事件）；`thread_state` `pending`(已登记无记录)→`active`（4B 只写此转换）；echo 首 prompt（`turn_id == initial_turn.turn_id`）
  只写收据、不分配 seq、不新增条目；空批次 `ABORTED` 拒绝。
- **D-025（ACCEPTED 为初值；本轮不 enforcement）**：G-015 初值——单运行非终态 event 上限 `thread_event_cap` 默认
  200,000；`node_event_receipts` 保留 `node_event_receipts_retention` 默认 `done` 后 30 天。**Phase 4B 有意不实现
  enforcement**：无 approved ADR 给出具体值，依 mandate §3/§39 保持 deferred。
- **D-026（本轮新增，Phase 4B 实现细节）**：记录 `kind` 取 `ora-history` 记录自身的 `type` 标签（六值闭集，权威 =
  desktop `crates/history/src/record.rs`）；批大小用 approved 的 1..64（非 D-025 的 cap）；两类 fail-closed 错误
  （invariant 失败 → `UNAVAILABLE` 可重试；client 类错误 → `CONFLICT` 丢弃）；stale/cancel/workspace-invalid 的 run
  仍写条目与收据但**跳过** `starting→running` CAS；字面 `thread_state='pending'` 无写者（该子句**已被 Phase 4C 的 D-4C-01 取代**，见下节与 §13 G-016 CLOSED）；§4B.3 中 echo 行
  被 §4B.6/§31 取代（echo-only 批次不进 running）。

### D-4C-01..D-4C-12 — Phase 4C 决策（设计轮；完整条款见 `plan.md` 的 `## Phase 4C — …`）

Phase 4C 是**纯设计轮**：下列决策均为设计结论，**没有任何 production 代码 / migration / 测试 / API 落地**。
逐条完整条款（含前置条件、事务边界、谓词、回滚、重放、并发、反例与替代方案）见 `plan.md` §4C.2–§4C.12。

- **D-4C-01（设计；关闭 G-016）**：`thread_state='pending'` **物化**（非派生）。写者 = B-owned `StartSession` 的既有事务，
  与 seq=1 同事务、同谓词（`thread_state IS NULL`）、**断言 1 行受影响**；另加幂等 backfill 迁移。否决 A 侧
  `RecordDispatch` 写入与读时派生。**取代 D-026 中「`pending` 无写者」子句**。
- **D-4C-02（设计，GET）**：`GET .../runs/:rid/thread` 返回 `{items, threadState, idleSince, nextCursor, prevCursor}`；
  快照语义；`after=N` = `seq > N`、`after=0` = 头、`before=N` = 更旧的 `limit` 条（仍升序返回）、无游标 = 取尾；
  两游标并存 → `400 invalid_pagination`；`limit` 默认 200 / 上限 500（**不复用、不放宽** `page`/`window`）；
  游标为十进制 seq 的不透明编码 → `400 invalid_cursor`。run 资源形状不变。
- **D-4C-03（设计，POST）**：`POST .../runs/:rid/thread` 接受条件 = `thread_state ∈ {pending,active,idle}` 且
  `cancel_requested_at IS NULL` 且 workspace live，否则 `409 thread_closed`；缺失/跨租户/非 agent → `404 not_found`；
  不可能的 phase/state 组合 → `500`。同一 `Store.transact` 内写条目（`source='user'`、`kind='user_turn'`、`status='queued'`、
  Cloud 生成 `turn_id`）→ CAS `thread_state='active'` + `idle_since=NULL`（须 1 行）→ `EnqueueThreadCommand`。
- **D-4C-04（设计，POST 幂等）**：复用既有 `idempotency_records`（哈希含 path）；7 例矩阵 —— 同 key 同载荷重放返回
  原响应；同 key 不同载荷 → `409 idempotency_conflict`；**同 key 指向不同 run → 同样是 `409`（不跨 run 重放）**；
  终态后重放返回**原始**响应；缺 key → `400 idempotency_key_required`。不新增条目级幂等列。
- **D-4C-05（设计，seam）**：`thread_commands` 归 **A** 所有；`command_id` 由 A 生成；seam 在**调用方事务**内执行；
  未接线时 fail-closed → `503 thread_command_unavailable`（因整体回滚不留幂等记录，可安全重试）。
- **D-4C-06（设计，身份分离）**：`turn_id`（Cloud 生成的用户轮次身份，写进条目）/ `command_id`（A 的投递身份）/
  `execution_id`（Controller 生成）/ `seq`（B 的 Thread 序号）四者语义与生成方互不代偿。
- **D-4C-07（设计，SSE 来源）**：SSE **只做失效通知**（`issue_run.thread_appended{issueId, runId, lastSeq}`），
  **只有 GET 推进客户端游标**；GET 与 SSE 重连共用同一 `after` 语义；PG notify **绝不被当作持久日志**；
  需为 `SpaceEvent` 增加三个可选字段。
- **D-4C-08（设计，SSE 序/重/丢）**：不承诺顺序；重复无害；**数据不丢、通知可能丢** → 前端必须重连 + ≥30s 轮询；
  即使只有 `status` 变化、`lastSeq` 未变也要发通知；更早的 `status` 翻转由客户端重读已加载窗口观察。
- **D-4C-09（设计，active/idle）**：`active ⇄ idle` 在**接管事务内**由本批**最后一条被接管的 content 记录**决定
  （`turnEnded` 且不存在 `source='user' AND status='queued'` → `idle` + `idle_since` = DB 时间）；受同一 gate 约束；
  gate 不满足时仍写条目。
- **D-4C-10（设计，ending/ended）**：`ending` 有三个触发源，恰好一次 `EndSession{reason}`；空闲窗口配置
  `issue_runs.thread_idle_timeout` 默认 15 分钟，由 B-owned 派发循环按 **DB 时间**判定；
  **`ending → ended` 唯一权威 = `SessionEnded`，属 Phase 5**；用户主动结束的端点缺失 → **G-018**。
- **D-4C-11（设计，cancel/terminal）**：与 4B「cancel-first 接管不进 running」一致；`starting` 取消 → 直接
  `releasing`，无 `ending`/无 `EndSession`；`running` 取消 → 同一事务写 `cancel_requested_at` + `ending` +
  `EndSession{cancelled}`；历史不可变（仅 `status` 单向推进）。
- **D-4C-12（设计，迁移）**：**需要** migration 0022：`thread_commands` 表（+FK/CHECK/部分索引）、`thread_entries.status`
  （`CHECK ((source='user') = (status IS NOT NULL))` + 部分索引）、`pending` backfill、`issue_runs (idle_since) WHERE
  `thread_state='idle'` 部分索引。**明确不做**：不改 `thread_state` 值集、不加计数器、不加条目级幂等列、
  不改 `node_event_receipts`、不加 `DEFAULT`、不引入第二个 running 权威。**本轮不得创建该 migration**。

### Phase 4C Readiness — 复核结论（评审轮，2026-10-08；完整条款见 `plan.md` 的 `## Phase 4C Readiness — …`）

- **D-4C-01..D-4C-12 全部复核通过**，未静默修改任何决策。其中 4 条需要 ADR 修订才能作为「已批准事实」落地
  （见 §13 的 G-022/G-023/G-024 与 `plan.md` §4R.7 的 A1–A4，**均为待批准，未写入任何 ADR 文件**）。
  **更新（收口轮，2026-10-08）**：这 4 条（A1/A2/A3/A4）与 G-018 已**全部获人类批准并写入** Thread ADR 正文
  （A1→D1、A2→D3、G-018→D4、A3+A4→D5）并实现；`G-017 / G-018 / G-023 / G-024` 据此关闭。
- **G-017 重新切分**：`after`/`limit ≤ 500`/升序/`threadState`/事件 `{issueId,runId,lastSeq}` = **已有批准依据**
  （Thread D5 逐字）；无游标 tail 读、`before`、`idleSince`、`nextCursor`/`prevCursor` = **新扩展**，需 ADR 修订。
  `pending` 的写入时点属**措辞澄清**（D4 行 1 有两种读法，设计轮取「Cloud `StartSession` 时」）。
- **G-018 给出端点契约提案**：`POST .../runs/{rid}/thread/end`、无 body、强制 `Idempotency-Key`、`202 Accepted`
  + `{threadState:"ending"}`、同事务恰好一条 `EndSession{user_ended}`、与 cancel/idle 的竞态用 CAS 影响行数 = 1 定序。
  **`ending → ended` 仍归 Phase 5。**
- **关键核验**：proto 已完整（`ClaimThreadCommands`/`RecordThreadCommandDelivered`/`ThreadCommand`/`SubmitUserTurn`/
  `EndSession`/`ThreadCommandAvailable` 均已声明）⇒ **Phase 4C 无需改 proto**；`thread_entries` 无 `status` 列；
  `thread_commands` 表不存在；`SpaceEvent` 缺 `issueId`/`runId`/`lastSeq`；`PublicRequest` 无 `Before`。
- **实施切片 S1–S7**：每片含 Scope / 预计修改文件 / Migration 影响 / Transaction contract / Ownership contract /
  对应 T4C 测试 / Regression tests / Exit criteria。**S1 不依赖** G-017/G-018，可直接开工。

---

## 13. Open Gaps / Decisions Needed

### G-001 — 真实 A Control-plane 实现

Owner：A

Production E2E 需要：

- `CreateRunWorkspace`
- `DeleteRunWorkspace`
- `EnqueueExecutionWork`
- `EnqueueThreadCommand`

在 contract 已批准的前提下，B 可以使用 deterministic injected stand-in 推进。

Phase 3 Design 状态（2026-09-30）：**仍完全 open，且决定 3A/3B 拆分。** 控制面表
`execution_work`、`node_executions`、`node_event_receipts`、`thread_commands` 在任何 migration 中都不存在
（0018 只有业务表），`controlgrpc.ClaimWork` 只是 tenant-clone stub，`CreateRunWorkspace`/`EnqueueExecutionWork`
只有 `Unavailable` stub + test fake。Phase 3 声明因此在确定性注入 stand-in 上跑（Phase 3A）；真实 A seam 属
Phase 3B / A 侧后续。见 detail 章节 3A/3B 拆分结论、D-012、G-007/G-008/G-010。

### G-002 — create_workspace Plugin Step

Owner：A

最终 control flow 必须保证 Workspace 被认为 ready 之前 Agent plugin 已可用。

不得在 B Dispatcher Slice 1 中顺手修复。

### G-003 — Terminal Workspace Operation → `RunWorkspaceSettled`

Owner：A/B integration seam

需要明确 control-plane terminal call site，并在正确 transaction 内调用 B business hook。

Phase 2 处理，不属于 Phase 1。

### G-004 — Global Advisory Lock 的未来可扩展性

状态：不是 Slice 1 blocker。

当前 Store mutation 通过 PostgreSQL 全局 advisory transaction lock 串行化。

Slice 1 在此基础上继续使用 short transaction + explicit state guard。

未来是否缩小或替换全局锁，是独立架构任务。

### G-007..G-011 — Phase 3 session-start 缺口（本 Design 轮新增；Phase 3A 实现轮后更新状态）

完整条款见 `plan.md` §3.17。Phase 3A 后状态：
- **G-007** first-prompt 固定模板渲染器（`renderAgentInitialTurn` → `thread_entries` seq=1 `system/user_turn`）— **已关闭（CLOSED）**，确定性测试已过。
- **G-008** `execution_work`/`node_executions` 表与 `execution_id` 写入 — **CLOSED（Phase 3B）**：A-owned `execution_work` 表（`0019_execution_work.sql`）+ `execution_work_unregistered_once` partial-unique 恰好一次已落地；`execution_work.id` A 生成，`execution_id` 由 Controller `RecordDispatch` 写入（Phase 3B 建行不占用 `execution_id`）。`node_executions` / `node_event_receipts` / `thread_commands` 不在 Phase 3B scope（§14 除非 pickup 需要，否则不建表）。
- **G-009** `thread_entries` seq=1 被保留，Phase 4 需从 seq=2 续接 — **CLOSED（Phase 4B 实现轮）**：分配机制
  （D-023：接管事务内 `MAX(seq)+1` 从 2 起）+ echo 去重（D-024）已落地，seq=1 永不重编号。证据：T4B-1（seq=1 保留、
  首条真实记录落 seq=2）、T4B-11（同 Node 事件重放不产生第二条条目）、T4B-16（并发批次 seq 单调唯一）、
  `TestAgentRunThreadTakeoverOverGRPC`（gRPC 端到端 + C5 重放后 seq/条目数不变）。
- **G-010** 从 `sandbox_instances`/`node_instances` 派生 D6 `target` — **以最小确定性 `sessionStartTarget` 关闭**（`{workspace_id, sandbox_instance_id, node_id}`，存在 live sandbox/connected Node 时）。
- **G-011** starting 下 workspace/target 无效缺失的精确终态 — **部分（PARTIAL）**：fail-closed 路径（保持 `starting`，不写终态，重试循环再触达）已实现并测试（`runWorkspaceLive`/`TestAgentSessionStartSoftDeletedWorkspaceFailsClosed`）；choose-path 权威决策留待后续批准。Phase 4B 接管钩子仍 fail-closed 保真（不因测试方便写 `status=failed`）。

### G-012..G-016 — Phase 4 thread/running 缺口（Phase 4B 实现轮更新状态）

- **G-012 — PARTIAL（Phase 4B 后重新评估，未拆分）**：`node_executions` 迁移（0020）+ production `RecordDispatch`
  + `execution_work.execution_id` 栅栏（D-020/D-021，T4A-*）与 `node_event_receipts` 迁移（0021）+
  `TakeOverThreadEvents` + `ThreadEventsTakenOver` caller 均已实现（D-022，T4B-*）。**仍 PARTIAL**：其历史义务中
  仍有无 approved ADR 依据的部分（event 上限/保留策略，见 G-015）。本轮**不**拆分为 G-012a/G-012b，以保持历史清晰。
- **G-013 — CLOSED（已实现）**：会话开始权威形状已落地——首条真实 Node 记录接管、无 synthetic 事件、`thread_state`
  `pending→active`、echo 去重、空批次拒绝。**不依赖 proposed 的 controller-session ADR**。见 D-024；证据 T4B-1/2/10/12/13。
- **G-014 — CLOSED（已实现）**：`seq` 分配机制落地，**D-023 ACCEPTED**（`MAX(seq)+1`；串行化证明用 approved 事实）。
  见 §4B.5；证据 T4B-1/3/11/16。
- **G-015 — PARTIAL / NON-BLOCKING（依 mandate §3/§39 有意不 enforcement）**：初值存在（单运行 event 上限
  200,000、收据 `done` 后保留 30 天，D-025，§4B.10），但本轮**不**实现 `thread_event_cap` 上限与
  `node_event_receipts` 保留清理——无 approved ADR 给出具体值。迁移中保留 `node_event_receipts_retention` 索引以便
  未来 enforcement，不构成语义。
- **G-016 — CLOSED（Phase 4C 设计轮，2026-10-08；决策见 D-4C-01）**：字面值 `thread_state='pending'` 无写者。
  Thread D4 规定「执行已登记、尚无 Node 记录」= `pending`，但 A 侧在 `RecordDispatch` 写 `thread_state` 会违反 §11
  所有权（A 不写 B 的列），B 侧在接管前再写一次会违反 §29（一次转换）。因此 4B 内已登记但未激活的 run 该列为 `NULL`
  而非 `'pending'`；`pending→active` 的实际语义仍被证明（`TestThreadTakeoverActivatesPendingThread` 以字面 `'pending'`
  预置后断言精确变为 `active`）。**4C 决议**：选择**物化**——写者 = B-owned `StartSession` 的既有事务（与 seq=1 同事务、
  同谓词 `thread_state IS NULL`、断言 1 行受影响），另加幂等 backfill 迁移；否决 A 侧写入与读时派生。
  **本项决策已 CLOSED；但对应测试证据 T4C-1..T4C-5 仍为 `MISSING`（本轮不写测试）。**
- **G-017 — 新增 / OPEN / NON-BLOCKING / DEPENDENCY（Phase 4C 设计轮，2026-10-08）**：4C 的读取合同**超出**已批准
  Thread D5 的字面文本——无游标的 **tail** 读、`before` 游标、响应中的 `idleSince`，以及对 D4 的 `pending` 时点/含义
  澄清（D-4C-01/§4C.2、D-4C-02/§4C.3）。**为什么必须登记而非静默决定**：D5 只固定 `after={seq}&limit={n}` 升序，客户端
  无法在不从 seq=1 走完的情况下到达长 Thread 的尾部，而「本会话即将结束」的展示需要 `idle_since`；但把未记录的扩展
  当作已批准 ADR 落地，正是 plan 明令禁止的「静默偏离」。**WHAT IS NEEDED TO CLOSE**：修订 Thread D4/D5（`pending`
  物化时点与含义、无游标默认 = tail、`before`、`idleSince`、`nextCursor`/`prevCursor` 语义），或取得架构师对扩展的
  显式确认。**不是** 4C 其余部分的 blocker，但**对应代码在修订/确认前不应合并**。
- **G-018 — 新增 / OPEN / NON-BLOCKING（Phase 4C 设计轮，2026-10-08）**：ADR 生命周期表写着「用户点击『结束』⇒
  `ending`」，但**任何决策中都没有该端点**。权威已定（B 的生命周期转换，D-4C-10），缺的只是 API 形状，因此
  `user_ended` 触发在 4C 不可达，而 `idle_timeout` 与 `cancelled` 不受影响。**WHY 非阻塞**：空闲窗口与取消已经在走
  同一转换与同一个 `EndSession` seam。**WHAT IS NEEDED TO CLOSE**：定 route/verb/status/幂等（推荐
  `POST .../runs/{rid}/thread/end`，202 + 冲突 `thread_closed`，强制 `Idempotency-Key`）并落到 OpenAPI；必须与
  「取消运行」保持区分（D-4C-11）。
- **G-019 — 新增 / OPEN / NON-BLOCKING / DEPENDENCY（Phase 5）**：`ending → ended` 与 `queued → discarded` **只**由
  `SessionEnded`（会话终态 `TakeOverNodeEvent` → `sessionEnded` 钩子，controller-integration D6）写入，**未实现**。
  4C 设计该列、谓词与转换，但**不得**实现通往 `ended` 的第二条路径。**WHAT IS NEEDED TO CLOSE**：Phase 5 实现该
  钩子 + 交付并复用 `thread_entries.status` 与 `ending` 谓词；Thread 的「终态顺序」与「轮次结算」义务在此之前保持
  `Partial`/`Missing`。
  **更新（Phase 5 Batch 1，2026-10-08）**：**拆分为 `PARTIALLY CLOSED — Thread terminal takeover complete; delivery
  pipeline remains Phase 5 Batch 2`**。已实现并有直接证据：`agent_session_takeover` 权威接管（唯一入口
  `TakeOverNodeEvent`）、同事务 `ending → ended`（并覆盖 `pending | active | idle`）、`queued → discarded`、
  `running → delivering`、放出恰好一个未登记的 `deliver_revision` 工作项、收据与 `last_event_sequence` 推进、
  重放 no-op、任一步失败整体回滚、提交后恰好一条 `thread_changed`。**仍未关闭的部分**：交付流水线
  （`DeliverRevision` 投递、`DeliverySettled`、`releasing`、`RunWorkspaceDeleted`、`done`）属 **Phase 5 Batch 2**，
  因此本项**不得**整项标 `CLOSED`。
  **更新（Phase 5 Batch 2，2026-10-08）**：**`CLOSED`**。交付半边已同一轮落地并有直接证据：`deliver_revision` 工作项的
  认领与派发（kind 白名单在**共享派发谓词**上扩大）、`agent_delivery_takeover` 的权威接管（收据 + 结果 + 钩子 + 序号推进
  同事务）、D5 的失败重试与退避（30s × 2^(n-1) 封顶 10min，且**不产生新的逻辑交付**）、D5 的放弃（>2h 连续失败或运行
  Workspace 的 Node 未知 >30m）→ `delivering → releasing` + D4 派生 `status` + 同事务声明**恰好一个**删除意图、
  `RunWorkspaceDeleted` 的 `releasing → done`（重放 no-op、非 `releasing` 阶段拒绝）、以及两条周期补偿路径。证据：
  `integration/agent_run_delivery_test.go` 的 P5-7…P5-16 与 `internal/core/agent_run_release_db_test.go` 的 5 个白盒测试。
  **唯一未实现的部分不是本项的语义缺口，而是 Revision 登记本身**：Cloud Revision ADR 仍为 `status: proposed`，故
  `saved`/`unchanged` 一律 `UNAVAILABLE` + 零写入；该受限面**单独**登记为 **G-030**，不再挂在 G-019 上。

- **G-029 — `DEFERRED / NON-BLOCKING`（Phase 5 Batch 2 登记；Revision ADR Approval Round 裁定，2026-10-08）** —
  原分类 `OPEN / NON-BLOCKING / ADR PRECISION`：IssueRun D4 的结算规则含有
  「Agent 的回复注释」一类内容，但**没有**任何已批准的字段路径规定它写在哪一列/哪一对象。**WHY 登记而不自行选定**：
  字段路径属持久化契约，发明它等于静默扩展 ADR（authority order 禁止）。本轮因此**不写**该字段，`releasing` 结算只写
  D4 明确规定的 `phase`/`status`/`deliveryState` 与既有活动记录。**WHAT IS NEEDED TO CLOSE**：ADR 明确该注释的来源与落点，
  或删除该义务。**不阻塞**本轮完成。
  **裁定（Revision ADR Approval Round，2026-10-08）＝ Phase 5 的 `DEFERRED / NON-BLOCKING`，且不为它扩 schema。** 理由：
  ① 审计对象（Cloud Revision ADR）**不要求**它 —— D5 只承诺「公开读暴露元数据」，从未涉及 Agent 回复注释；
  ② `issue_runs.result` 的形状由 **approved** IssueRun D4 钉死为 `{revisionId | null, deliveryState}`，**没有**承载该注释的字段，
  为它加列/加字段属**未获批准的 schema 扩展**；
  ③ 该义务在数据上**已经由既有结构承载**：Agent 消息本身经 Thread 条目（`thread_entries`，`source = node|user|system`）
  与随 Revision 一并上传的会话 JSONL 落地，二者按 Thread D2 来自**同一份记录**；
  ④ 该注释若要成为一条「回复评论」，其 `kind` 词表属 desktop `ora-history` 领域，而 Thread **D2 明确禁止 Cloud 解析业务字段**
  （只校验是 JSON 对象且 `kind` 在已知集合内）——Cloud 侧自行发明落点会**同时**越界两处。
  ⇒ **结论**：本项**明确排除**在 Revision ADR 的批准范围之外；Phase 5 不为它扩 schema、不改 `issue_runs.result`、不加列。
  若产品确需该注释，另开一轮由 ADR 决定承载面。

- **G-030 — 新增 / OPEN / NON-BLOCKING FOR THIS ROUND, BUT THE APPROVED MANDATORY BLOCKER FOR THE REVISION HALF
  （Phase 5 Batch 2，2026-10-08）**：`specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`
  仍为 **`status: proposed`**，而它定义的正是 Revision 的对象键、`revision_ref`、Cloud 侧对象校验与 `GrantRevisionUpload`
  的授权形状。依 authority order（approved ADR > AGENTS.md > plan > implementation）与 `specs/AGENTS.md`（`proposed` 阶段
  不得据其编写契约与核心测试用例），**不得**据此实现。现状：无 `revisions` 表、0022 仍为最新 migration。
  **本轮的处理是受阻且不静默降级**：`DeliverySettled` 收到 `revision_delivered`/`revision_unchanged` 一律回 `UNAVAILABLE`
  且**零写入**，运行只能经 D5 放弃或 D6 无会话取消到达 `releasing`；**未**发明第二套 upload auth、**未**信任 Node 上报的
  tenant/run、**未**先产生外部副作用再补身份。**WHAT IS NEEDED TO CLOSE**：人类把该 ADR 评审为 `approved`，随后另开一轮
  实现注册、对象校验与上传授权。证据：`TestRevisionDeliveredIsRefusedAndTheRunStaysDelivering`。
  **更新（Revision ADR Approval Round，2026-10-08）：解除条件细化 —— 不能按现文本批准。** 本轮对该 ADR 做了
  approval-readiness 审计，结论 `REVISION_ADR_NOT_READY_FOR_APPROVAL`：存在 4 个**每一项都允许两种合理实现**的阻塞项，
  以及 3 条精度补充。**获批前实现等于猜。**
  - **B-1 —— Revision 行身份与幂等未定义。** ADR 列出 `revisions` 列，但**没有**规定：`id` 由谁在哪个事务内生成、
    唯一/幂等键是 `UNIQUE(run_id)` 还是 per-attempt、同一身份再次登记且负载不同时如何处置（拒绝/覆盖/取首次）。
    对照：**approved** 的 clone 路径对同一问题有**显式**冲突规则，Revision 路径一条都没有。**最小修订提案**：写明
    「`id` 由 Cloud 在交付接管事务内生成；唯一键 = `run_id`（一次运行至多一条 Revision，因为 `unchanged` 也登记）；
    已存在同键行时**不**覆盖，按已登记结果结算（确定性 no-op），负载冲突记日志不改变状态」。
  - **B-2 —— Object key 拼写不可落地。** ADR D2 写键为 `revisions/{tenantId}/{runId}/{deliveryWorkId}/revision.bundle|session.jsonl`，
    并说明「键在 Cloud 放出交付工作项时生成并写入工作项的输入」。但**交付工作项的 ID 由已冻结的 A seam 在创建时内部生成**
    （Phase 3B 封签了该签名），Cloud 调用方**拿不到**它用于拼键；Batch 1 的实现因此在同一处改用 Cloud 自己生成的
    per-attempt id（`newID()`），并在代码里留下「拼写对账是 Phase 5 Batch 2 的活」的注释 —— Batch 2 交付时**未**做该对账。
    ⇒ ADR 的字面拼写**不是**已交付实现所用的拼写。**最小修订提案**：把 D2 的键段改为 Cloud 生成的 per-attempt 交付尝试 ID
    （语义与 `deliveryWorkId` 相同，但**由 Cloud 生成并可回传**），并写明「同一次交付尝试内，工作项 ID 与键中的尝试 ID 同源可比」。
  - **B-3 —— `revision_ref` 拼写无 Cloud 权威。** Cloud ADR 未定义它的形状，proto 只给「必须在 `refs/ora/revisions/` 之下」，
    另一份**也是 `proposed`** 的 node revision ADR（D2.4）给出 `refs/ora/revisions/<runId>`。只读 Cloud ADR 的实现者可以
    合理地选别的拼写。**最小修订提案**：在 Cloud ADR 中直接写明 `revision_ref = "refs/ora/revisions/" + runId`（与实现一致），
    使 Cloud 侧不再依赖 node ADR。
  - **B-4 —— 「缺 `object_store` ⇒ `skipped`」与 approved IssueRun 不变量 3 冲突。** ADR D1 规定未配置对象存储时运行进入
    `delivering` 后直接 `deliveryState = skipped` 并进入 release。这**同时**有三个问题：① 它构成**第四个** `delete_workspace`
    触发条件，而 approved IssueRun **不变量 3** 只承认三种（Revision 已登记 / D5 明确放弃 / D6 无会话取消）；
    ② 它**复用**了已被 D6 无会话取消占用的 `skipped` 值，使公开读无法区分两种语义；③ 其触发点（在 Batch 1 的 session-ended
    事务内）**未规定**且该路径**未实现**。**最小修订提案**：把该情形改为「运行**不**进入 `delivering`，按 D6 的无会话取消语义
    结束」，或为「未配置对象存储」定义**新的** `deliveryState` 值并**同时**修订 IssueRun 不变量 3 —— 二者择一，不得两者都不做。
  - **精度补充（非阻塞，建议一并写入）**：**P-1** 明确对象校验**只**检查存在性、大小与 SHA-256，**不**检查 commit/base 关系、
    **不**检查 `revision_ref` 前缀、**不**检查 content-type/编码（归属由键的形状按构造保证）；**P-2** 明确同一 grant 的 TTL 内
    对**同一键**重复 `PUT` 的行为；**P-3** 明确 `unchanged` **不**复用既有 Revision（v1 没有「从 Revision 续跑」）。
  **本轮未改 ADR 正文、未改 `status`** —— 修订 approved/proposed ADR 需人类批准（mandate §10）。
  **更新（Revision ADR Decision & Amendment Round，2026-10-08）：B-1..B-4 与 P-1..P-3 已各自收敛为唯一方案，并已写入该 ADR 正文（文首与文末「修订记录」明确标识为待审批）。** 与上一轮提案相比有**两处实质变化**，需人类注意：
  - **B-1 的冲突处置改了**：上一轮提案是「已存在同键行时不覆盖、按已登记结果结算（确定性 no-op）、负载冲突记日志」；
    本轮改为「读回比较负载：**逐字段相同 = 幂等成功并返回既有 `id`；任一字段不同 = 不变式冲突** ⇒ 接管事务整体回滚、
    事件不确认、Node 重放」。理由：静默 no-op 会让一次**负载不同**的重放（例如 Node 换了 `final_commit`）被当成成功，
    而 **approved** 的 clone 路径对同一问题的规则是**冲突即回滚、结果不覆盖**；Revision 不应比 clone 更宽松。
    另写明：`revisions` 是 controller-integration D6 的**控制面**表 ⇒ 由控制面在接管事务内生成 `id` 并写行，
    随后同事务调用 `deliverySettled`；`PRIMARY KEY (id)` + `UNIQUE (run_id)`；`ON CONFLICT (run_id) DO NOTHING` + 读回；
    重放由 `node_event_receipts` 判定；行与阶段推进同事务 ⇒ 唯一允许的偏斜是「对象已上传而 Revision 未登记」。
  - **B-4 选了第三种方案**：上一轮给的是两个选项（① 按 D6 无会话取消语义结束；② 定义新 `deliveryState` 并同时修订不变量 3）。
    本轮**两个都不选**：① 会让「配置缺失」在公开读里表现为「用户取消」（`skipped`），语义失实；② 需要修订 approved
    不变量 3（等于承认第四条删除触发条件）并新增一个公开读取值。本轮改为**完全走 approved D5**：照常进入 `delivering`、
    照常放出交付工作项（D3/D6 的硬约束：不放会让运行停在既无工作项也无失败证据的 `delivering`）、拒发上传授权
    （`UNAVAILABLE`）、交付**确定失败**、D5 放弃窗口到期 ⇒ `deliveryState = failed` ⇒ `releasing`。
    **不新增任何删除触发条件、不新增 `deliveryState` 取值、不新增失败码**；`skipped` 归还 D6 的无会话取消专用。
    **不需要修改 IssueRun ADR**；唯一依赖是对 D5 的一处澄清（放弃窗口自运行的首个交付工作项起算）⇒ 新增 **G-033**，
    需人类单独批准。否决策略亦已写入 ADR 的「为什么不是这些替代方案」（含拒绝启动、派发前阻塞两条）。
  - B-2：第三段 = Cloud 生成的**尝试 ID**（**不是**工作项 ID —— 工作项 ID 由 A seam 在插入时才生成，调用方冻结输入时尚不可得）；
    新增「**逻辑交付 / 交付尝试 / Revision**」三种身份的区分表，并明确**不得**用 per-attempt 身份偷换逻辑交付身份
    （逻辑交付 = `run_id`；尝试 = 一个交付工作项/一次执行；Revision = 一行，`UNIQUE (run_id)`）。
  - B-3：`revision_ref` 由 Cloud 生成为 `refs/ora/revisions/<runId>` 并写入输入，Node 只回显、不构造不改写；
    它按**运行**命名 ⇒ Revision 的内容身份是行内 `final_commit` + 对象摘要，**不是**「ref 现在指向哪里」。
  - P-1..P-3 已全部写入 D4（校验范围＝HEAD 三项 + 本地输入一致性/形状比较，并明确**不**校验的五项；
    同一授权内重复 `PUT` 是覆盖写、不符声明即 `failed{verification_failed}` 并重试；`unchanged` 仍登记且不复用既有 Revision）。
  **`status` 仍为 `proposed`** —— 本轮只收敛文本，**未**自行批准。Node revision ADR 另补一句最小回显条款，
  也**需单独批准**（「Cloud Revision ADR 获批不代表 Node Revision ADR 自动获批」）。

- **G-031 — 新增 / OPEN / NON-BLOCKING / IDENTITY SPELLING DEVIATION（Phase 5 Batch 2，2026-10-08）**：IssueRun D6 把
  运行 Workspace 的幂等身份写作 `(issue_run_id, kind)`，而既有实现的唯一约束是 `(workspace_id, kind)`。在本轮全部路径上
  两者等价（运行 Workspace 与 run 一一对应、`kind` 固定），因此本轮**沿用既有 schema**，**未**新增 migration、
  **未**改写已应用约束（AGENTS.md 的兼容边界）。**WHAT IS NEEDED TO CLOSE**：ADR 与实现择一对齐（若确需 `issue_run_id`，
  须有独立 migration 与证据）。**不阻塞**本轮完成。
  **推荐方案（Revision ADR Approval Round，2026-10-08）＝ Option B：把 ADR 改为 `(workspace_id, kind)`，并限定「未完成的操作」。**
  理由三条：① **schema 事实**——`operations` 表**没有** `issue_run_id` 列，且它是 run Workspace 与**用户** Workspace 共用的表；
  按 ADR 字面加列会为一个纯幂等用途付费，并让用户 Workspace 的行多出一个恒为 NULL 的列。
  ② **等价性已被 approved 权威保证**——IssueRun **不变量 1**（run ↔ run Workspace 1:1）加上 `workspaces.issue_run_id` 的
  唯一约束，使 `(workspace_id, kind)` 与 `(issue_run_id, kind)` 在运行 Workspace 上**一一对应**；且既有 create 路径本就是
  **经由 binding**（`workspaces.issue_run_id IS NOT NULL`）识别 run Workspace，而不是靠 operations 表自带身份。
  ③ **决定性理由**：一个字面的 `UNIQUE(issue_run_id, kind)` 会**禁止重新声明**，而 operation **D4** 恰恰要求「未终态执行仍在时
  quiesce 失败 ⇒ 重新声明并重试」——该行为已被 `TestRefusedQuiesceIsRedeclaredAndStillReachesDone` 证明是 **approved** 的。
  因此措辞必须限定为「**未完成**（`state IN ('queued','running','retry_wait','blocked')`）的操作**至多一个**」，而这一限定
  正是实现已有的形状（`agent_run_workspace_release.go` 的未完成检查 + 迁移 0018 的既有唯一约束）。
  **本轮未改 schema、未改 ADR** —— 修订 approved ADR 需人类批准。
  **精确 amendment（Revision ADR Decision & Amendment Round，2026-10-08；待人类批准，本轮未应用，只输出提案）**：
  ① `specs/decisions/cloud/operation/20260928-plugin-step-and-run-workspace-release.md:73` —— 把
  「`(issue_run_id, kind)`，由 Cloud 生成，不经过公开幂等键」改为
  「`(workspace_id, kind)`，由 Cloud 生成，不经过公开幂等键；对同一个运行 Workspace，同一时刻至多存在一个
  **未完成**（`queued`/`running`/`retry_wait`/`blocked`）的该 `kind` 操作，已终结的操作不占用该身份，
  因此被拒绝 quiesce 后可以**重新声明**同一 `kind` 的操作」。
  ② `specs/decisions/cloud/controller-integration/20260928-agent-run-executions-thread-and-upload-grants.md:119` —— 把函数表中的
  「以 `(issue_run_id, kind)` 幂等地创建运行 Workspace 及其 operation」改为
  「以 `(workspace_id, kind)` 幂等地创建运行 Workspace 及其 operation（同一时刻至多一个未完成的同 `kind` 操作；
  运行 Workspace 与运行一一对应，故与按运行限定等价）」。
  ③ `specs/test-cases/cloud/operation/plugin-step-and-run-workspace.md:62` —— 同一句的用例侧复述同改
  （`specs/AGENTS.md`：ADR 变更须同步核心用例）。
  范围 = **两个 approved ADR 各一句 + 一个核心用例一句**；**不改 schema、不加列、不加索引**、不改任何 Go 代码
  （既有实现已是该语义：`(workspace_id, kind)` 的未完成检查由 `agent_run_workspace_release.go` 表达，
  数据库侧由既有 `one_project_operation` 传递性串行）。**是否再补一个数据库部分唯一索引是独立问题，不在本 amendment 内。**

- **G-032 — 新增 / OPEN / NON-BLOCKING / MISSING LIMIT（Phase 5 Batch 2，2026-10-08）**：D5 只给**交付**侧放弃窗口
  （连续失败 >2h、Node 未知 >30m）；**Workspace 删除**侧的持续失败没有上限，删除意图由
  `RedeclareRunWorkspaceDeletesOnce` 无限重新声明，运行会一直停在 `releasing`。这**不是**本轮引入的回归（本轮之前该路径
  根本不存在），但属同一生命周期的空缺。**WHAT IS NEEDED TO CLOSE**：后续 ADR 明确删除侧的重试上限与终态。
  **不阻塞**本轮完成。相关证据：`TestRefusedQuiesceIsRedeclaredAndStillReachesDone` 证明「重新声明」这一半边有效。
  **最小 proposal（Revision ADR Approval Round，2026-10-08）**：新增 IssueRun **D8「删除侧也有上限，且只以不可达为条件」**，
  只规定四件事 —— ① **重试来源**：沿用 operation **D4** 既有的 quiesce 失败重试与「同一 operation 的新执行」规则，Cloud 侧由
  删除重新声明保证任一时刻**至多一个**未完成的 `delete_workspace`（`RedeclareRunWorkspaceDeletesOnce` 已是该形状）；
  ② **上限**：只以 D5 已有的**不可达**窗口为准（运行 Workspace 的 Node 状态未知超过 `delivery_unreachable_after`，默认 30m）；
  D5 的 **2h「连续失败」窗口不适用于删除侧**，**Node 明确拒绝 quiesce 不触发放弃**（否则 P5-16 的恢复路径会被判死）；
  ③ **终态**：超过上限后运行进入 `done`，业务 `status` **保持不变**（`releasing` 时按 D4 派生写入的值），另记
  `failure_reason = workspace_unavailable`（沿用 D3 对 create_workspace 失败的既有取值），`delete_workspace` operation 以失败码终结；
  ④ **不留在 `releasing`**：`done` 是唯一终态，且 `done` **不**表示成功（D4「终态不变」）。**不谎报删除**：未真正删除的 Workspace
  保持存在、operation 标 `failed`，绝不声称删除已发生。**本轮未实现**（规范轮）。
  **精确 amendment（Revision ADR Decision & Amendment Round，2026-10-08；待人类批准，本轮未应用）**：在 IssueRun ADR
  新增 **D8** 并只改 D3 阶段表的 `done` 行 —— ① `done` 的进入条件由「`delete_workspace` `succeeded`」改为
  「`delete_workspace` `succeeded`，或删除侧超过 D8 的上限（此时 `done` **不代表**删除成功）」；② D8 正文四件事：
  **重试来源**（沿用 operation D4 的 quiesce 拒绝重试与「同一 operation 的新执行」，Cloud 侧由删除重新声明保证
  任一时刻至多一个未完成的 `delete_workspace`）、**上限**（只取 D5 已有的不可达窗口，证据是 **Cloud 自己观察到的**
  该 Workspace 的 Node 状态，与 D5 放弃窗口 2 同一证据，**不依赖 Node 上报**；D5 的 2h 窗口**不**适用于删除侧，
  **Node 明确拒绝 quiesce 不触发放弃**）、**终态**（operation 以既有失败码终结，运行进入 `done`，`status` **保持不变**，
  另记 `failure_reason = workspace_unavailable`，沿用 D3 对 `create_workspace` 失败的既有取值）、
  **残留登记**（运行 Workspace 行仍是活的、`issue_run_id` 仍指向该运行，且存在一个处于**终结失败态**的
  `delete_workspace` operation —— **不新增列、不新增表、不发明后台清理机制**）。
  ③ 同一次变更必须同步核心用例 `specs/test-cases/cloud/issue-run/agent-run-orchestration.md` 中把 `done` 定义为
  「delete 到达 `succeeded`」的两处，并登记新验证义务「超上限 ⇒ `done` + 残留 Workspace」（当前 `Missing`）。
  **明确登记的剩余风险**：超上限后运行 Workspace 及其容器/进程/卷**不会被自动回收**，且运行 Workspace 在公开 API 上
  不可达（operation D4：公开 `delete` 恒 404）⇒ **没有自助重清路径**，只能由人类/运维处置，或由后续专用决策发明机制；
  `failure_reason = workspace_unavailable` 是唯一可见信号。**不谎报删除成功**：`done` 只表示「不再重试」，
  删除是否真的发生由 Workspace 行与 operation 的终态如实体现。

- **G-033 — 新增 / OPEN / 跨 ADR 澄清（Revision ADR Decision & Amendment Round，2026-10-08）**：approved 的 IssueRun D5 写的是
  「交付连续失败超过 2 小时」，而**缺 `object_store`** 的交付**永远不会产生任何失败结果**（Cloud 拒发授权，Node 连授权都没收到）。
  本轮 B-4 的收口依赖对 D5 的一处读法：**放弃窗口自该运行的首个 `deliver_revision` 工作项被放出起算**，即「连续失败」包含
  「从未产生任何结果」。该读法已在 Phase 5 Batch 2 的实现中固化（`deliveryGivenUp` 窗口 1 以首个交付工作项的 `created_at` 起算），
  但**不是** D5 的字面文本 ⇒ **本轮不据此改写 approved ADR**，只登记为独立待批准项。**精确 amendment**：在 D5 的放弃窗口处补一句
  「窗口自该运行的首个交付工作项被放出起算，因此一个从未取得上传授权、从未产生任何交付结果的运行同样收敛
  （不需要为「能力未配置」发明失败码或新的 `deliveryState` 取值）」。**为什么必须单独批准**：它是对 approved ADR 的文本修改，
  且 B-4「不新增第四条删除触发条件」这一结论建立在该澄清之上。**不阻塞**本轮收敛结论，但**阻塞** B-4 的最终批准
  （人类可与其他项一并批准）。

- **G-034 — 新增 / OPEN / NON-BLOCKING / 生命周期残留（Revision ADR Decision & Amendment Round，2026-10-08）**：交付工作项在放弃后
  **不会被清理**——`execution_work` 行在**全仓没有任何删除者**（无 `DELETE FROM execution_work`），而认领路径
  （`agent_work_claim`/`agent_work_get`/`agent_work_pending`）**没有运行阶段过滤**，因此一个已经 `releasing`/`done` 的运行的
  **未登记** `deliver_revision` 工作项仍可被认领并派发。终态结果本身无害（`deliverySettled` 在 `phase != 'delivering'` 时是
  确定性 no-op、零写入），但① 白白消耗一次执行，② 该执行可能对着正在被删除的 Workspace 跑，③ 会留下一条永不完结的执行记录。
  **本轮不修**：属实现行为，且修复形状取决于 B-4 与 G-032 的最终批准结果（在认领侧加运行阶段谓词，或在放弃/终态时终结未登记的工作项）。
  **不阻塞**任何批准项。

- **G-027 — 新增 / OPEN / NON-BLOCKING / ADR PRECISION（Phase 5 Batch 1，2026-10-08）**：Thread D4 的末行只写「会话执行终态被
  接管 ⇒ `ended`」，**未**写明该转换的**活状态集合**，也**未**写明终态事件落在已 `ended` 或非会话阶段时的行为。本轮依据
  D4 不变量 4 的**无条件**措辞 + IssueRun D3 的 `delivering` 进入条件「任何结束原因」，接受 `pending | active | idle | ending`，
  其余（已 `ended`/`delivering`、`provisioning`、`releasing`）按 invariant violation 处理（`UNAVAILABLE`、零写入、Node 重放）。
  **WHY 登记而不自行改 ADR**：ADR 修订需人类批准。**WHAT IS NEEDED TO CLOSE**：在 Thread D4 或 controller-integration D6 写明
  活状态集合与该前置行为。**不阻塞**本轮完成；证据见 `TestSessionEndedPreconditionMatrix`。

- **G-028 — 新增 / OPEN / NON-BLOCKING / PRE-EXISTING LINT BASELINE（Phase 5 Batch 1 观察到，2026-10-08）**：
  `internal/core/space_agents.go` 的 `activeSpaceAgentRoster` **无任何调用者**（全仓含 HEAD 零引用），`task lint` 因此报
  `unused` 1 条。该函数在 `73c2aa4` 引入时即无调用者，**不是**本轮产物：在 `git archive HEAD` 的未改动树上用同一
  `.golangci.yml` 运行 `golangci-lint` 得到**逐字相同**的 1 条。它与 `internal/core/agent_target.go` 的租户级 roster 读重叠。
  **WHY 本轮不清理**：删除他人已文档化的读取助手属 Batch 1 范围外的行为变更；加 `//nolint` 等于弱化门禁（AGENTS.md 禁止）；
  接线为公开读取面则是新 API。**WHAT IS NEEDED TO CLOSE**：由拥有 Space Agent roster 读取面的轮次决定接线或删除。
  **不阻塞**本轮完成，但**是本仓 `task lint` 全绿的唯一剩余项**。
- **G-020 — 新增 / OPEN / NON-BLOCKING / DEPENDENCY（Phase 4C 设计轮，2026-10-08）**：SSE 失效提示**不保证送达**——
  hub 为进程内、缓冲 8、单实例，可能丢/重/乱序，无持久化、无 replay（api-boundary ADR，已实现）。**WHY 必须登记**：
  假定「可靠通知」的设计会诱导客户端把流当日志。**WHAT IS NEEDED TO CLOSE**：slice 1 依赖客户端重连 + 周期轮询
  （前端义务）；多实例投递需在同一 Publish/Subscribe 边界后换成 broker，并另行决策。
- **G-021 — 新增 / OPEN / NON-BLOCKING（Phase 4C 设计轮，2026-10-08）**：多 Controller worker 下 `thread_commands`
  的**投递分区**由 controller-integration ADR 自己列为未决。4C 的 seam 与表**不改变**该问题，只保证单 worker 下的
  「至少投递一次 + 登记幂等」。**WHAT IS NEEDED TO CLOSE**：另行决策（按 run 分区 / 租约归属）。

### G-022..G-024 — Phase 4C 就绪评审发现的设计冲突（2026-10-08，按 mandate §4 先登记、不静默修改）

- **G-022 — CLOSED BY A1**（2026-10-08 人类批准 A1 后关闭；原分类 `OPEN / NON-BLOCKING / 需 ADR 修订`）：`thread_entries.status`（`queued|delivered|discarded`）为
  D-4C-01/04/05/09 所需，但 Thread **D1 逐项列举了 `thread_entries` 的列，其中没有 `status`**（`source`/`kind`/
  `record jsonb`/`turn_id`/`node_execution_id`/`node_sequence`/`created_at`）；D3 **提到**了这三个状态却从未把列放进
  D1 的 schema，0018 也已按 D1 建表。**WHY 需登记**：给已列举的 schema 加列属持久化 schema 变更，§14 归类为架构性变更。
  **WHY 非阻塞**：语义已由 D3 + 不变量 4 批准，缺的只是列在 D1 中的存在性；且当前不存在 `source='user'` 行的写者，
  迁移的 CHECK 平凡成立。**WHAT IS NEEDED TO CLOSE**：`plan.md` §4R.7 的修订 A1；此外 0022 **不得依赖**「碰巧没有
  数据」（加 CHECK 前先对既有 `source='user'` 行回填 `status='queued'`，或显式断言该前提）。
  **更新（Phase 4C Slice 1 实施轮，2026-10-08）**：该条件的**迁移侧**已满足——`0022_thread_api_and_commands.sql`
  加列、**在加 CHECK 之前**对既有 `source='user'` 行回填 `status='queued'`，T4C-5 在「数据库里已存在这样一行」的
  升级路径上验证通过。缺口本身**仍 OPEN**：它说的是 D1 的枚举，修订 A1 仍为待批准；**未改任何 ADR 文件、未改任何 `status`**。
  **更新（ADR Approval Decision 轮，2026-10-08）**：人类**只**批准了 **A1**，修订已落入 Thread D1 的 `thread_entries`
  列清单与对应说明，**G-022 现为 `CLOSED BY A1`**。**只改 ADR 文本** —— `production behavior changed: NO`、
  `migration changed: NO`、`Phase 5 boundary unchanged`（`discarded` 仍只属 Phase 5）。A2 (G-024) / A3 (G-017) /
  A4 (G-023) / G-018 **未获批准**，仍为 `OPEN / READY FOR APPROVAL`。
- **G-023 — CLOSED BY A4（收口轮，2026-10-08；原分类 `OPEN / NON-BLOCKING / 需 ADR 修订`）**：D-4C-07/08 在**任何**改变该运行 Thread 可见状态的提交后发布
  `issue_run.thread_appended`，包括只翻转 `thread_entries.status` 或 `thread_state`、并未追加条目的提交；而 Thread
  **D5 写的是「条目写入事务提交后…发布」**，读起来只覆盖条目追加。**WHY 需登记**：事件名说的是 "appended"，客户端会
  依赖该契约。**WHY 非阻塞**：负载（`issueId`/`runId`/`lastSeq`）不变、事件只是失效提示，对客户端是加性的。
  **WHAT IS NEEDED TO CLOSE**：修订 A4；在落地前 S6 **只对追加了条目的提交**发布。
- **G-024 — CLOSED BY A2（收口轮，2026-10-08；原分类 `OPEN / NON-BLOCKING / 需 ADR 修订`）**：D-4C-03 在 `cancel_requested_at IS NOT NULL` 时拒绝
  `POST .../thread/messages`，即使 `thread_state = 'pending'`；而 Thread **D3 写的是 `pending|active|idle` 时接受**，
  只把 `ending|ended` 列为拒绝情形。**WHY 需登记**：该冲突**可达**——`provisioning`/`starting` 阶段的取消会直接进入
  `releasing` 而**从不**写 `ending`（IssueRun D4/D6、D-4C-11），因此运行可能同时是 `thread_state='pending'` 与已取消；
  此时接受消息会持久化一条永不被执行的用户轮次。**WHY 非阻塞**：更严格的谓词才是正确行为，只是 D3 的枚举不完整。
  **WHAT IS NEEDED TO CLOSE**：修订 A2（把 D3 的接受谓词扩为「且 `cancel_requested_at IS NULL`、且 workspace live」），
  随后同步 S4 的接受矩阵行与 Thread 核心用例义务。
  **更新（Phase 4C 加速实施 Batch 1，2026-10-08）**：A2 仍**未获批准**，故这两行**有意未实现**——
  `agent_run_thread_message.go` 的 `requireThreadAccepting` 与 `thread_state` CAS **窄于** D-4C-03，且
  `TestThreadMessageCancelRowIsDeferred` 钉住该延后（今天「已取消 + Workspace 非活」的运行仍会被接受，A2 一落地该测试
  即转红）。S4 接受矩阵的**其余各行已实现**。缺口**保持 OPEN**；关闭它**不得**由本轮改任何 ADR 的 `status` 来完成。
  **更新（Phase 4C Final Completion Batch = 收口轮，2026-10-08）**：人类批准 **A2**，D3 的接受谓词已改为
  `cancel_requested_at IS NULL` **且** `thread_state ∈ pending|active|idle`，两种情况共用 `409 thread_closed`。
  实现同时收窄了**权威前置条件**（`requireThreadAccepting`）与写命令的 **CAS 谓词**（`AND cancel_requested_at IS NULL`），
  因此「取消先提交、写入后到」这一竞争也被拒；deferral 测试已删除，由
  `TestThreadMessageRejectsAfterCancellationRequested` 取代。**G-024 = `CLOSED BY A2`**；
  ADR 仍**未**声称取消立即终态、立即删 Workspace，`discarded` 仍只属 Phase 5。
  G-023 的关闭说明见其条目首行（A4 采用**新增** `issue_run.thread_changed`，不加宽 `thread_appended`）。

### G-025 — migration 清单文档债（Phase 4C Slice 1 实施轮发现，2026-10-08）

- **G-025 — OPEN / NON-BLOCKING / 纯文档**：`internal/core/migrations/README.md` 与 `README.en.md` 的迁移清单只到
  `0017_tenant_membership_and_join.sql`；`0018`–`0021`（Phase 3A/3B 与 4B）与 `0022`（4C Slice 1）全部缺失，
  「哪个迁移落了什么」的叙述落后六个文件。**WHY 需登记**：README 是 reviewer 理解 schema 历史的入口，而 §14 把持久化
  schema 变更归为架构性变更。**WHY 非阻塞**：权威记录是有序 SQL 文件与 `schema_migrations` 校验和，两者完整且由
  `CheckSchema` 严格校验；没有任何行为依赖该 README。**WHY 未在 Slice 1 修复**：该债比本轮早六个迁移，而 S1 范围只有
  `0022`；只补 `0022` 会让清单**看起来**是最新的、却仍漏 `0018`–`0021`。**WHAT IS NEEDED TO CLOSE**：用一轮把
  `0018`–`0022` 一起写进两个 README。

---

## 14. Plan 维护规则

### 14.1 实现前

Agent 必须确认：

```text
Current Phase:
Current Slice:
Current Scope:
Known Blockers:
```

如果本轮 request 与当前 phase 不一致：

- 不得自行扩大 scope
- 必须指出偏差
- 必要时先更新 plan

### 14.2 发现新问题时

#### 已批准 ADR 已有答案

更新对应章节，并在 Decision Log 中补充 implementation decision。

#### ADR 没有答案，但不影响 architecture

作为：

`IMPLEMENTATION CHOICE`

记录到 Decision Log。

#### 影响 ownership / state machine / schema / public contract / transaction boundary

必须记录到：

`Open Gaps / Decisions Needed`

得到明确结论前，不得静默实现。

### 14.3 每轮结束

Agent 必须更新：

- Phase 状态
- 本轮实际完成项
- 未完成项
- 新 Decision
- 新 Gap
- Test evidence
- Quality gate evidence
- 下一步

---

## 15. Current Execution Marker

### 当前 Phase

`Phase 5 — Revision ADR Decision & Amendment Round`

### 当前 Slice

`把 B-1..B-4 与 P-1..P-3 各自收敛为唯一方案并写入两个 proposed Revision ADR 的正文（明确标识为待审批），为 G-031 / G-032 输出精确的 amendment 提案，登记 G-033 / G-034，并更新 plan/plan.md 与 plan/plan-zh.md。决策与修订轮：不改生产代码、不改任何 ADR 的 status、不改任何 approved ADR 的已批准决策。`

### 当前状态

`REVISION_ADR_DECISIONS_READY_FOR_HUMAN_APPROVAL`
（本轮为 **DECISION & AMENDMENT ROUND（mandate §1–§11）**。**判定：`REVISION_ADR_DECISIONS_READY_FOR_HUMAN_APPROVAL`——B-1..B-4 与 P-1..P-3 各自只剩**一个**明确推荐方案；所有跨 ADR 变更已逐项独立列出；两个 `proposed` ADR 的 `status` **未**改动。**
**权威重读**：`cloud/AGENTS.md`、`specs/AGENTS.md`、`plan/plan.md`、`plan/plan-zh.md`、Cloud Revision ADR（**proposed**，修订对象）、
node revision ADR（**proposed**）、IssueRun ADR（approved，D3 阶段表 / D4 / D5 / D6 / 不变量 1–3）、
controller-integration ADR（approved，D1/D4/D6 含函数签名与钩子表）、operation ADR（approved，D4）、
现有 Phase 5 Batch 1/2 实现（`agent_run_session_settle.go` / `agent_run_delivery.go` / `agent_run_release.go` /
`agent_run_delivery_loop.go` / `agent_run_execution_work.go` / `agent_run_workspace_release.go`）、
proto `agent_executions.proto`、migration `0019_execution_work.sql` 与 `0001_core.sql`**全部重读**；
权威顺序 **approved ADR > AGENTS.md > plan > implementation**。开工前已检查两个仓库 Git 状态并保留全部未提交修改。
**本轮性质**：**未改**生产代码 / proto / migration / OpenAPI / frontend / 测试；**未改**任何 ADR 的 `status`；
**未**改写任何 approved ADR 的已批准决策（G-031 / G-032 只输出提案）；**未**自行批准、**未**实现 Revision 生产路径、**未**输出 `PHASE_5_COMPLETE`。
**B-1（Revision 行身份与幂等）**：`revisions` 是 controller-integration D6 的**控制面**表 ⇒ 控制面在交付终态接管事务内生成 `id`（uuid）并写行，
随后同事务调用 `deliverySettled` 并传入该 `revisionId`；`PRIMARY KEY (id)` + `UNIQUE (run_id)`；`INSERT ... ON CONFLICT (run_id) DO NOTHING` + 读回比较负载
（逐字段相同 = 幂等成功并返回既有 `id`；不同 = 不变式冲突 ⇒ 接管事务整体回滚、事件不确认、Node 重放）；重放由 `node_event_receipts` 判定；
行与收据/结果/阶段推进同事务 ⇒ 不存在孤儿行，唯一允许的偏斜是「对象已上传而 Revision 未登记」。
**B-2（对象键身份）**：第三段 = **Cloud 生成的尝试 ID**，因为工作项 ID 由 A seam 在插入时生成、调用方冻结工作项输入时尚不可得
（`EnqueueExecutionWork` 返回 id 但 input 是逐字写入的）；新增「逻辑交付 / 交付尝试 / Revision」三种身份表；两条性质 = 同尝试内键不变、跨尝试键永不相等；
**不**用 per-attempt 身份偷换逻辑交付身份。改 A seam 签名的方案已列入替代方案表并被否决。
**B-3（`revision_ref`）**：Cloud 是唯一生成者（`refs/ora/revisions/<runId>`，按**运行**命名）并写入工作项输入；Node 不构造、不改写、原样回显，
`base_commit` 同；⇒ Revision 的内容身份是行内 `final_commit` + 对象摘要，**不是**「ref 现在指向哪里」，恢复不读 ref。
**B-4（缺 `object_store`）**：**删除** D1 的「`skipped` 直接释放」路径；改为一律照常进入 `delivering`、照常放出交付工作项（D3/D6 硬约束）、
拒发授权（`UNAVAILABLE`）⇒ 交付**确定失败** ⇒ **完全由 approved D5 收口**（`deliveryState = failed`、`revisionId = null`、随放弃进入 `releasing`）。
**不新增第四条 `delete_workspace` 触发条件、不新增 `deliveryState` 取值、不新增失败码**；`skipped` 归还 D6 无会话取消专用；**不需要修改 IssueRun ADR**。
**P-1..P-3**：校验范围（HEAD 三项 + 本地输入一致性/形状比较，明确**不**校验 Git 谱系、bundle 可应用性、JSONL 可解析性、content-type/编码、ref 当前指向）、
同授权内重复 `PUT`（覆盖写；按声明摘要校验，不符 ⇒ `failed{verification_failed}` + 重试）、`unchanged` 仍登记且第一版不复用既有 Revision —— 均已写入 D4。
**跨 ADR 变更（逐项独立列出，本轮**均未应用**）**：① **Node Revision ADR** 补一句「结果原样回显输入的 `revision_ref`/`base_commit`」；
② **G-033** IssueRun D5 澄清一句（放弃窗口自首个交付工作项起算）；③ **G-031** operation D4 一句 + controller-integration D6 一句 + 核心用例一句；
④ **G-032** IssueRun D8 正文 + D3 阶段表 `done` 行 + 核心用例两处。**Cloud Revision ADR 获批不代表 Node Revision ADR 自动获批。**
**新登记**：**G-033**（跨 ADR 澄清，阻塞 B-4 的最终批准）、**G-034**（NON-BLOCKING 生命周期残留：放弃后未登记的 `deliver_revision` 工作项仍可被认领）。
**剩余风险披露**：G-032 超上限后运行 Workspace **不会被自动回收**且公开 API 不可达（`delete` 恒 404）⇒ 无自助重清路径，只能由人类/运维处置；
**未**发明任何后台清理机制；`done` 只表示「不再重试」，**不**谎报删除成功。
**门禁**：`git diff --check`（cloud 与 specs）**均干净**；两个仓库 `git status --short` 已复核，既有未提交工作**全部保留**；
本轮**未**运行测试（无生产代码/测试变更；本会话最后一次完整 `task test:race` 全绿之后 Go / proto / migration / OpenAPI / frontend 均无改动）。
**交付面清单**：`Production code changed: NO`；`proto changed: NO`；`migration changed: NO`；`OpenAPI/frontend changed: NO`；`tests changed: NO`；
`ADR status changed: NO`；`self-approved: NO`；`Revision production behavior implemented: NO`；`staged: NO`；`committed: NO`；`pushed: NO`；`PR: NO`；`destructive git: NO`。
工作树中 `.gitignore` 的 4 行新增**不属本轮**：**未改、未回滚、不纳入本轮归属判断，仅披露**。
**供人类定稿的审批文本**：见本轮报告的「人类审批清单」——逐项给出文件、决策与**精确批准文本**（5 项：Cloud Revision ADR、Node Revision ADR、G-033、G-031、G-032）。
**下一步**：人类逐项裁决；全部获批后才进入 **Revision Completion Slice**（`object_store` 配置 + 对象存储客户端 + `revisions` 迁移 + `GrantRevisionUpload` + 交付成功/校验失败分支 + 两个 ADR 的核心用例）。）

**上一轮（Revision ADR Approval Round — G-030 approval-readiness audit，保留记录）：**

### 上一轮 Phase

`Revision ADR Approval Round — 解除 G-030 并冻结 Phase 5 最终契约`

### 上一轮 Slice

`对 status: proposed 的 Cloud Revision ADR 做 approval-readiness 审计（按 Revision identity / Object identity / Upload grant / Verification / Registration / revision_unchanged / revision_delivered 逐项 YES/NO），输出最小、明确、可批准的 Revision contract 与精确的人类审批文本，并裁定 G-029 / G-031 / G-032。规范轮：只改 plan/plan.md 与 plan/plan-zh.md。`

### 上一轮状态

`REVISION_ADR_APPROVAL_BLOCKED`
（本轮为 **NORMATIVE / APPROVAL-READINESS ROUND（mandate §1–§14）**。**判定：`REVISION_ADR_APPROVAL_BLOCKED`——审计结论 `REVISION_ADR_NOT_READY_FOR_APPROVAL`。**
**权威重读**：`cloud/AGENTS.md`、`specs/AGENTS.md`、`plan/plan.md`、`plan/plan-zh.md`、IssueRun ADR（approved，D3/D4/D5/D6/不变量 1–3）、
controller-integration ADR（approved，D1/D4/D6）、operation ADR（approved，D4）、Thread ADR（approved，D1/D2/D5）、
Cloud Revision ADR（**proposed**，审计对象）、node revision ADR（**proposed**，D2.4）、
`cloud/proto/ora/cloud/internal/v1/agent_executions.proto`、以及 Batch 2 既有的交付/删除实现**全部重读**
（`authoritative constraints re-read: yes`）；权威顺序 **approved ADR > AGENTS.md > plan > implementation**。
**本轮性质**：**未改生产代码 / proto / migration / OpenAPI / frontend**，**未改任何 ADR 文件**（含**未**改 `status: proposed`），
**未**自行批准、**未**把 `proposed` 当作已批准、**未**实现 Revision production behavior。
**审计结论（`REVISION_ADR_NOT_READY_FOR_APPROVAL`）**：4 个阻塞项——**B-1** Revision 行身份未定义（`id` 由谁在哪个事务生成、
唯一/幂等键是 `UNIQUE(run_id)` 还是 per-attempt、同键不同负载如何处置）——而 approved 的 clone 路径对同一问题有显式冲突规则，
Revision 路径**一条都没有**；**B-2** Object key 拼写（ADR 写 `{deliveryWorkId}`，实现在放交付工作项时用的是 Cloud 自己生成的
per-attempt `newID()`，且工作项 ID 由已冻结的 A seam 内部生成不可回传）；**B-3** `revision_ref` 的拼写只有 proto
前缀约束与 **node** 的 `proposed` ADR（D2.4）给出，**Cloud** ADR 未定义（实现取了 `refs/ora/revisions/<runId>`）；
**B-4** D1 的「缺 `object_store` ⇒ 运行直接 `deliveryState = skipped` 到 release」既是**第四个** `delete_workspace` 触发条件
（与 approved IssueRun **不变量 3** 冲突），又**复用**了已被 D6 无会话取消占用的 `skipped`，且触发点（在 Batch 1 的
session-ended 事务内）未规定、路径未实现。另有 3 条精度补充（P-1 校验范围只含存在性/大小/SHA-256；P-2 同一 grant TTL 内重复
`PUT` 同一键未规定；P-3 `unchanged` 不复用既有 Revision）。**这不是实现缺陷，而是契约不可批准**——每一项都允许两种合理实现，
获批前实现即等于**猜**。
**G-029 裁定**：`DEFERRED / NON-BLOCKING`（Revision ADR 本身不要求它；`issue_runs.result` 无对应字段；Thread 条目与会话 JSONL
已承载 Agent 消息；`kind` 词表属 desktop `ora-history`，而 Thread D2 禁止 Cloud 解析业务字段）⇒ **不为其扩 schema**，并**明确排除**
在 Revision ADR 批准范围之外。
**G-031 推荐**：**Option B** —— 把 controller-integration D6 的幂等身份由字面 `(issue_run_id, kind)` 改为 `(workspace_id, kind)`
并限定「未完成的操作」：`operations` 表**没有** `issue_run_id` 列且与用户 Workspace 共用；等价性由 approved 不变量 1
（run ↔ run Workspace 1:1）+ `workspaces.issue_run_id UNIQUE` 保证，且 create 路径本就**通过 binding** 识别 run Workspace；
决定性理由是一个字面的 `UNIQUE(issue_run_id, kind)` 会**禁止** operation D4 要求的「重新声明」
（已被 `TestRefusedQuiesceIsRedeclaredAndStillReachesDone` 证明）。**本轮未改 schema、未改 ADR。**
**G-032 最小 proposal**：新增 IssueRun **D8**（重试来源 = 既有 operation D4 的 quiesce 失败重试 + 任一时刻至多一个未完成
`delete_workspace`；上限只取 D5 的**不可达**窗口 `delivery_unreachable_after`（默认 30m），2h 连续失败窗口**不**适用于删除侧，
Node 明确拒绝 quiesce **不**触发放弃；终态 = `done` + 业务 `status` 不变 + `failure_reason = workspace_unavailable`，
**不**留在 `releasing`）。**未实现。**
**门禁与披露**：`git diff --check`（cloud 与 specs）干净，两个仓库 `status` 已复核；本轮**未**运行 Go 全量测试（无生产代码变更）。
工作树中 `.gitignore` 的 4 行新增（`plan/`、`plan/plan.md`、`plan/plan-zh.md`）**不属本轮**：**未改、未回滚、不纳入本轮归属判断，仅披露**。
**未提交**：无 stage / commit / push / PR / 破坏性 git 操作，cloud 与 specs 的既有未提交修改原样保留。
**交付面清单**：`Production code changed: NO`；`proto changed: NO`；`migration changed: NO`；`OpenAPI/frontend changed: NO`；
`ADR text changed: NO`；`ADR status changed: NO`；`self-approved: NO`；`Revision production behavior implemented: NO`；
`staged: NO`；`committed: NO`；`pushed: NO`；`PR: NO`；`destructive git: NO`。
**供人类定稿的审批文本（本轮未应用）**：本轮判定为 **NOT-ready**，故建议人类**先**按 B-1..B-4 的最小修订提案修订 ADR 正文
（建议一并写入 P-1..P-3），**再**使用 READY 分支的一句话批准 —— 「我批准 `…/0-cloud-owned-object-store-and-verified-revisions.md`
在按 B-1..B-4 修订后成为 `approved`；**并**同时批准 **G-031 的 Option B**（`(workspace_id, kind)` + 限定未完成操作）
与 **G-032 的 D8**（删除侧只以不可达为放弃条件，终态 `done` + `status` 不变 + `failure_reason = workspace_unavailable`）；
**不包括** G-029（`DEFERRED / NON-BLOCKING`，不为其扩 schema）」。若不拟一并批准，**必须**在批准文本中逐项显式写「不包括」——
不得含糊。**本轮未写入任何 ADR 文件、未改 `status`**。
**下一步**：人类按本轮给出的审批文本裁决；获批（且 B-1..B-4 按提案收敛）后才进入 Revision 实现切片。）

**再上一轮（Phase 5 Batch 2 — Delivery → Releasing → Done，保留记录）：**

### 再上一轮 Phase

`Phase 5 Batch 2 — Delivery → Releasing → Done`

### 再上一轮 Slice

`实现 deliver_revision 工作项的认领与派发、交付终态经 agent_delivery_takeover 的权威接管与结算、D5 的失败重试/退避/放弃、delivering → releasing 与同事务的删除意图声明、RunWorkspaceDeleted 的 releasing → done、两条周期补偿路径，并执行 Phase 5 Final Gate。`

### 再上一轮状态

`PHASE_5_BATCH_2_BLOCKED`
（本轮为 **IMPLEMENTATION ROUND（Phase 5 Batch 2，mandate §1–§40）**。**判定：`PHASE_5_BATCH_2_BLOCKED`——存在 1 项已批准的 mandatory blocker（G-030）。**
**权威重读**：`cloud/AGENTS.md`、`plan/plan.md`、`plan/plan-zh.md`、`specs/AGENTS.md`、IssueRun ADR（D3/D4/D5/D6）、
controller-integration ADR（D2/D6）、Thread ADR、Batch 1 实现与测试、既有 revision / upload-grant / workspace-deletion 实现、
proto **全部重读**（`authoritative constraints re-read: yes`）；权威顺序 approved ADR > AGENTS.md > plan > implementation。
**受阻判定（唯一 mandatory blocker）**：`specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`
仍为 **`status: proposed`**，而 `specs/AGENTS.md` 规定 `proposed` 阶段不得据其编写契约与核心测试用例 ⇒
Revision 登记、对象校验与 `GrantRevisionUpload` **不得实现**。本轮的处理是**受阻且不静默降级**：`DeliverySettled` 收到
`revision_delivered`/`revision_unchanged` 一律回 `UNAVAILABLE` 且**零写入**（运行只能经 D5 放弃或 D6 无会话取消到达
`releasing`）；**未**发明第二套 upload auth、**未**信任 Node 上报的 tenant/run、**未**先产生外部副作用再补身份、
**未**把 `done` 自动解释为 `status=completed`。
**交付执行与派发**：`agent_work_claim`/`agent_work_dispatch` 的 kind 白名单在**共享派发谓词**上扩为
`('agent_session','deliver_revision')`（恢复读路径 `agent_work_get`/`agent_work_pending` 未加过滤）；
`DeliverRevisionSpec` 由持久化 Cloud 状态构造，**未**把 Thread seq / command_id / turn_id 当作交付执行身份。
**接管事务边界**：交付终态只经既有 `ExecutionService.TakeOverNodeEvent` → 控制动作 `agent_delivery_takeover`
（**未**新增 public endpoint / route 直接写 delivery state）；形状与 Batch 1 一致：`BEGIN` → 身份/重放/缺口/冲突校验 →
`INSERT node_event_receipts` → `UPDATE node_executions.result` → `DeliverySettled` 钩子 → fenced `last_event_sequence` 推进 →
`COMMIT`；任一步失败 `panic(databaseFailure{err})` 整体 rollback。
**结算决策顺序**：身份校验 → 未知 kind 拒绝 → `phase != 'delivering'` 确定性 no-op → `saved`/`unchanged` 受阻拒绝 →
`failed` reason 闭集校验 → `deliveryGivenUp` → `releaseAfterDelivery`，否则按 D5 退避放出**同一逻辑交付**的
`deliver_revision`（重试不创建新的逻辑交付）。
**重试与放弃（严格取自 D5，未自造数字）**：退避 `30s × 2^(n-1)` 封顶 `10min`；放弃条件为连续失败 >2h 或运行 Workspace 的
Node 未知 >30m；两个窗口是**部署级时长**（`delivery_give_up_after` 默认 2h、`delivery_unreachable_after` 默认 30m，
含 `CLOUD_ISSUE_RUNS_*` 覆盖），已在 `configs/config.yaml` 记录；判定使用**数据库时间**。
**释放与终态**：`releaseAfterDelivery` 在**同一事务**内 CAS `delivering → releasing`、写 D4 派生的 `status`、`revisionId = NULL`、
追加活动记录，并声明**恰好一个** `delete_workspace` 意图（**不得**在事务内调用 workspace provider，事务只声明 work）；
`RunWorkspaceDeleted` 把 `releasing → done`（`done` 重放 no-op；`provisioning`/`starting`/`running`/`delivering` 与未知 run 被拒），
终态失败路径同样是 `done` + `status=failed`（D4「终态不变」）。**补偿非权威**：两条 `pluginmarket.RunSyncLoop` 周期任务
（`agentDeliveryInterval = 10s`）驱动 `GiveUpStaleDeliveriesOnce` 与 `RedeclareRunWorkspaceDeletesOnce`，两者只读 PostgreSQL
并复用既有事务路径，**不是**内存队列权威。
**测试**：新增 `integration/agent_run_delivery_test.go`（P5-7…P5-16，真实 gRPC + 真实 PostgreSQL + 真实公开 HTTP）与
`internal/core/agent_run_release_db_test.go`（5 个白盒：三行 `status` 的 `releasing → done` 且版本 +1、重放 no-op、
四个不可有成功删除的阶段被拒、未知 run 被拒、退避数字 {0,1}→30s、2→60s、3→120s、4→240s、5→480s、{6,7,16,1000}→600s）。
**缺口**：**G-019 = `CLOSED`**；新增 **G-029**（D4 的回复注释无批准字段路径）、**G-030**（上述受阻面，本轮唯一 mandatory blocker）、
**G-031**（D6 的幂等身份拼写 `(issue_run_id, kind)` 与实现的 `(workspace_id, kind)` 偏差，**未**改 schema）、
**G-032**（`releasing` 侧删除失败无放弃上限）；G-020/G-021/G-025/G-026/G-027/G-028 **状态未变**
（G-027 仍为 `OPEN / DOCUMENTATION CLARIFICATION`，**未**自行改 ADR；G-028 仍为 `PRE-EXISTING BASELINE`）。
**门禁**：`format:check` exit 0；`lint` exit 201 且**仅剩 1 条 PRE-EXISTING BASELINE**（G-028，在 `git archive HEAD` 的
**未改动树**上逐字相同 ⇒ Phase 5 引入 0 条回归；本轮自身出现的 1 条 G115 已按**修复根因**处理，非 `nolint`、非删除）；
`build` exit 0；`test` exit 0（**0 FAIL / 0 SKIP**）；`test:race` exit 0（**0 DATA RACE**）；`git diff --check`（cloud 与 specs）干净；
OpenAPI 与前端生成物**未变**（无 REST 形状变化），故未运行 `frontend:generate`/`frontend:check`。
**未改**：proto、任何 migration（0022 仍为最新，**未**新增、**未**修改已应用文件）、generated/OpenAPI、前端、
任何 ADR 的语义或 `status`、Phase 4B/4C/5B1 既有不变量与冻结回归。
**披露**：工作树中 `.gitignore` 有 4 行新增（`plan/`、`plan/plan.md`、`plan/plan-zh.md`），不属本轮文件集且与
`plan/*.md` 已被跟踪的事实冲突；**未**改动、**未**回滚（禁止破坏性 git），交由人类判定归属。
**未提交**：无 stage / commit / push / PR / 破坏性 git 操作，cloud 与 specs 的既有未提交修改原样保留。）

**更早轮次（Phase 5 Batch 1 — SessionEnded 终态接管，保留记录）：**

### 更早轮次 Phase（Phase 5 Batch 1）

`Phase 5 Batch 1 — SessionEnded 终态接管 → ending → ended → queued → discarded`

### 更早轮次状态（Phase 5 Batch 1）

`PHASE_5_BATCH_1_DONE`
（本轮为 **IMPLEMENTATION ROUND（Phase 5 Batch 1，mandate §1–§23）**。**判定：`PHASE_5_BATCH_1_DONE`——不存在已批准的 mandatory blocker。**
**权威重读**：`cloud/AGENTS.md`、`plan/plan.md`、`plan/plan-zh.md`、`specs/AGENTS.md`、IssueRun ADR、Thread ADR、
controller-integration ADR、Phase 4B 接管实现、Phase 4C S5/S7 实现、Final Completion Batch 测试**全部重读**；
权威顺序 approved ADR > AGENTS.md > plan > implementation。
**入口唯一合法性**：终态事件只经既有 `ExecutionService.TakeOverNodeEvent` → 控制动作 `agent_session_takeover` 进入
（controller-integration D2：终态事件仍走 `TakeOverNodeEvent`，**不经** `TakeOverThreadEvents` 批量路径）；**未**新增
endpoint / route / background job，**未**自造 event，**未**改 proto（`ExecutionResult.outcome` 字段 6 `AgentSessionEnded` +
`AgentSessionEndReason` 五值即已批准形状）。Phase 4B 的收据身份、重放、缺口、冲突、调用方自有事务、无嵌套事务全部保留。
**事务边界**：`BEGIN`（lease + submission）→ A 连续性/重放校验 → A `INSERT node_event_receipts` →
A `UPDATE node_executions.result` → B `AgentRunHooks.SessionEnded`（Thread `ended` + 丢弃 + `delivering` + 放出交付工作项）
→ A fenced `last_event_sequence` 推进 → `COMMIT` → 之后才允许 `EventAck`；任一步失败整体 rollback。
**状态矩阵判定（§9/§13）**：Thread D4 末行对**会话执行终态**无条件（不变量 4），IssueRun D3 的 `delivering` 进入条件为
「会话执行有终态结果（**任何结束原因**）」，故 `pending | active | idle | ending` **一律**接受并推进；ADR **未**给它们设前置条件，
因此**不是**「未规定 ⇒ invariant violation」。ADR 未覆盖的三种（已 `ended`/`delivering`、`provisioning`、`releasing`）按
invariant violation 处理：`UNAVAILABLE`、零收据、零序号推进、生命周期不变、Node 重放，**不**为容错自动变 `ended`。
**范围由 ADR 扩大**：IssueRun D3/D6 把「会话终态」与「`delivering`」规定为同一事务语义，故本轮实现包含
`running → delivering` 与交付工作项放出（mandate 的「Thread terminal only」默认边界**不适用**）；交付的**执行与结算**
（`DeliverRevision` 投递、`DeliverySettled`、`releasing`、`done`）**未**实现，留 Batch 2。
**事件**：提交后**恰好一条** `issue_run.thread_changed`（每 run/事务去重、commit 后、rollback 零事件、`lastSeq` = Thread 高水位）；
**未**发 `thread_appended`（不追加条目，**未**为发事件人工造条目）。
**测试**：新增 `integration/agent_run_session_end_test.go`（真实 gRPC + 真实 PostgreSQL + 真实 SSE/HTTP）：P5-1 活状态矩阵；
P5-2/P5-3 多 `queued` 全 `discarded` 且 `delivered` 不变；P5-4 两种重放全程 no-op；P5-5 四类拒绝零残留；
P5-6 钩子失败（软删运行 Workspace）全量回滚；§11 公开 GET 重读读回 `discarded` 且其余条目逐字节不变；
§13 三类非法状态完整 `UNAVAILABLE` 断言；§14 提交/回滚/重放三面通知；§15 `discarded` 只在会话终态出现；
§19 三个并发子用例（竞态 POST、同一终态并发双发、空闲扫描器抢占）。
**Phase 4C 回归**：全量 `go test ./...`（含 `integration`）通过，Thread POST / GET / `/thread/end` / 取消拒绝 / 分页 /
事件 / `queued→delivered` / `active⇄idle` / 空闲超时 / 取消 → `ending` / 命令投递全部未变。
**缺口**：**G-019 拆分为 `PARTIALLY CLOSED — Thread terminal takeover complete; delivery pipeline remains Phase 5 Batch 2`**；
新增 **G-027**（ADR 未写明活状态集合）与 **G-028**（`activeSpaceAgentRoster` 无调用者——**PRE-EXISTING** lint 基线）。
**门禁**：`format:check` exit 0；`lint` **仅剩 1 条 PRE-EXISTING BASELINE**（G-028，在 `git archive HEAD` 未改动树上逐字相同；
本轮新增 0 条；**未**加 `nolint`、**未**删除他人代码）；`build` exit 0；`test` 全绿（真实 PostgreSQL）；
`test:race` exit 0 / **0 DATA RACE**；`git diff --check`（cloud 与 specs）干净；OpenAPI 与前端生成物**未变**（无 REST 形状变化）。
**未改**：proto、任何 migration（0022 未动、无新 migration）、generated/OpenAPI、前端、任何 ADR 的语义或 `status`、
Phase 4B/4C 既有不变量。**未提交**：无 stage / commit / push / PR / 破坏性 git 操作，cloud 与 specs 的既有未提交修改原样保留。）

**更早轮次（Phase 4C COMPLETE — Final Completion Batch，保留记录）：**

### 更早轮次 Phase（保留记录）

`Phase 4C COMPLETE — Final Completion Batch（A2 / A3 / A4 / G-018 落地 + 最终验收）`

### 更早轮次 Slice（保留记录）

`Phase 4C Final Completion Batch：把人类已批准的 A2 / A3 / A4 / G-018 写入对应 approved ADR 正文并实现，随后做 Phase 4B + 4C 全量回归、Final Gate 与收口判定。`

### 更早轮次状态（Phase 4C COMPLETE，保留记录）

`PHASE_4C_COMPLETE`
（本轮为 **FINAL COMPLETION BATCH**：**判定 `PHASE_4C_COMPLETE` —— 不存在已批准的 mandatory blocker**。
**ADR**：A2 → **D3**、G-018 → **D4**、A3 与 A4 → **D5**；frontmatter `status` 保持 `approved`（**未改**）；**未新增 migration**、
**未改 0022**（四项都不需要 schema 变化）。**实现**：① Thread POST 接受谓词 = `cancel_requested_at IS NULL` **且**
`thread_state ∈ pending|active|idle`，两种情况共用 `409 thread_closed`，**权威前置条件与 CAS 谓词同时收窄**，
deferral 测试被正式的 `TestThreadMessageRejectsAfterCancellationRequested` 取代；② GET 的无游标取尾 / `before` /
`idleSince` / `nextCursor`·`prevCursor`（`after` 与 `before` 互斥 ⇒ `400 invalid_pagination`，游标恒为 Cloud Thread `seq`）；
③ 新增 `issue_run.thread_changed{issueId,runId,lastSeq}` 覆盖任何改变 Thread REST 表示的提交，**`thread_appended` 未加宽**；
④ `POST .../runs/{rid}/thread/end`（`Idempotency-Key` 必需、body `{}`、`202 {"threadState":"ending"}`、同键重放、
新键对 `ending|ended` ⇒ `409 thread_closed`、幂等预检先于生命周期校验），**只**推进到 `ending`，不写条目、不分配 `seq`、
不改 `phase`/`status`。**缺口**：**G-017 / G-018 / G-023 / G-024 = CLOSED**；**G-019 归 Phase 5**、
**G-020**（多实例 SSE）、**G-021**（多 worker 分区）、**G-025**（文档债）、**G-026**（`cancel_requested_at` 无生产写入者）
仍 **OPEN / NON-BLOCKING**；**0 项 BLOCKER**。
**门禁**：`format:check` exit 0；`lint` **仅剩 1 条 PRE-EXISTING BASELINE**（`internal/core/space_agents.go` 的
`activeSpaceAgentRoster` unused——文件逐字节未改、全仓含 HEAD 无引用、在 `git archive HEAD` 的未改动树上同样报出；
基线 7 条中另有 6 条随 format 修掉，**新增 0 条**，未加 `nolint`）；`build` exit 0；`test` **368 PASS / 0 FAIL**（真实 PostgreSQL，
含 `TestPublishedOpenAPIIsValidAndCurrent`）；`test:race` **368 PASS / 0 DATA RACE**；`frontend:generate` **幂等**
（三次 sha256 一致、无手改）；`npm run check` 全绿；`git diff --check`（cloud 与 specs）干净
⇒ **Phase 4C introduced gate regressions = 0**。`-count=10` 并发压力（Thread POST 同键、`/thread/end` 同键与四类竞争、
GET 并发追加、接管重放、核心生命周期）全绿且**零 `time.Sleep`**。
`task frontend:check` 只在 `git status --porcelain -- frontend/src/api` 一步失败，原因是生成物相对 HEAD **未提交**（本轮禁止 commit），
不是内容漂移。**未提交**：无 stage / commit / push / PR，cloud 与 specs 的既有未提交修改原样保留。**未进入 Phase 5。**）

**上一轮（Final Gate，保留记录）：**

`PHASE_4C_FINAL_GATE_CORE_DONE_WITH_APPROVAL_DEFERRED`
（本轮为 **FINAL ACCEPTANCE ROUND**（mandate §0–§24）：只做验收、回归、缺口分类与判定，**不扩展产品范围、不实现未批准 ADR 扩展、
不进入 Phase 5、不为得 PASS 弱化测试、不把 deferred 伪装成完成**。**判定：`PHASE_4C_CORE_COMPLETE_WITH_APPROVAL_DEFERRED`。**
`S1/S2a/S3/S4/S5/S6/S7` 的**已批准子集完整且各有直接证据**；下列项**需要批准后才能落地**，故**明确 deferred**而非「已完成」：
① **S2b**（Thread GET 的无游标取尾 / `before` / `idleSince` / `nextCursor`·`prevCursor`）→ 需 Thread ADR 修订 **A3**（**G-017**）；
② **`POST .../runs/{rid}/thread/end`**（`user_ended` 触发）→ 需 **G-018** 的端点形状批准；
③ **仅状态变化也发布** `issue_run.thread_appended` → 需 **A4**（**G-023**，现行为：只对「有新条目」的提交发布）；
④ **POST 在 `cancel_requested_at` 已置时拒绝** → 需 **A2**（**G-024**，现行为：与已批准 D3 字面一致，两条更严行**故意不实现**）；
⑤ **`thread_entries.status` 进入 D1 列清单** → 需 **A1**（**G-022**；迁移 0022 已按安全路径落地，语义已批准）。
**当前安全边界**：上述 ①–⑤ 对应代码**一行未写**；`ending → ended`、`queued → discarded`、`SessionEnded`、`delivery`/`deliver_revision`
**零实现**（Phase 5，G-019）；**A1–A4 全部未批准，未改任何 ADR 文件或其 `status`**。
**验收结果**：**Phase 4C introduced gate regressions = 0**；`format:check` / `lint` 的失败**全部为 PRE-EXISTING BASELINE**
（5 个 format 文件 + 6 个 lint 文件，`git diff --quiet HEAD` 全为真 ⇒ 与本轮无关，按 §24 不清理）；`build` / `test` / `test:race` 全绿、
**0 DATA RACE**；`-count=10` 并发压力两腿全绿且**零 sleep**；迁移「全新链 / 0021→0022 升级 / 重复 `Migrate` 幂等」全部通过；
`frontend:generate` 幂等、OpenAPI 纯增无删除；**G-016 CLOSED**，**G-017..G-026 逐条复核后状态不变**，
其中 **0 项 BLOCKER**、G-019 归 **Phase 5**、G-025 归**文档债**、其余 8 项 **OPEN / NON-BLOCKING**。
本轮**新增一个测试**（`TestOnlyTheFirstRecordAdvancesRunning`，证明 running 的唯一权威与其他路径的不推进性）并同步 specs 证据表；
**未修改任何生产行为**。**未提交**：无 stage / commit / push / PR，cloud 与 specs 的既有未提交修改原样保留。）

**ADR Approval Round（2026-10-08，规范收口轮，非实现轮）—— `ADR_APPROVAL_ROUND_READY_FOR_HUMAN_DECISION`：**

五项提案全部就绪、**等待人类决定**，本轮**未自行批准任何一项**、**未修改任何 approved ADR 文件或其 `status`**、
**未改生产代码 / migration / OpenAPI / frontend generated / proto**（记录见 `plan/plan.md` 的
`### Round: Phase 4C ADR Approval Round …`，完整 A1 修订文本亦在其中）：

| 项 | Gap | 状态 | 要点 |
|---|---|---|---|
| **A1** | G-022 | **APPROVED · APPLIED（2026-10-08，人类批准）** ⇒ G-022 `CLOSED BY A1` | D1 的 `thread_entries` 列清单补入 `status text`，语义与已批准 D3 逐条对齐（`source='user'` ⇒ `queued\|delivered\|discarded` 且恒非空；`source ∈ {node,system}` ⇒ `NULL`；echo 同事务 CAS `queued→delivered`；`discarded` 仍属 Phase 5）。**不新增任何行为** |
| **A2** | G-024 | **READY FOR APPROVAL** | `cancel_requested_at IS NOT NULL` 时新 Thread POST ⇒ `409 thread_closed`；接受谓词收窄为 `cancel_requested_at IS NULL AND thread_state ∈ {pending,active,idle}`。落地时 `TestThreadMessageCancelRowIsDeferred` 应转红并被重写 |
| **A3** | G-017 | **READY FOR APPROVAL** | GET v1：`after` 与 `before` 互斥、都不给 ⇒ tail、响应**始终**升序、`limit ≤ 500`、新增 `idleSince`/`nextCursor`/`prevCursor`；cursor 是 Cloud Thread `seq`。**不**含 snapshot token / 服务端 cursor 对象 / durable resume token；S2b 仍 `S2B_DEFERRED_PENDING_ADR` |
| **A4** | G-023 | **READY FOR APPROVAL** | 首选新增 `issue_run.thread_changed{issueId,runId,lastSeq}` 覆盖「任何改变 Thread REST 表示的提交」；备选是把既有 `thread_appended` 明确为历史名称的 invalidation hint。保留：仅提交后发布 / 回滚零事件 / 可有损 / 重复无害 / GET 为唯一权威 / 无内容 / 无 exactly-once。**不**解决 G-020 |
| **G-018** | G-018 | **READY FOR APPROVAL** | `POST .../runs/{rid}/thread/end` + `Idempotency-Key` + body `{}` ⇒ `202 {"threadState":"ending"}`；同事务 CAS `pending\|active\|idle → ending` 并 `enqueueThreadCommand(EndSession{user_ended})`；新 key 对已 `ending\|ended` ⇒ `409 thread_closed`；**不得** `ending→ended` |

建议批准顺序：**A1 → A2 → G-018 → A3 → A4**（A1 已有 schema 偏差须先修文档；A2/G-018 直接影响 lifecycle/API；A3/A4 为增强）。
跨提案一致性已核对：A1 不授权 `discarded`；A2（拒绝新消息）与 G-018（主动结束）不同；A3 cursor 与 A4 事件**不绑定**为同一 cursor；
A4 不解决多实例；G-018 只推进到 `ending`。

**ADR Approval Decision（2026-10-08，规范收口轮）—— 只落地 A1，退出标记 `ADR_APPROVAL_ROUND_A1_APPLIED`：**

人类**只批准 A1 / G-022**。A1 已按批准范围落入
`specs/decisions/cloud/thread/0-durable-agent-thread-with-user-turns.md` 的 **D1**：`thread_entries` 列清单补入 `status`，
并写明 `source='user'` ⇒ 恒非空且限于 `queued|delivered|discarded`、`source ∈ {node,system}` ⇒ `NULL`、Cloud 首次持久化写
`queued`、echo 同 `turn_id` 同事务 `queued→delivered`（不回退）、终态接管 `queued→discarded` **仍属 Phase 5**（本轮**未实现**），
并**显式区分** `thread_entries.status`（轮次）/ `issue_runs.thread_state`（Thread，D4）/ `thread_commands.delivered_at`
（命令投递登记）三者互不代偿。**G-022 由 `OPEN / READY FOR APPROVAL` 变为 `CLOSED BY A1`**；
**A2 (G-024) / A3 (G-017) / A4 (G-023) / G-018 保持 `OPEN / READY FOR APPROVAL`，未被自行批准、行为未变**。
本轮**只改 ADR 文本**：**未改** production Go 代码、migration 0022 或任何新 migration、OpenAPI、frontend generated output、
proto、任何其他 ADR 文件，**未改** Thread ADR 的 `status` 字段，**未把**「看起来不错 / 继续 / 可以」当批准。
**`production behavior changed: NO`**、**`migration changed: NO`**、**`Phase 5 boundary unchanged`**。
（完整记录见 `plan/plan.md` 的 `### Round: Phase 4C ADR Approval Decision …`。）

**上一轮（Batch 3，保留记录）：**

`PHASE_4C_ACCEL_BATCH_3_CORE_DONE_WITH_S2B_DEFERRED`
（本轮为 **ACCELERATED IMPLEMENTATION ROUND**：单 Coding Agent 按 mandate 顺序推进 **S2a → S6 → 条件性 S2b → 回归**，
中途不结束本轮。**S2a 完整** —— `GET /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread` **只**落地 Thread D5 的已批准子集：
`after={seq}`（严格 `seq > after`，`after=0` = 从头）、`limit={n}`（缺省 200 = `ThreadPageDefault`、上限 500 = `ThreadPageLimit`）、
条目按 `seq` **升序**、响应 `{items, threadState}`；`items` 与 `threadState` 出自**同一个** `Store.transact` 快照；
`threadState` 由 Thread 响应投影 `issue_runs.thread_state`，run 资源字段集**逐字节不变**（`stripAgentRunSkeleton` 语义保持）；
授权复用 Thread D3 的 Issue read（与 comments 同）——跨 tenant / 跨 Issue / 软删 run / 非 agent run / `thread_state IS NULL`
一律 `404`，非成员 `403`；**只读**：单次事务零写（不分配 `seq`、不写状态、不发事件）；条目投影只给
`seq/source/kind/record/createdAt`，`node_execution_id`/`node_sequence`/`run_id` 不上线（D-022 身份内部化），
`turnId`/`status` 只随 `source='user'`。**未实现**（属 **G-017**）：无游标取尾、`before`、`idleSince`、`nextCursor`/`prevCursor`、
`PublicRequest.Before`；`after` 缺省按「不新增语义」读作**从头**（= `after=0`），**不是** tail。**S6 完整** —— `SpaceEvent`
增量新增可选 `issueId`/`runId`/`lastSeq`（`omitempty`：4 个 4C 之前的事件形状**逐字节不变**），提交后发布
`issue_run.thread_appended`；hint 在**调用方事务**上排队（`transaction.appends`），由 `Store.transact` 在 `tx.Commit()`
返回 nil **之后**与既有两个 signal 并列释放，因此「回滚不发」是**结构性**的（不是每个调用点自觉）；**三个 entry 写入点**
（session 声明 `seq=1`、Thread POST、接管 hook）都发布，`lastSeq` = 该提交的 `MAX(seq)`（**高水位**，只表示「有数据在
seq ≤ lastSeq」，**不**推进客户端游标）；hint **不**携带条目内容或会话状态；space 解析不到就**丢弃** hint（不让通知失败污染
业务事务）。**仅状态变化零通知**：echo 的 `queued → delivered` 与 S7 的 `thread_state='ending'`（不写条目）**不**发布，
**T4C-26 不实现**（**G-023** 未批准），并用双向断言把边界钉住（状态变化是持久的、通知不被发明；将来实现 A4 必红）。
SSE 传输层与内存单实例 hub **未改**、**未**建 broker，**G-020** 保持 OPEN。**S2b 未实现** —— `decisions/cloud/thread/` 下
**只有**一个已批准 ADR，其 D5 不含 tail/`before`/`idleSince`/游标，**A3/A4 未批准**，故标记
**`S2B_DEFERRED_PENDING_ADR`**（该 deferred **不**否定已完成的 S2a/S6，T4C-6/T4C-8 保持未实现）。
**未改**：proto、migration、任何 ADR 文件或其 `status`、Phase 4B 的 running authority / `seq` 分配 / 收据 / 接管事务、
S3 命令所有权、S4 POST 幂等、S5 echo `delivered`、S7 停止在 `ending`、D6「缝在调用方事务内」。
Checkpoint：`S2A_IMPLEMENTATION_CHECKPOINT_REACHED`（S2a 定向测试 + run 资源形状回归 + 并发分页测试全绿后记录，**未 commit**），
随后继续 S6；`S6_IMPLEMENTATION_CHECKPOINT_REACHED`（S6 五条集成测试 + 事件字节形状白盒 + Phase 4B/4C 回归全绿后记录，**未 commit**）；
**S2b：`S2B_DEFERRED_PENDING_ADR`**（无代码、无 route、无 OpenAPI 变更）。
**G-017 / G-018 / G-019 / G-020 / G-021 / G-022 / G-023 / G-024 / G-025 / G-026 全部保持 OPEN 且未被静默关闭**。
**未提交**：无 stage / commit / push / PR，既有未提交修改原样保留。）
（本轮为 **ACCELERATED IMPLEMENTATION ROUND**：单 Coding Agent 连续推进 **S5 → S7**，中途不结束本轮。**S5 完整**：
用户轮次 echo 从「仅首提示」推广到所有 Cloud 生成的用户轮次——批次中某事件的 `turn_id` 命中已存在的
`source='user'`/`kind='user_turn'` 行时，接管事务只做 `queued → delivered` 的 CAS，**不**新增条目、**不**分配 seq、
**不**改写 `record`/`turn_id`、**不**新建命令、**不**重新入队；已 `delivered` 的行是相同内容的重放 ⇒ 逐字节不动，
`delivered` **永不回退**；首提示 echo（seq=1，`source='system'`）行为**不变**。批次后生命周期**只在**本事务提交了真实
Node 记录且事后权威重读为 `phase='running' AND status='running'` 时判定：`active` + 批次**末条有效记录**为 `turnEnded`
+ 无 `source='user' AND status='queued'` ⇒ `thread_state='idle'`、`idle_since = now()`（数据库时钟）；`idle` + 末条非
`turnEnded` ⇒ `thread_state='active'`、清 `idle_since`。判定依据是**末条有效记录**，**不是**「批中出现过 `turnEnded`」；
两条 CAS 的 0 行都按不变量损坏整批回滚。**S7 完整（除 G-018 门禁项）**：结束严格停止在 `thread_state='ending'`，且
**只在同一事务**内经 **A 缝**放出恰好一条 `EndSession{reason}`（业务层**不**写 `thread_commands`，`command_id` 由 A 生成）；
idle 窗口用 `issue_runs.thread_idle_timeout`（点号命名的**配置 key，不是列**，默认 **15m**，`<= 0` 时整个 pass 是 no-op），
判定与比较**全部用数据库时间**（`idle_since < now() - make_interval(secs => $2)`），逐 run **短事务** + 精确 CAS，重复 tick
**不产生第二条** `EndSession`；取消按 IssueRun D6 分流——**无会话**（`thread_state IS NULL`）⇒ `starting` 直进
`releasing`/`cancelled` + `deliveryState='skipped'` + 同事务 `declareDelete`，**不**写 `ending`、**不**发 `EndSession`；
**有会话** ⇒ `ending` + `EndSession{cancelled}`，run 自身 `phase/status` **不动**，不 release、不删 Workspace。三触发的
`user_ended` 因 **G-018 未获批准而不实现**（无 endpoint、无 route、无 OpenAPI 变更，**T4C-35 不适用**），作为明确
deferred item 登记。**ADR 优先于 plan 的一处偏离**：无会话取消的判别键是 `thread_state IS NULL`（IssueRun D6 的
「（尚无会话）」限定语 + 不变量 3），**不是** plan §4C.11 的 `phase` 判别；「`starting` 且会话已声明（`pending`）」的取消走
`ending`。**未改**：proto、migration、generated/OpenAPI、前端、任何 ADR 文件或其 `status`、Phase 4B 的 running
authority / `seq` 分配 / receipt / takeover 事务、`ending → ended`/`SessionEnded`/`discarded`/`delivering`/`deliver_revision`。
**G-018 / G-019 / G-022 / G-024 全部保持 OPEN 且行为未变**，新增 **G-026**。**未提交**：无 stage / commit / push / PR，
既有未提交修改原样保留。）

### 本轮关键结论（Phase 4C Final Completion Batch — A2 / A3 / A4 / G-018 — 2026-10-08）

- **本轮性质**：FINAL COMPLETION BATCH（最后一批实现 + 收口）。人类审批决定：**批准 A2、A3、A4 与 G-018**（A1/G-022 已在上一轮落地）。
  权威记录见 `plan.md` §15 的 `### Round: Phase 4C Final Completion Batch …`。**未进入 Phase 5。**
- **ADR 修订**：`specs/decisions/cloud/thread/0-durable-agent-thread-with-user-turns.md` —— A2 进 **D3**（接受谓词加
  `cancel_requested_at IS NULL` 合取项，并写明取消是请求、不立即终态、不立即删 Workspace、不改变 `discarded` 语义）；
  G-018 进 **D4**（用户主动结束端点的形状、事务顺序与「只到 `ending`」边界）；A3 与 A4 进 **D5**（读取窗口词汇与
  两类失效提示）。**frontmatter `status` 未改**（早已 `approved`），**未新增后续 ADR 文件**，**未改 migration**。
- **A2 落地**：`requireThreadAccepting` 的**权威**前置条件与写命令的 **CAS 谓词**（`AND cancel_requested_at IS NULL`）同时收窄，
  因此「请求先提交、写入后到」这一竞争也被拒；`cancel_requested_at` 置位后的追加与 `ending | ended` 共用 `409 thread_closed`，
  且**零**条目、**零**命令、**零**幂等记录，`thread_state`/`version`/`idle_since`/`phase` 全不变。旧的 deferral 测试
  `TestThreadMessageCancelRowIsDeferred` **删除**，由 `TestThreadMessageRejectsAfterCancellationRequested` 取代。
- **A3 落地**：`after` / `before` / 无游标（取尾）三种窗口统一由 `seq` 范围查询实现（**不用 `OFFSET`**），响应恒升序并带
  `idleSince`（与快照同刻）、`nextCursor`/`prevCursor`（取窗口内真实条目，空窗不给）；`limit` 缺省 200、上限 500；
  `after` 与 `before` 互斥，**互斥优先于游标校验**；非十进制/负数游标 `400 invalid_cursor`。空串游标回落到取尾。
- **A4 落地**：**新增** `issue_run.thread_changed`，而不是加宽旧事件——`issue_run.thread_appended` 的名称、负载与语义逐字不变。
  两类提示都在**提交后**发布（hint 在调用方事务上排队，由 `Store.transact` 在 `tx.Commit()` 成功后释放），同一提交各至多一条
  （`thread_changed` 按 `lastSeq` 取大去重）；测试按**每个提交的完整通知集**断言（hub 与 SSE 双通道）。
- **G-018 落地**：新路由 `POST /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread/end`（`Idempotency-Key` 必需、body `{}`），
  单事务内幂等预检 → 授权 → 权威重读与状态校验 → CAS `pending|active|idle → ending` + `EndSession{user_ended}` + 幂等响应，
  提交后发命令可用信号，返回 `202 {"threadState":"ending"}`。**幂等预检先于生命周期校验**（同键重放即使 Thread 已 `ended`
  仍回放那个 `202`），新键对 `ending|ended` 是 `409 thread_closed`，同键异地是 `409 idempotency_conflict`。
  它**不**写条目、**不**分配 `seq`、**不**改 `phase`/`status`；`ended` 仍由会话终态决定。
- **竞争矩阵**（`-count=10` 全绿，零 `time.Sleep`）：`/thread/end` 对空闲期满、对取消投放、对新的用户轮次、两个键互相竞争——
  四种竞争都只产生**恰好一次** `ending` 转换与**恰好一个** `EndSession`，且结果是合法的串行结果之一（`reason` 只取
  `user_ended` / `idle_timeout` / `cancelled`）；并发同键 POST 仍只产生一个轮次。
- **缺口分类**：**CLOSED** —— G-017（A3）、G-018、G-023（A4）、G-024（A2）。**仍 OPEN / NON-BLOCKING** ——
  G-019（`SessionEnded` / `ending → ended` / `queued → discarded`，**归 Phase 5**）、G-020（多实例 SSE 与重放）、
  G-021（多 worker 分区）、G-025（migration README 文档债）、G-026（`cancel_requested_at` 的**生产写入者**仍不存在，
  因此取消只有读侧谓词）。**0 项 BLOCKER**，因此本批次结束时 Phase 4C 判定为完成。
- **specs 证据表同步**：`test-cases/cloud/thread/durable-thread.md` 新增核心用例
  **A User End Request Ends The Thread Exactly Once**，并把读取节、SSE 节、接受谓词行的 `Missing`/`Partial` 如实升级为
  `Covered`（含 `before`、取尾、`idleSince`、游标、仅状态变化通知、取消谓词）；`queued → discarded`、`ending → ended`、
  客户端重连/轮询兜底、多实例投递**保持 `Missing`/`Partial`**，**未**把 `SessionEnded` / `discarded` / Phase 5 / G-026 的
  cancel 写入者标成 `Covered`。
- **本轮未做**：未进入 Phase 5（`SessionEnded`、`ending → ended`、`queued → discarded`、`running → delivering`、
  `DeliverRevision`、交付结算、释放完成、Workspace 删除完成、`done` 一律零实现）；未新增/修改 migration；未改 proto；
  未改任何 ADR 的 `status`；未手改任何 generated artifact；未弱化任何 lint / 测试。

### 上一轮关键结论（Phase 4C 加速实施 Batch 3：S2a + S6 — 2026-10-08，保留）

- **本轮性质**：IMPLEMENTATION，单 Agent 按 mandate 顺序 **S2a → S6 → 条件性 S2b → Batch 3 集成回归**。权威记录见
  `plan.md` §15 "Round: Phase 4C Accelerated Implementation Batch 3 — S2a (Thread GET, approved subset) + S6 (SSE
  invalidation notice) / 2026-10-08"。
- **S2a 交付**：`internal/core/thread_read.go`（新）、`internal/core/public.go`（dispatch）、
  `internal/api/router/router.go`（route）、`internal/contract/openapi.go` + `api/openapi.json` +
  `frontend/src/api/*`（生成物，未手改）、`integration/agent_run_thread_read_test.go`（新）。
- **S2a 三条不变量**：① **只实现已批准子集**——D5 只写了 `after`/`limit`/升序/`threadState`，其余（tail、`before`、
  `idleSince`、游标）**一行未写**；② 快照一致——`items` 与 `threadState` 同事务读取，run 资源字段集不变；③ 只读且授权与
  comments 一致（三重作用域 + membership），`node_execution_id`/`node_sequence`/`run_id` 不出网。
- **S2a 的一处读法选择（已登记 G-017）**：`after` 缺省读作**从头**（= `after=0`，即「不新增语义」的那一种解释），
  **不是** tail；`thread_state IS NULL` 答 `404` 而非空 `threadState`（D4 的状态集封闭）。
- **S6 交付**：`internal/core/thread_events.go`（新）、`internal/core/hub.go`（+`issueId`/`runId`/`lastSeq`）、
  `internal/core/store.go`（`transaction.appends` + 提交后 `publishThreadAppends`）、三个发布点
  （`agent_run_session_start.go`、`agent_run_thread_message.go`、`agent_run_thread.go`）、
  `internal/core/hub_test.go`（字节形状白盒）、`integration/agent_run_thread_events_test.go`（新）、
  `integration/agent_run_thread_takeover_test.go`（lease seed 改幂等以便场景复用，语义未变）。
- **S6 三条不变量**：① 发布**只在提交之后**——hint 排在调用方事务上、由 `Commit()` 成功后释放，panic 回滚与**提交失败**都不发
  （两条路径各有一条测试）；② 事件只是**失效提示**——`lastSeq` 是高水位、不含内容/状态、**不**推进客户端游标，恢复永远走
  GET + `after`；③ **兼容性逐字节**——4 个既有事件形状按字面固定，新增字段全部 `omitempty`。
- **T4C 映射**：S2a → T4C-7/T4C-9/T4C-10/T4C-11/T4C-12（另有「无条目的空窗」「只读无写」两条补充用例）；
  S6 → T4C-25、T4C-27。**T4C-6/T4C-8 不实现**（随 S2b），**T4C-26 不实现**（G-023），三者均未标 Covered。
- **载荷证明（变异测试）**：把 `publishThreadAppends` 移到 `tx.Commit()` **之前**后，只有「提交失败」那条测试变红——
  因为 panic 回滚根本走不到释放那一行，所以提交失败这半边必须有独立测试；变异后已从备份恢复并重新编译。
- **门禁**：`task build` PASS；`task test` PASS（全包 0 FAIL，`integration` 57.9s、`internal/core` 24.4s）；
  `task test:race` PASS（`./...` 与最终工作树的 `./integration` 各一次，**0 DATA RACE**）；`git diff --check`（cloud 与 specs）
  干净；`task frontend:generate` **生成幂等**（`task openapi` + `npm run api:generate` 重跑后三个生成物 md5 不变）；
  `npm --prefix frontend run check` PASS（组合 `frontend:check` 的漂移步骤只因生成物**未提交**而失败，按 mandate 以幂等证据
  替代结论）。`task format:check`（5 文件）与 `task lint`（7 项）的失败项**全部**位于 **HEAD 未修改**文件，属既有基线；
  本轮新增的 1 项 `bodyclose` + 2 项 `hugeParam` 已修（后者是 `SpaceEvent` 增字段后 96 字节触发，按值传参是刻意的扇出语义，
  用紧邻最小 `//nolint:gocritic` + 理由记录，未改阈值、未改签名）。**未运行 `task format`**。
- **Gaps**：**G-017 stays OPEN**（S2b `S2B_DEFERRED_PENDING_ADR`：A3/A4 未批准，未改任何 ADR 文件或其 `status`）；
  **G-023 stays OPEN**（T4C-26 不实现，边界被双向断言钉住）；**G-018 / G-019 / G-020 / G-021 / G-022 / G-024 / G-025 /
  G-026 状态未变**（`/thread/end` 未实现、无 `ending → ended`/`discarded`、SSE 仍无 broker 无回放）。
- **specs 证据同步（`specs` 仓库）**：`test-cases/cloud/thread/durable-thread.md` 的 GET 与 SSE 两节：只翻转有**直接测试证据**
  的义务，未实现项（tail/`before`/`idleSince`/游标、仅状态变化通知、前端重连轮询）一律保持 `Missing`；
  并加 Batch 3 implementation status 段；`test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`
  的 SSE/授权相关行同步。Phase 5 契约保持 `Missing`。
- **下一步**：**Final Gate 轮**收口（全套门禁 + 与 Phase 4B/4C 全基线对照 + gap 复核）。需要 ADR 修订才能开工的项：
  S2b（**A3**）、仅状态变化通知（**A4**）、`/thread/end`（G-018）；Phase 5 需 G-019 专门轮次。

### 上一轮关键结论（Phase 4C 加速实施 Batch 2：S5 + S7 — 2026-10-08，保留）

- **本轮性质**：IMPLEMENTATION，单 Agent **连续**推进 **S5 → S7**。权威记录见 `plan.md` §15
  "Round: Phase 4C Accelerated Implementation Batch 2 — S5 (user-turn echo + `active⇄idle`) + S7 (ending triggers, stopped at `ending`) / 2026-10-08"。
- **S5 交付**：`internal/core/agent_run_thread.go`（echo 推广 + 批次后生命周期）、
  `internal/core/agent_run_thread_lifecycle_db_test.go`（新，T4C-19/20/28/29/30 + 首提示 echo-only 回归）。
- **S5 四条不变量**：① 用户轮次 echo **绝不**产生新条目/新 seq/内容改写/新命令/重新入队，只有 `status` 单向
  `queued → delivered`；② 首提示 echo（seq=1）保持「仅收据」；③ 生命周期判定依据**批次末条有效记录**，被 echo 的用户轮次
  算有效、首提示 echo 不算有效，`queued` 用户轮次**阻止** idle；④ `pending → active` 保持 Phase 4B 规则，**未**改写。
- **S7 交付**：`internal/core/agent_run_thread_end.go`（新）、`internal/core/agent_run_thread_command.go`
  （`EndSessionCommand`）、`internal/core/store.go`（`ThreadIdleTimeout`，零值 = 未配置 = no-op）、
  `internal/config/config.go`（`IssueRunsConfig` + `DefaultThreadIdleTimeout = 15m`）、`configs/config.yaml`
  （`issue_runs.thread_idle_timeout: 15m`）、`cmd/server/main.go`（两个 10s 循环 + 配置接线）、
  `internal/core/agent_run_thread_end_db_test.go`（新，T4C-31/32/33/34 + 边界）。
- **S7 三条不变量**：① 结束**只**到 `ending`，无第二条终态权威（G-019）；② `EndSession` 是**请求**——run 自身
  `phase`/`status` 不动，不 release、不删 Workspace、不跳过 session 关停/revision 生命周期；③ 三条结束路径各有**唯一**
  请求，重复 tick 与已 `ending` 的取消都**不**产生第二条。
- **并发矩阵（T4C-33，真实 PostgreSQL + channel barrier，无 sleep）**：两个 idle tick ⇒ 恰一条 `EndSession`；idle tick ‖
  取消（请求先落库）⇒ 恰一条且 reason 为 `cancelled`；idle tick ‖ 接管 ⇒ 允许集为「ending + 一条 `idle_timeout`」或
  「active + 零命令」，并以**确定性顺序**另行覆盖 takeover-first 的一半；取消 ‖ 接管 ⇒ 收敛为 ending + 一条 `cancelled` +
  条目照常追加。
- **T4C-34**：`record` 永不改写、`seq` 永不重编号、唯一可变列是单向的 `status`；同时断言 Phase 5 边界（无 `ended`、
  无 `discarded`）。**GET 半边仍不可测**（S2a 在**不实现**清单内）。
- **S5 checkpoint**：S5 定向测试 + Phase 4B 接管全量回归 + Batch 1 S3/S4 套件全绿后记录
  `S5_IMPLEMENTATION_CHECKPOINT_REACHED`，**未 commit**，随后继续 S7。
- **门禁**：`task build` PASS；`task test` PASS（全包 0 FAIL）；`task test:race` PASS（无 DATA RACE）；
  `git diff --check` 干净；`gofmt`/`go vet ./internal/core/` 干净。`task format:check`（5 文件）与 `task lint`（7 项）
  的失败项**全部**位于 **HEAD 未修改**文件，属**既有**基线；本轮新增文件已修正 gofumpt，**零新增** lint 发现。
  **未运行 `task format`**（会重写既有未提交文件）。
- **Gaps**：**G-018 stays OPEN**（`/thread/end` 未实现，`user_ended` 不可达，T4C-35 不适用，作为明确 deferred item）；
  **G-019 stays OPEN**（Phase 5 边界完整守住）；**G-022 stays OPEN**（migration 0022 中 `status` 的存在**不**自动关闭 ADR
  gap）；**G-024 stays OPEN 且行为未变**（`cancel_requested_at` + `pending/active/idle` 的 POST deferred 行为**未**被
  side effect 改变，`TestThreadMessageCancelRowIsDeferred` 仍绿）；**新登记 G-026**（`issue_runs.cancel_requested_at`
  无生产写者，本轮只实现**反应**、**未**发明公开取消 API）。
- **specs 证据同步（`specs` 仓库）**：`test-cases/cloud/thread/durable-thread.md`（加 Phase 4C implementation status 段；
  `连续与幂等`/`冲突整批拒绝`/`同事务写入与幂等`/`关闭后拒绝` → `Covered`，`轮次结算` → `Partial`（`delivered` 半有证据、
  `discarded` 半属 Phase 5），`空闲期满结束`/`追加消息重置`/`进入 idle 的判定` → `Covered`；**GET 与 SSE 两节保持
  `Missing`**）；`test-cases/cloud/issue-run/agent-run-orchestration.md`（`取消是请求` → `Partial`，登记 IssueRun D6
  「（尚无会话）」判别式与 plan §4C.11 措辞的差异、以及 G-026；header 拆出 Batch 1+Batch 2 已落地项）；
  `test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`（Batch 2 段：S5 echo 结算、A 侧
  `EndSession{reason}` 投放，并显式区分 `EndSession`（Cloud→Node 请求）与 `SessionEnded`（Node→Cloud 终态，Phase 5））。
  只翻转有**直接测试证据**的义务（Phase 4B、Batch 1 与本轮的证据，本次门禁实跑为绿）；Phase 5 契约一律保持 `Missing`。
- **下一步**：**S2a**（Thread GET 已批准子集）可立即开工；**S2b** 需 ADR 修订 **A3**（G-017）；**S6** 需 **A4**（G-023）；
  **S7 的 `/thread/end`（T4C-35）** 需先取得 G-018 的 ADR 修订或架构师确认；Phase 5（`SessionEnded` → `ended` +
  `discarded`）需 G-019 专门轮次；**G-026** 需要一个已批准的取消 API 切片。

### 上一轮记录（Phase 4C 加速实施 Batch 1：S3 + S4 — 2026-10-08，保留）

#### 当时 Phase

`Phase 4C ACCELERATED IMPLEMENTATION BATCH 1 — S3（Thread Command Control Plane）+ S4（Thread POST）`

#### 当时 Slice

`Phase 4C Batch 1（IMPLEMENTATION，单 Agent 连续推进）：S3（thread_commands 控制面 + A 缝 + 提交后 ThreadCommandAvailable + T4C-21..T4C-24）+ S4（公开 Thread POST + 同事务条目/状态/命令/幂等 + T4C-13..T4C-18）`

#### 当时状态

`PHASE_4C_ACCEL_BATCH_1_CORE_DONE_WITH_ADR_DEFERRED`
（本轮为 **ACCELERATED IMPLEMENTATION ROUND**：单 Coding Agent 连续推进 **S3 → S4**，中途不结束本轮，两片边界互不合并、
互不削弱。**S3 完整**：`EnqueueThreadCommand` 在**调用方事务**内 `INSERT thread_commands`、A 生成 `command_id`、
`created_at` 用数据库时钟、投递字段初值 NULL；`ClaimThreadCommands{epoch,limit}` 为纯读，按 run 分组、组内创建序，只列出
**已登记 agent_session 执行**的 run 并返回其 `execution_id` 与目标 Node；`RecordThreadCommandDelivered` 首次登记生效、
同执行重放收敛幂等、异执行 `CONFLICT` **绝不**复写；`ThreadCommandAvailable{run_id}` **只在提交后**发布；错误映射到既有
内部 Fault/gRPC 码，**未新增 proto 枚举**。**S4 core 完整**：`POST .../runs/{rid}/thread/messages`（强制
`Idempotency-Key`、`{content:[{type:"text",text}]}`、文本总量 ≤ 64 KiB、v1 仅 text）在**同一事务**内按序执行
幂等预检 → 鉴权 → 权威重读 run → 生命周期前置 → 分配 Thread `seq` → 生成 Cloud `turn_id` → 写
`source='user'`/`kind='user_turn'`/`status='queued'` 条目 → `thread_state='active'` 并清 `idle_since` → 经 **A 缝**放出
`SubmitUserTurn` → 写幂等记录 → COMMIT → 提交后 `ThreadCommandAvailable`；业务层**不**直接写 `thread_commands`。
T4C-13..T4C-18 与 T4C-21..T4C-24 全部落地并通过；`task build` / `task test` / `task test:race` 全绿；OpenAPI 与前端
客户端经 `task frontend:generate` 仅**增量**更新且生成**幂等**，`npm --prefix frontend run check` EXIT=0。
**A2 未获批准 ⇒ G-024 仍 OPEN**：D-4C-03 的 `cancel_requested_at IS NOT NULL` 与「运行 Workspace 不活」两行**未实现**，
`requireThreadAccepting` 与 CAS 有意窄于 D-4C-03，并以 `TestThreadMessageCancelRowIsDeferred` 钉住，故标记为
**CORE_DONE_WITH_ADR_DEFERRED**。**G-021 仍有意未解决**。**未改**：Phase 4B 的 running authority / `seq` 分配 /
receipt / takeover 事务、proto、任何 ADR 文件或其 `status`、`specs/test-cases/cloud/thread/durable-thread.md`。
**未提交**：无 stage / commit / push / PR，既有未提交修改原样保留。）

#### 上一轮关键结论（Phase 4C 加速实施 Batch 1：S3 + S4 — 2026-10-08）

- **本轮性质**：IMPLEMENTATION，单 Agent **连续**推进 **S3 → S4**。权威记录见 `plan.md` §15
  "Round: Phase 4C Accelerated Implementation Batch 1 — S3 (Thread Command Control Plane) + S4 (Thread POST) / 2026-10-08"。
- **S3 交付**：`internal/core/agent_run_thread_command.go`（A 侧三类操作 + `SubmitUserTurnCommand`）、
  `internal/controlgrpc/agentruns.go` + `agentruns_test.go`（proto 转换与错误映射）、`server.go`/`signals.go`/
  `internal/core/signals.go`/`store.go`/`cmd/server/main.go`（**提交后**发布 `ThreadCommandAvailable` 的接线）、
  `internal/core/agent_run_thread_command_db_test.go`（T4C-21..T4C-24）、
  `integration/agent_run_thread_command_grpc_test.go`（端到端投递回路）。
- **S3 四条不变量**：① `EnqueueThreadCommand` **只用调用方事务**，不自行开事务/不 commit/不 rollback/不做任何
  HTTP/Node/文件系统/网络 IO；② `ClaimThreadCommands` 是**纯读**且**只依赖 Cloud 持久化状态**（绝不读 Controller 内存），
  会话执行未登记的命令**留在 Cloud**；③ 投递登记首次生效、同执行重放幂等、异执行 `CONFLICT`；④ 信号**只在提交后**发，
  回滚**零信号**（确定性注入 observer，**无 sleep**）。
- **S4 交付**：`internal/core/agent_run_thread_message.go`、`internal/api/router/router.go`（路由 + 256 KiB 传输上限）、
  `internal/core/public.go`（分派 + `Idempotency-Key` 前置校验 + 同事务幂等记录）、`internal/contract/openapi.go`、
  `api/openapi.json`、`frontend/src/api/*`（生成产物，**无手工编辑**）、
  `integration/agent_run_thread_message_test.go`（T4C-13..T4C-18，真实 HTTP + PostgreSQL）。
- **A/B 原子性证明（本轮最重要的验收项之一）**：`TestThreadMessageSeamFailureRollsBackEverything` —— 缝未接线时
  `EnqueueThreadCommand` 返回错误 ⇒ `503 thread_command_unavailable`，回滚后**无**用户条目、**无** `thread_state` 变化、
  **无**命令、**无**幂等记录；换真实控制面后以**同一 key** 重试表现如干净首发。
- **幂等 7 情形**：首发 201（T4C-13）、同 key 同 body 重放原响应且零新写入（T4C-14）、并发同 key 恰一次创建
  （T4C-15，显式 barrier，无 sleep）、同 key 异 body `409 idempotency_conflict`（T4C-16）、异 key 同 body 两次独立轮次
  （T4C-17）、同 key 异 run 冲突（T4C-16）、首次成功后运行进入终态仍以原响应重放（由同事务幂等记录在**生命周期校验之前**
  命中实现——见 §4C.5 的顺序）。
- **G-024 显式延后（范围偏差 1）**：A2 未批准，故 D-4C-03 的 `cancel_requested_at IS NOT NULL` 与 Workspace 非活两行
  **不实现**；`requireThreadAccepting`/CAS 有意窄于 D-4C-03，注释标注 A2 未批准，`TestThreadMessageCancelRowIsDeferred`
  钉住该行（A2 落地即转红）。**保持 G-024 OPEN，未改任何 ADR `status`。**
- **其余范围偏差**：② `phase='running' + thread_state='pending'` 与「已启动却无 Thread 状态」按 §4C.15 归 **500
  `internal_error`**（自身不变量破坏，不得伪装成客户端可解释的 409）；③ 跨 tenant/Issue/软删 run ⇒ 404 `not_found`，
  **非成员 ⇒ 403 `membership_required`**（与评论授权一致，复用既有 `membership()`，未改授权语义）；④ D3 的 64 KiB 是
  **解码后文本**上限（core 校验 `content_too_large`），传输层故取 256 KiB，超出传输上限降级为 `invalid_json`。
- **测试编号对齐**：mandate 对 S4 的 T4C-13..18 逐条编号与 §4C.16 设计矩阵的 T4C-13..18 编号不同，但**指向同一组
  测试**；两套编号的六条义务均已覆盖（逐条对应见 `plan.md` §15 本条的「测试编号对齐」）。S3 的 T4C-21..24 编号两处一致。
- **门禁**：`go build ./...` PASS；`task build` PASS；`task test` PASS；`task test:race` PASS；
  `task frontend:generate` PASS 且**幂等**（md5sum 复跑零变化）；`npm --prefix frontend run check` EXIT=0
  （53 文件 / 299 测试，行覆盖 90.93%）；`git diff --check` 无新增空白错误。`task format:check`（5 文件）与
  `task lint`（7 项）的失败项全部位于 **HEAD 未修改**文件，属**既有**基线；本轮引入的 1 项格式与 2 项 misspell 已修复。
  `task frontend:check` 的漂移步骤**预期失败**（生成产物未提交而本轮禁止 commit），已用 md5sum 证明生成稳定后直接
  运行前端门禁并通过；**未**跳过生成、**未**手工编辑产物。
- **下一步**：**S5**（echo→`delivered` + `active⇄idle`）；**S2a**（Thread GET 已批准子集）可并行；
  **S2b/S6/S7** 仍需先完成 ADR 修订 **A2**（G-024）/ **A3**（G-017）/ **A4**（G-023）。

### 上一轮关键结论（Phase 4C Slice 1 实施轮 — 2026-10-08，保留）
（本轮为 **IMPLEMENTATION ROUND**，严格限于 §4R.5 的 **S1**：新增 `0022_thread_api_and_commands.sql`（`thread_commands`
控制面表；`thread_entries.status` 与两条 CHECK；`thread_entries_queued` / `thread_commands_undelivered` /
`issue_runs_idle_threads` 部分索引；对「有 seq=1 真实声明」的 agent run 回填 `thread_state='pending'`），并让 B-owned
`StartSession` 在 seq=1 的**同一事务**内物化 `thread_state='pending'`（D-4C-01 / D-4C-12）。T4C-1..T4C-5 全部落地并通过；
`task build` / `task test` / `task test:race` 全绿。**回填按业务事实而非阶段代理**（谓词为
`EXISTS(thread_entries seq=1, source='system', kind='user_turn')`，排除取消/终态/无 Workspace/软删除/非 agent），
且 CHECK 前先安全回填既有 `source='user'` 条目为 `queued`，升级不会失败。
**G-022 仍 OPEN**：A1 修订提案**未获批准**，**未**改任何 ADR 文件或其 `status`。**新登记 G-025**（migration README 停在
0017 的文档债）。**未改**：Phase 4B 的 running authority / `seq` 分配 / receipt / takeover 事务、proto、OpenAPI/generated。
`task format:check` 与 `task lint` 的失败项全部落在 **HEAD 未修改**文件，与本轮无关。
**未提交**：无 stage / commit / push / PR，既有未提交修改原样保留。）

### 本轮关键结论（Phase 4C Slice 1 实施轮 — 2026-10-08）

- **本轮性质**：IMPLEMENTATION，**仅 S1**。S2a–S7 未动。权威记录见 `plan.md` §15
  "Round: Phase 4C Implementation Slice 1 — migration 0022 + `pending` materialization / 2026-10-08"。
- **交付**：`internal/core/migrations/0022_thread_api_and_commands.sql`；`agent_run_session_start.go` 的 `pending` 物化
  （CAS `WHERE id=$1 AND thread_state IS NULL`，断言恰 1 行，否则 panic 回滚——宁可整体回滚，也不留下「首条 prompt 已提交
  但 Thread 状态永不物化」的不一致读模型）；T4C-1..T4C-5。
- **回填的三条硬要求（前置检查 2/3）**：① 以**真实会话声明**（seq=1, `source='system'`, `kind='user_turn'`）为基础，
  **不用** `phase IS NOT NULL` 之类的阶段代理；② 排除取消（`cancel_requested_at IS NOT NULL`）、终态、无 Workspace、
  软删除、非 agent 运行；③ 加 CHECK **之前**先回填 `source='user'` 条目的 `status='queued'`，保证升级不失败。
  三条均以**变异测试**验证 T4C-5 会捕获（换成阶段代理 → 失败；删取消排除 → 失败；删 CHECK 前回填 → `23514` 失败），
  随后逐字还原。
- **前置检查 1 未满足**：G-022 对应的 Thread D1 修订**未获批准**。按 mandate 只输出最小修订提案 **A1**，**未**改任何 ADR、
  **未**改任何 `status`；实现依据为**已批准**的 D3、不变量 4 与 D-4C-12。
- **回归兼容（范围偏差，已登记）**：0022 的新约束与 4B 时期的两处断言直接冲突，故修改
  `integration/agent_issue_run_skeleton_test.go`（既有 user 条目补 `status`，加两条反例）与
  `internal/core/agent_run_thread_takeover_db_test.go`（过期注释订正；T4B-19 由「表不存在」改为「接管事务零写入」）。
  **未**放宽任何 CHECK、**未**跳过任何断言、**未**改动 4B 语义。
- **门禁**：`go vet` PASS；`task build` PASS；`task test` PASS；`task test:race` PASS。`task format:check` / `task lint`
  的失败项（5 + 7 项）全部位于 HEAD 未修改文件，属**既有**问题。
- **下一步**：**S2a**（Thread GET 已批准子集）；S2b 需先完成 ADR 修订 A3。

### 上一轮关键结论（Phase 4C 就绪评审轮 — 2026-10-08，保留）

- **本轮性质**：ARCHITECTURE REVIEW + IMPLEMENTATION READINESS。**零** production 代码、**零** migration、
  **零** 测试、**零** proto/API/generated 改动、**零** ADR 文件改动。权威全文在 `plan.md` 的
  `## Phase 4C Readiness — …`（§4R.0–§4R.8）。
- **§1 核验**：以当前本地工作区为事实来源，只读 `git status --short` × 4 仓库并读取 plan / 两份 approved ADR /
  4B production / migration 0018–0021 / proto。**新确认**：proto **已完整**（4C 无需改 proto）；`thread_entries`
  无 `status` 列；`thread_commands` 表不存在；`SpaceEvent` 只有 `{type,spaceId,projectId?,version?}`；
  `PublicRequest` 有 `After`/`Limit` 无 `Before`；全局 advisory lock 在 `store.go:267`。
- **§2 G-017 切分**：`after`/`limit ≤ 500`/升序/`threadState`/事件 `{issueId,runId,lastSeq}` = **已有批准依据**；
  无游标 tail 读、`before`、`idleSince`、`nextCursor`/`prevCursor` = **新扩展（需 ADR 修订）**；`pending` 物化时点
  = **措辞澄清**。⇒ **只有已批准子集可直接实施**，故 S2 拆为 **S2a**（已批准）与 **S2b**（扩展，需先修订）。
- **§3 G-018**：给出完整端点契约（method/body/幂等/权限/接受条件/成功码/`EndSession`/与 cancel 及 idle 的竞态/
  错误表/测试义务）。**`ending → ended` 仍归 Phase 5。**
- **§4 D-4C-01..12 复核**：全部通过，未静默修改；**新登记 3 项冲突** G-022（`thread_entries.status` 不在 D1 列清单）、
  G-023（无新条目也发 `thread_appended`，与 D5 字面冲突）、G-024（`cancel_requested_at` 已置时拒绝 POST，与 D3
  字面冲突）。三项均为**可达**冲突，均按 mandate §4 先登记为 Gap。
- **§5 切片**：S1（migration 0022 + `pending` 物化）、S2a/S2b（Thread GET）、S3（`thread_commands` 控制面 + A 缝，
  **无 proto 改动**）、S4（Thread POST）、S5（echo→`delivered` + `active`/`idle`）、S6（SSE 失效提示）、S7（`ending`：
  用户结束端点 + idle 超窗扫描）。每片八要素齐全，**禁止合并为一次大规模实现**。
- **§4R.7 待批准**：ADR 修订 A1–A4（Thread D1 加 `status`；D3 加 cancel/workspace 谓词；D4 写明 `pending` 物化时点；
  D5 补 tail/`before`/`idleSince`/游标并调整事件发布条件）。**未写入任何 ADR 文件、未改任何 `status`。**
- **§6 Git**：未 stage / commit / push / PR；未 `reset`/`restore`/`checkout .`/`clean`/`stash`；既有未提交修改原样保留。

### 上一轮关键结论（Phase 4C 设计轮 — 2026-10-08，保留）

- **本轮性质**：DESIGN / DECISION ONLY。**零** production 代码、**零** migration、**零** 测试、**零** API/proto/
  generated/schema 改动。权威设计全文在 `plan.md` 的 `## Phase 4C — …`（§4C.0–§4C.20），本文件只做镜像与索引。
- **D-4C-01（关闭 G-016）**：`thread_state='pending'` **物化**——写者 = B-owned `StartSession` 的既有事务
  （与 seq=1 同事务、同谓词 `thread_state IS NULL`、断言 1 行）；另加**幂等 backfill** 迁移。否决 A 侧
  `RecordDispatch` 写入与读时派生；**取代 D-026 中「`pending` 无写者」子句**。
- **D-4C-02 GET**：`{items, threadState, idleSince, nextCursor, prevCursor}`；`after=N`（`seq > N`）/`after=0`（头）/
  `before=N`（更旧的 `limit` 条，仍升序）/无游标（取尾）；两游标并存 → `400 invalid_pagination`；limit 默认 200、
  上限 500；游标为不透明十进制 seq → `400 invalid_cursor`。**不复用也不放宽**既有 `page`/`window` 助手。
- **D-4C-03/D-4C-04 POST**：接受条件 = `thread_state ∈ {pending,active,idle}` ∧ `cancel_requested_at IS NULL` ∧
  workspace live，否则 `409 thread_closed`；缺失/跨租户/非 agent → `404 not_found`；不可能的 phase/state 组合 → `500`。
  条目 `source='user'`、`kind='user_turn'`、`status='queued'`、Cloud 生成 `turn_id`；同一事务 CAS `thread_state='active'`
  + `idle_since=NULL`（须 1 行）后调用 `EnqueueThreadCommand`。幂等复用既有 `idempotency_records`（哈希含 path），
  **同 key 不同 run → `409`（不跨 run 重放）**；终态后重放返回**原始**响应；不新增条目级幂等列。
- **D-4C-05/D-4C-06 seam 与身份**：`thread_commands` 归 A、`command_id` 由 A 生成、seam 在**调用方事务**内执行；
  未接线 fail-closed → `503 thread_command_unavailable`（整体回滚不留幂等记录，可安全重试）。
  `turn_id` / `command_id` / `execution_id` / `seq` 四类身份互不代偿。
- **D-4C-07/D-4C-08 SSE**：SSE **只做失效通知**（`issue_run.thread_appended{issueId, runId, lastSeq}`，需给
  `SpaceEvent` 增加三个可选字段）；**只有 GET 推进客户端游标**；GET 与 SSE 重连共用 `after`；PG notify **绝不**当
  持久日志。序不承诺、重复无害、**数据不丢 / 通知可能丢** → 前端重连 + ≥30s 轮询（G-020）。
- **D-4C-09 生命周期**：`active ⇄ idle` 在**接管事务内**由本批**最后一条被接管的 content 记录**决定
  （`turnEnded` 且无 `source='user' AND status='queued'` → `idle` + `idle_since` = DB 时间）；gate 不满足仍写条目。
- **D-4C-10/D-4C-11 终态与取消**：`ending` 三个触发源、恰好一次 `EndSession{reason}`；空闲窗口
  `issue_runs.thread_idle_timeout` 默认 15 分钟，由 B-owned 派发循环按 DB 时间判定；
  **`ending → ended` 唯一权威 = `SessionEnded`，属 Phase 5**（G-019）。`starting` 取消 → 直接 `releasing`（无
  `ending`/无 `EndSession`）；`running` 取消 → 同一事务写 `cancel_requested_at` + `ending` + `EndSession{cancelled}`；
  与 4B「cancel-first 接管不进 running」一致；历史不可变（仅 `status` 单向）。
- **D-4C-12 迁移（仅设计，不得创建）**：需要 migration **0022**（若实现）：`thread_commands` 表、`thread_entries.status`
  （`CHECK ((source='user') = (status IS NOT NULL))` + 部分索引）、`pending` backfill、
  `issue_runs (idle_since) WHERE thread_state='idle'` 部分索引。**明确不做**：不改 `thread_state` 值集、不加计数器、
  不加条目级幂等列、不改 `node_event_receipts`、不加 `DEFAULT`、不引入第二个 running 权威。
- **缺口**：**G-016 CLOSED**；新增 **G-017**（GET 游标词汇是 approved Thread D5 的扩展）/ **G-018**（用户主动
  结束端点缺失）/ **G-019**（`ending→ended` 依赖 Phase 5 `SessionEnded`）/ **G-020**（SSE 客户端重连+轮询义务）/
  **G-021**（`thread_commands`/`status` 迁移与 seam 接线待实现轮），**全部 NON-BLOCKING**。
- **测试设计**：T4C-1..T4C-34（GET / POST / EnqueueThreadCommand / SSE / 生命周期 / 真实 PostgreSQL 并发），
  **全部 `DESIGNED / MISSING`——本轮不写任何测试代码**。
- **specs 证据纪律（§13）**：`test-cases/cloud/thread/durable-thread.md` 新增两组 **`Missing`** 义务（GET 快照/游标/
  并发追加；SSE 是提示而非日志 + 客户端重连与轮询）；`test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`
  新增两行 **`Missing`** 义务（条目与命令同事务原子 + `503` 回滚可重试；投递身份与轮次身份分离）。既有 `Covered`/`Partial`
  **未改**；本轮**未**把任何 4C 面标记为 `Covered`；`proposed` 的 controller-session ADR **未被触碰**。
- **改动文件**：`plan/plan.md`（本轮唯一权威设计记录，新增 §4C.0–§4C.20 + §12 D-4C-01..12 + §13 + §7 + §15 + §16）、
  `plan/plan-zh.md`（镜像：头部 / §7 / §12 / §13 / §15），以及 specs 仓库的两份证据文档（仅义务与状态）。
  **production 代码 / migration / 测试 / API / proto / generated / schema 一律未改**。
- **git**：未 stage / 未 commit / 未 push / 未开 PR；工作区既有未提交修改**原样保留**。
- **下一步**：Phase 4C **实现轮**（按 D-4C-12 落 migration 0022、GET/POST/SSE/seam/lifecycle、T4C-* 转绿）；
  本轮不进入。

### 上一轮关键结论（Phase 4B 实现轮 — 2026-09-30，保留）

- **迁移**：`internal/core/migrations/0021_node_event_receipts.sql`（新，本轮唯一新表）：`node_event_receipts(execution_id,
  sequence, event jsonb, created_at)`，PK `(execution_id, sequence)`，FK `execution_id`→`node_executions`，
  索引 `node_event_receipts_retention`。**未改任何既有迁移**、未改业务表。
- **A 侧接管核心** `internal/core/agent_run_thread_takeover.go`（`agent_thread_takeover`）：校验 executionId 与
  1..64 事件（空批次 400 / 形状非法 400）→ 未知 execution 404 → kind/operationId 不符 409 → batch 内严格升序
  （缺口/乱序 409）→ 起点必须 `<= last_event_sequence+1` → 重叠段逐条比对收据（同 payload 幂等、异 payload 409
  `receipt_conflict`）→ 纯重放直接返回 → 插入收据 → 调用 `t.hooks`（同一事务）→ fenced `UPDATE node_executions
  SET last_event_sequence`。全程在 caller 事务内。
- **B 侧钩子** `internal/core/agent_run_thread.go`（`Store.threadEventsTakenOver`）：重读权威行（不信 caller 对象）→
  跳过 echo（`turn_id == seq=1.turn_id`）→ 真实记录按 `MAX(seq)+1` 落 `thread_entries`（`UNIQUE(node_execution_id,
  node_sequence)` 兜底）→ `taken>0` 时才 CAS `starting/dispatched/cancel_requested_at IS NULL → running/running/active`；
  echo-only 批次 **不**进入 running；stale/cancel/invalid-workspace 仍写条目但跳过 CAS。
- **接线（production，非 test helper）**：`internal/controlgrpc/agentruns.go`（`TakeOverThreadEvents`）+
  `internal/controlgrpc/server.go` 注册 `AgentRunService` + `internal/core/agent_run_settle.go`
  （`businessAgentRunHooks` / `NewBusinessAgentRunHooks`）+ `cmd/server/main.go` 设 `store.AgentRunHooks`。
- **决策**：新增 **D-026**（kind 来源、batch 上界、fail-closed 错误分类、stale run 的 take-over-but-don't-run、
  `pending` 无写者、§31 对 §4B.3 echo 行的取代）；D-025 标注「本轮有意不 enforcement」。
- **缺口**：**G-009 CLOSED**；G-013/G-014 CLOSED 且已实现；G-012 保持 PARTIAL（未拆分）；G-015 保持
  PARTIAL / NON-BLOCKING；**G-016 新增**（无写者物化字面 `thread_state='pending'`）；G-011 不变。
- **测试**：`internal/core/agent_run_thread_takeover_db_test.go`（T4B-1..T4B-19，真实 PostgreSQL 白盒，含 §42 并发
  场景）与 `integration/agent_run_thread_takeover_test.go`（端到端 gRPC：`TestAgentRunThreadTakeoverOverGRPC` 含 C5
  重放、`TestAgentRunThreadTakeoverGRPCRejections`）；`integration/migration_upgrade_path_test.go` 新增 0021 升级测试。
- **Gate**：gofmt / `go build ./...` / `go vet ./internal/... ./integration` / `go test ./... -count=1` /
  `go test -race -count=1 ./internal/core/... ./integration` / `git diff --check` 全绿；`task format:check` 与
  `task lint` 仅报告**既有基线**（5 文件 / 7 条，均非本轮改动文件）。
- **git**：未 stage / 未 commit / 未 push / 未开 PR。
- **下一步**：**Phase 4C 设计**（Thread API / SSE / `EnqueueThreadCommand` / delivery 生命周期）；本轮不进入。

### 上一轮关键结论（Phase 4B 架构决议，D-023/D-024/D-025，保留）

- **G-013 CLOSED（D-024）**：`starting→running` 唯一权威 = 首条**真实 Node Thread 记录**被 `TakeOverThreadEvents` 接管
  且 `ThreadEventsTakenOver` 钩子同事务提交（IssueRun D3 + controller-integration D6 + Thread D4）；**无** synthetic
  `session_started` 事件（Node 协议 D2 只发 `ThreadEvent{record}`）。`thread_state`：`pending`（已登记无记录；Thread D4）
  → 首条记录接管 → `active`；4B 只写 `pending→active`。echo 首 prompt（`turn_id == initial_turn.turn_id`）只写收据、
  不分配 seq、不新增条目（seq=1 不可变）；空批次 `ABORTED` 拒绝。
- **G-014 CLOSED / D-023 ACCEPTED**：`seq` = 接管事务内 `MAX(seq)+1`（从 2 起）。串行化证明用 **approved** 事实：
  一运行至多一会话执行（controller-integration D1/D-021）+ 全局 advisory lock（`transact`）+ `PK(run_id,seq)`——
  **不依赖 proposed 的 controller-session ADR**。Node `sequence` ≠ Thread `seq`；不新增计数器。
- **G-015 PARTIAL（D-025 初值）**：单运行非终态 event 上限 200,000（`thread_event_cap`）；`node_event_receipts` 保留
  `done` 后 30 天（`node_event_receipts_retention`）。
- **迁移 proposal**：Phase 4B 唯一新表 `node_event_receipts(execution_id, sequence)` + `event jsonb`（FK→`node_executions`）；
  不改业务表。
- **controller-session ADR 仍 `proposed`**：Cloud 侧 4B 不依赖它，**不报告** `BLOCKED_ON_CONTROLLER_SESSION_ADR`。
- **未改 production / migration / 测试**；仅 `plan/plan.md` 与 `plan/plan-zh.md`。git 未 stage/commit/push/PR。

### 上一轮关键结论（Phase 4A — D-020/D-021 实现证据，保留）

- **migration** `0020_node_executions.sql`（新）：`node_executions`（execution_id text PK、kind('agent_session'|
  'deliver_revision')、operation_id uuid 语义引用、work_id uuid UNIQUE FK→execution_work、node_id、input/result jsonb、
  dispatched_epoch、last_event_sequence DEFAULT 0、created/updated）；`node_executions_pending_node (node_id, created_at)
  WHERE result IS NULL` 支撑 C2 崩溃恢复。**不**创建 `node_event_receipts`/`thread_commands`。
- **production 首次登记** `agentWorkDispatch`：Controller 提供 work_id/execution_id/node_id/input/epoch；A 校验
  （work 存在、kind=agent_session、node==work.target.node_id、input==work.input 不可变快照 json 相等），随后**同一事务**
  INSERT node_executions + fenced UPDATE `execution_work SET execution_id WHERE id=.. AND execution_id IS NULL`；fence
  0 行→整体回滚，绝不 orphan（§9/§10）；经 `submitted` 幂等包装。
- **replay / invariant 冲突**：已注册同 work——同 execution_id/node/input → 幂等返回既有行；不同 → `dispatch_conflict`
  （绝不复写）。同一 execution_id 复用于另一 work → 冲突。首次登记路径对"读未注册期间他方已落的同 execution_id"做幂等容忍。
- **recovery reads（无 lease 纯读）** `agent_work_get`（按 executionId）/ `agent_work_pending`（result IS NULL，可按 node）：
  替换失败 Controller 复用既有 execution_id，不重复登记（C2）。
- **run 状态与 owner 纪律**：claim 与登记都不触碰 `issue_runs.phase/status/thread_state`/`thread_entries`；A 侧
  Create/DeleteRunWorkspace、EnqueueThreadCommand 仍 fail-closed。

### 本阶段未实现（mandate §30 不假装完成）

> 以下为 **Phase 4B 架构决议轮**的未实现清单（历史记录，保留原样）。Phase 4B 实现轮已交付
> `ThreadEventsTakenOver`、`TakeOverThreadEvents`、`node_event_receipts`、thread seq≥2、starting→running、
> `thread_state` transition 与 D-023 `MAX(seq)+1`；其余各项仍未实现。

- **4B 实现全部未做**：`ThreadEventsTakenOver`、`TakeOverThreadEvents`、`node_event_receipts`、thread seq≥2、
  starting→running、`thread_state` transition、Thread API/SSE、POST Thread message、`EnqueueThreadCommand`、
  `SessionEnded`/`DeliverySettled`/`RunWorkspaceDeleted`、cancel API、D-023 `MAX(seq)+1` —— 均无实现。
- 不把「schema/契约已设计」当作已实现；T4B-1..T4B-19 全 `DESIGNED / MISSING`。

**Phase 4C 已实现（截至 Batch 4，即收口轮）**：S1（migration 0022 + `pending` 物化）、S3（`thread_commands` 控制面 +
`EnqueueThreadCommand` 缝 + `ThreadCommandAvailable` 提交后发布）、S4（公开 Thread POST + 同事务条目/状态/命令/幂等 +
**A2 的取消谓词**）、**S5（用户轮次 echo → `delivered`、批次末条有效记录判定的 `active⇄idle` 生命周期）**、
**S7 的三个结束触发（`idle_timeout` 扫描、`cancelled` 分流、`user_ended` 的 `/thread/end` 端点，严格停止在
`thread_state='ending'`）**、**S2a 的完整读取面（`after` / `before` / 取尾 / `limit` / `idleSince` / `nextCursor`·`prevCursor`，A3）**、
**S6 的两类提交后失效提示（`thread_appended` + `thread_changed`，A4）** —— **均已实现并通过真实 HTTP/gRPC + PostgreSQL 测试**。
**Phase 4C 仍未实现**（**不得**标 Covered）：**`ending → ended` / `SessionEnded` / `queued → discarded`（Phase 5，G-019）**、
`DeliverySettled` / `RunWorkspaceDeleted`、`running → delivering → releasing → done`、`DeliverRevision` / 上传授权
（`GrantRevisionUpload`）、**完整 cancel API（`cancel_requested_at` 无生产写者，G-026——本仓只实现反应与读侧谓词）**、
SSE 的客户端重连/轮询兜底（无 Thread 面板）、多实例投递（G-020）、多 worker 命令分区（G-021）、
收据 GC / event 保留清理 / 任意 event 上限（G-015 PARTIAL）、migration README 债务（G-025）。
> **更新（Phase 5 Batch 1 + Batch 2，2026-10-08）**：上述 roster 的**大部分已实现**，逐项取代如下 ——
> `ending → ended` / `SessionEnded` / `queued → discarded` = **已实现**（Batch 1；G-019 = `CLOSED`）；
> `running → delivering` = **已实现**（Batch 1）；`delivering → releasing → done`、`DeliverySettled`、
> `RunWorkspaceDeleted`、`DeliverRevision` 的派发与执行 = **已实现**（Batch 2）；
> **仍未实现**：`GrantRevisionUpload` 与 Revision 登记/对象校验（**G-030**，Cloud Revision ADR 仍为 `proposed`，
> 故 `revision_delivered`/`revision_unchanged` 回 `UNAVAILABLE` + 零写入）、完整 cancel API（**G-026**，未变）、
> SSE 客户端兜底与多实例投递（**G-020**，未变）、多 worker 命令分区（**G-021**，未变）、收据 GC / event 保留清理
> （**G-015 PARTIAL**，未变）、migration README 债务（**G-025**，未变）。
> 本轮**未**触碰上述任何一项（mandate §0 只记录不修）。
**无会话取消的判别键是 `thread_state IS NULL`**（IssueRun D6 的「（尚无会话）」限定语），与 plan §4C.11 按 `phase` 的措辞不同：
ADR 优先于 plan，偏离已登记在 `plan.md` §15 与本文件。

### 当前 Blockers

**无 4C blocker** ⇒ Phase 4C 判定 `PHASE_4C_COMPLETE`。
**CLOSED**：**G-016**（设计轮 D-4C-01）、**G-022**（A1）、**G-017**（A3）、**G-018**、**G-023**（A4）、**G-024**（A2）。
**NON-BLOCKING OPEN**：**G-019**（`ending→ended` 与 `queued→discarded` 依赖 Phase 5 `SessionEnded`——**归 Phase 5**）、
G-020（SSE 客户端重连 + 周期轮询义务、多实例投递与重放）、G-021（多 worker 命令分区——S3 已按**单 worker 保证**交付，**有意未解决**）、
**G-026**（`issue_runs.cancel_requested_at` **无生产写者**——A2 只收窄了读侧接受谓词，**未**发明任何公开取消 API；
反应是 fail-closed 且幂等的，接线写者不改变已落地行为）、G-025（migration README 文档债）。
**A2 落地后的行为变化**：`cancel_requested_at` 已置时 POST 返回 `409 thread_closed`（与 `ending | ended` 同一错误），
旧 deferral 测试 `TestThreadMessageCancelRowIsDeferred` 已删除并由 `TestThreadMessageRejectsAfterCancellationRequested` 取代。
历史缺口：**G-012 PARTIAL**、**G-015 PARTIAL / NON-BLOCKING**、**G-009 CLOSED**、G-001 保持 PARTIAL
（Create/Delete RunWorkspace 与 `EnqueueThreadCommand` 归各自 Phase）、G-011 不在 Phase 4 顺手解决。
**controller-session ADR 仍 `proposed`** —— 是 desktop 侧独立待批准决策，**不是** Cloud 的 blocker，
也**不是** 4C 任何决策的前提（D-4C-01..12 的论证**不依赖**它）。

### 当前 Blockers（Phase 5 Batch 2，2026-10-08）

**存在 1 项已批准的 mandatory blocker** ⇒ Phase 5 Batch 2 判定 `PHASE_5_BATCH_2_BLOCKED`。
**APPROVED MANDATORY BLOCKER**：**G-030** —— Cloud Revision ADR
（`specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`）仍为 `status: proposed`，
故 Revision 登记 / 对象校验 / `GrantRevisionUpload` **不得实现**；`revision_delivered`/`revision_unchanged` 一律回
`UNAVAILABLE` + **零写入**（**不**静默降级）。解除条件：人类把该 ADR 评审为 `approved`。
**CLOSED**：**G-019**（Batch 1 的 Thread 终态半边 + Batch 2 的交付/释放/删除终态半边均已实现并有直接证据）。
**NON-BLOCKING OPEN（本轮新增）**：**G-029**（D4 的回复注释无批准字段路径）、**G-031**（D6 的幂等身份拼写与实现的
`(workspace_id, kind)` 偏差，**未**改 schema）、**G-032**（`releasing` 侧删除失败无放弃上限）。
**NON-BLOCKING OPEN（状态未变，§0 只记录不修）**：**G-020**（多实例 SSE 投递与客户端重连/轮询义务）、
**G-021**（多 worker 命令分区）、**G-025**（migration README 文档债）、**G-026**（`cancel_requested_at` 无生产写者）、
**G-027**（`OPEN / DOCUMENTATION CLARIFICATION`——**未**自行改 ADR）、**G-028**（`PRE-EXISTING BASELINE` lint 基线，
本仓 `task lint` 的唯一剩余项）。**G-012 / G-015 / G-001 / G-009 / G-011 状态不变。**

### 上一轮 Blockers（Revision ADR Approval Round，2026-10-08，保留）

**审计判定 `REVISION_ADR_NOT_READY_FOR_APPROVAL` ⇒ 该轮 `REVISION_ADR_APPROVAL_BLOCKED`。**
**仍存在的 approved mandatory blocker**：**G-030**（未变；本轮**未**改其 `status`，**未**自行批准）。
**新登记的 4 项批准阻塞（都属 G-030 的解除条件细化）**：
- **B-1** Revision 行身份与幂等未定义（`id` 生成者/事务、唯一键是 `UNIQUE(run_id)` 还是 per-attempt、同键不同负载如何处置）。
- **B-2** Object key 拼写：ADR 写 `{deliveryWorkId}`，而工作项 ID 由已冻结的 A seam 内部生成、Cloud 拿不到；
  实现只能用 Cloud 自生成的 per-attempt id。**ADR 的字面拼写不是已交付实现的拼写。**
- **B-3** `revision_ref` 的拼写只有 proto 前缀约束 + **node** 的 `proposed` ADR 给出，**Cloud** ADR 未定义。
- **B-4** D1 的「缺 `object_store` ⇒ 直接 `skipped` 到 release」构成**第四个** `delete_workspace` 触发条件，与 approved
  IssueRun **不变量 3** 冲突；且**复用**了 D6 无会话取消已占用的 `skipped`；触发点未规定、路径未实现。
**精度补充（非阻塞）**：P-1 校验范围只含存在性/大小/SHA-256；P-2 grant TTL 内重复 `PUT` 同一键未规定；
P-3 `unchanged` 不复用既有 Revision。
**本轮裁定**：**G-029 = `DEFERRED / NON-BLOCKING`**（**不**为其扩 schema，明确排除在批准范围外）；
**G-031 推荐 Option B**（`(workspace_id, kind)` + 限定未完成操作；**本轮未改 schema/ADR**）；
**G-032 最小 proposal = IssueRun D8**（重试沿用 operation D4、上限只取不可达窗口、终态 `done` + `status` 不变 +
`failure_reason = workspace_unavailable`；**未实现**）。
**其他缺口状态全部未变**：G-020 / G-021 / G-025 / G-026 / G-027 / G-028 与本轮前一致；**G-019 保持 `CLOSED`**。
**生产面**：**未改** Go / proto / migration / OpenAPI / frontend / 任何 ADR 文件或其 `status`；**未**实现任何 Revision behavior。

### 当前 Blockers（Revision ADR Decision & Amendment Round，2026-10-08）

**判定 `REVISION_ADR_DECISIONS_READY_FOR_HUMAN_APPROVAL` ⇒ 本轮的阻塞不再是「未定义的决策」，而是「待人类批准」。
没有任何未收敛的歧义残留。**
**仍存在的 approved mandatory blocker**：**G-030** —— Cloud Revision ADR 仍为 `status: proposed`（本轮**未**改其 `status`、
**未**自行批准）。它的解除条件已从「文本不可批准」变为**明确的 5 项审批清单**（见当前状态与报告）。
**B-1..B-4 与 P-1..P-3 全部收敛**（每项只剩一个方案），并已写入 ADR 正文；跨 ADR 变更逐项独立列出且**均未应用**：
① Node Revision ADR 的一句回显条款；② **G-033** 的 IssueRun D5 澄清；③ **G-031** 的 operation D4 + controller-integration D6 + 核心用例各一句；
④ **G-032** 的 IssueRun D8 + D3 阶段表 `done` 行 + 核心用例两处。
**新登记的缺口**：**G-033**（`OPEN / CROSS-ADR CLARIFICATION`，**阻塞 B-4 的最终批准**，不阻塞本轮收敛）；
**G-034**（`OPEN / NON-BLOCKING`，放弃后未登记的 `deliver_revision` 工作项仍可被认领 —— 认领路径无运行阶段过滤、
`execution_work` 从无删除者；**本轮不修**）。
**明确披露的剩余风险**：G-032 超上限后运行 Workspace **不会被自动回收**，且公开 API 对运行 Workspace 的 `delete` 恒 404
⇒ **没有自助重清路径**，只能由人类/运维处置；**本轮未发明任何后台清理机制**；`done` 只表示「不再重试」，**不**谎报删除成功。
**G-029 保持 `DEFERRED / NON-BLOCKING`**（**不**为其扩 schema，明确排除在批准范围外）；
**G-019 保持 `CLOSED`**；G-020 / G-021 / G-025 / G-026 / G-027 / G-028 / G-012 / G-015 / G-001 / G-009 / G-011 状态全部未变。
**生产面**：**未改** Go / proto / migration / OpenAPI / frontend / 测试；**未改**任何 approved ADR 的已批准决策
（G-031 / G-032 只输出提案）；两个 `proposed` ADR 的 `status` **未改**；**未**实现任何 Revision 生产路径。

---

## 16. 每轮 Agent 最终报告模板

```text
## A. Plan Compliance
plan.md updated: yes/no
implemented plan section:
scope deviation: none / details

## B. Implementation
files changed:
behavior implemented:

## C. Decisions
new decisions:
updated decisions:

## D. New Gaps
new blockers/gaps:
owner:
impact:

## E. Tests
tests added:
tests updated:
results:

## F. Gates
go build:
go vet:
integration:
race:
full test:
diff check:

## G. Git Status
cloud:
specs:
staged: no
commit: no
push: no
PR: no

## H. Current Plan State
phase:
slice:
status:
remaining work:
next recommended step:
```

如果达到稳定提交边界，额外报告：

```text
COMMIT_BOUNDARY_REACHED
```

但 Agent 不得执行 commit。

---

## 17. 核心执行原则

1. **先有计划，再写代码。**
2. **ADR 高于 plan，plan 高于临时实现选择。**
3. **设计变化必须留下记录。**
4. **业务状态与 control-plane 状态保持清晰 ownership。**
5. **same transaction 不等于 same owner。**
6. **所有 retry 必须可由持久化状态恢复。**
7. **sleep 永远不进入 DB transaction。**
8. **重复调用必须安全。**
9. **不得为了当前 Slice 偷跑后续 Phase。**
10. **Agent 永远不 commit / push / PR。**
11. **每轮结束必须更新 `plan.md`。**
12. **架构师通过审核 `plan.md` diff 控制实施方向。**
