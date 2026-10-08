# AgentRunDispatcher 实施计划

> 状态：**持续维护的设计 / 执行记录**  
> 范围负责人：**B — Cloud 业务 / 编排**  
> 当前目标：**Phase 4C DESIGN DONE — Thread API / SSE / Thread Commands / Lifecycle（纯设计轮；未实现）**；当前指标 `PHASE_4C_DESIGN_DONE / PHASE_4C_ARCHITECTURALLY_REVIEWABLE / READY_FOR_PHASE_4C_IMPLEMENTATION`（交付 D-4C-01..D-4C-12 与 §4C.0–§4C.20 设计；**G-016 CLOSED**；NON-BLOCKING OPEN：G-017..G-021。上一轮：Phase 4B 实现完成——迁移 `0021` + production 接管 + `ThreadEventsTakenOver` 钩子 + gRPC `TakeOverThreadEvents`，T4B-1..T4B-19 通过。4C 的 production 代码 / migration / 测试 / API 一律**未实现**）。
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

状态：**Phase 4A IMPLEMENTED（A-side dispatch registration）→ Phase 4B IMPLEMENTED（Thread takeover + `starting→running`）→ Phase 4C DESIGN DONE（Thread API / SSE / commands / lifecycle；设计轮，未实现）**。权威详见 plan.md 的 §4B.1–§4B.15、`## Phase 4C — …`（§4C.0–§4C.20）与 §15 的两轮记录。

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

核心原则（mandate §4）：`execution_work created ≠ running`、`claim ≠ running`、`dispatch ≠ running`；只有
权威接管/会话开始证据经 `ThreadEventsTakenOver` 钩子成功提交才 `starting → running`。

决策 D-019..D-026 与 D-4C-01..D-4C-12、缺口 G-009/G-012..G-021 见 §12/§13；4A/4B 的 T4-* 行已按实现证据标
Covered，T4-14/T4-15 为 DEFERRED，**T4C-1..T4C-34 为 DESIGNED / MISSING（本轮为设计轮，无测试代码）**。
当前状态指标见表头与 §15。

### Phase 5 — Delivery / Releasing / Done

状态：**PLANNED**

包含：

- session end
- delivery settlement
- cleanup
- workspace deletion
- terminal states

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
- **G-020 — 新增 / OPEN / NON-BLOCKING / DEPENDENCY（Phase 4C 设计轮，2026-10-08）**：SSE 失效提示**不保证送达**——
  hub 为进程内、缓冲 8、单实例，可能丢/重/乱序，无持久化、无 replay（api-boundary ADR，已实现）。**WHY 必须登记**：
  假定「可靠通知」的设计会诱导客户端把流当日志。**WHAT IS NEEDED TO CLOSE**：slice 1 依赖客户端重连 + 周期轮询
  （前端义务）；多实例投递需在同一 Publish/Subscribe 边界后换成 broker，并另行决策。
- **G-021 — 新增 / OPEN / NON-BLOCKING（Phase 4C 设计轮，2026-10-08）**：多 Controller worker 下 `thread_commands`
  的**投递分区**由 controller-integration ADR 自己列为未决。4C 的 seam 与表**不改变**该问题，只保证单 worker 下的
  「至少投递一次 + 登记幂等」。**WHAT IS NEEDED TO CLOSE**：另行决策（按 run 分区 / 租约归属）。

### G-022..G-024 — Phase 4C 就绪评审发现的设计冲突（2026-10-08，按 mandate §4 先登记、不静默修改）

- **G-022 — OPEN / NON-BLOCKING / 需 ADR 修订**：`thread_entries.status`（`queued|delivered|discarded`）为
  D-4C-01/04/05/09 所需，但 Thread **D1 逐项列举了 `thread_entries` 的列，其中没有 `status`**（`source`/`kind`/
  `record jsonb`/`turn_id`/`node_execution_id`/`node_sequence`/`created_at`）；D3 **提到**了这三个状态却从未把列放进
  D1 的 schema，0018 也已按 D1 建表。**WHY 需登记**：给已列举的 schema 加列属持久化 schema 变更，§14 归类为架构性变更。
  **WHY 非阻塞**：语义已由 D3 + 不变量 4 批准，缺的只是列在 D1 中的存在性；且当前不存在 `source='user'` 行的写者，
  迁移的 CHECK 平凡成立。**WHAT IS NEEDED TO CLOSE**：`plan.md` §4R.7 的修订 A1；此外 0022 **不得依赖**「碰巧没有
  数据」（加 CHECK 前先对既有 `source='user'` 行回填 `status='queued'`，或显式断言该前提）。
  **更新（Phase 4C Slice 1 实施轮，2026-10-08）**：该条件的**迁移侧**已满足——`0022_thread_api_and_commands.sql`
  加列、**在加 CHECK 之前**对既有 `source='user'` 行回填 `status='queued'`，T4C-5 在「数据库里已存在这样一行」的
  升级路径上验证通过。缺口本身**仍 OPEN**：它说的是 D1 的枚举，修订 A1 仍为待批准；**未改任何 ADR 文件、未改任何 `status`**。
- **G-023 — OPEN / NON-BLOCKING / 需 ADR 修订**：D-4C-07/08 在**任何**改变该运行 Thread 可见状态的提交后发布
  `issue_run.thread_appended`，包括只翻转 `thread_entries.status` 或 `thread_state`、并未追加条目的提交；而 Thread
  **D5 写的是「条目写入事务提交后…发布」**，读起来只覆盖条目追加。**WHY 需登记**：事件名说的是 "appended"，客户端会
  依赖该契约。**WHY 非阻塞**：负载（`issueId`/`runId`/`lastSeq`）不变、事件只是失效提示，对客户端是加性的。
  **WHAT IS NEEDED TO CLOSE**：修订 A4；在落地前 S6 **只对追加了条目的提交**发布。
- **G-024 — OPEN / NON-BLOCKING / 需 ADR 修订**：D-4C-03 在 `cancel_requested_at IS NOT NULL` 时拒绝
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

`Phase 4C ACCELERATED IMPLEMENTATION BATCH 1 — S3（Thread Command Control Plane）+ S4（Thread POST）`

### 当前 Slice

`Phase 4C Batch 1（IMPLEMENTATION，单 Agent 连续推进）：S3（thread_commands 控制面 + A 缝 + 提交后 ThreadCommandAvailable + T4C-21..T4C-24）+ S4（公开 Thread POST + 同事务条目/状态/命令/幂等 + T4C-13..T4C-18）`

### 当前状态

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

### 本轮关键结论（Phase 4C 加速实施 Batch 1：S3 + S4 — 2026-10-08）

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

**Phase 4C 已实现（截至 Batch 1）**：S1（migration 0022 + `pending` 物化）、S3（`thread_commands` 控制面 +
`EnqueueThreadCommand` 缝 + `ThreadCommandAvailable` 提交后发布）、S4（公开 Thread POST + 同事务条目/状态/命令/幂等）
**已实现并通过真实 PostgreSQL 测试**。
**Phase 4C 仍未实现**：S2a（GET Thread 已批准子集）、S2b（GET 扩展，需 A3）、S5（echo→`delivered`、`active⇄idle`）、
S6（SSE `issue_run.thread_appended`，需 A4）、S7（`/thread/end`、idle 扫描、`ending→ended`，需 A2/G-018）、
`SessionEnded` / `DeliverySettled` / `RunWorkspaceDeleted`、`queued→discarded`、`DeliverRevision` / 上传授权
（`GrantRevisionUpload`）、完整 cancel API、Phase 5 / workspace failure policy / 收据 GC / event 保留清理 / 任意 event 上限。
**G-024 的两行有意未实现**（见 Blockers）。

### 当前 Blockers

**无 4C blocker**（就绪评审轮 verdict 为 `READY_FOR_PHASE_4C_IMPLEMENTATION_SLICE_1`；**Slice 1 / S3 / S4 均不依赖任何未决项**）。
**G-016 CLOSED**（Phase 4C 设计轮，D-4C-01）。**NON-BLOCKING OPEN**：G-017（GET 扩展需 ADR 修订——**S2b 前置**）、
G-018（用户主动结束端点缺失——**S7 前置**）、G-019（`ending→ended` 依赖 Phase 5 `SessionEnded`）、G-020（SSE 客户端
重连 + ≥30s 轮询义务）、G-021（多 worker 命令分区——S3 已按**单 worker 保证**交付，**有意未解决**）、
**G-022**（`thread_entries.status` 不在 Thread D1 列清单——需修订 A1；S1 已按**已批准**的 D3 实现该列，**不自动关闭**该 ADR 缺口）、
**G-023**（无新条目也发 `thread_appended`，与 D5 字面冲突——需 A4；未落地前 S6 只对追加条目的提交发布）、
**G-024**（`cancel_requested_at` 已置时拒绝 POST，与 D3 字面冲突——需 A2）。**S4 已交付其余全部接受矩阵行**，
仅 D-4C-03 的 `cancel_requested_at IS NOT NULL` 与「运行 Workspace 不活」两行**延后**（A2 未批准；由
`TestThreadMessageCancelRowIsDeferred` 钉住，A2 落地即转红）。
历史缺口：**G-012 PARTIAL**、**G-015 PARTIAL / NON-BLOCKING**、**G-009 CLOSED**、G-001 保持 PARTIAL
（Create/Delete RunWorkspace 与 `EnqueueThreadCommand` 归各自 Phase）、G-011 不在 Phase 4 顺手解决。
**controller-session ADR 仍 `proposed`** —— 是 desktop 侧独立待批准决策，**不是** Cloud 4C 的 blocker，
也**不是** 4C 任何决策的前提（D-4C-01..12 的论证**不依赖**它）。

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
