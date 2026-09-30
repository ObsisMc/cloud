# AgentRunDispatcher 实施计划

> 状态：**持续维护的设计 / 执行记录**  
> 范围负责人：**B — Cloud 业务 / 编排**  
> 当前目标：**Phase 3 DONE → Phase 4 DESIGN（next）**；当前指标 `PHASE_3B_DONE / READY_FOR_PHASE_4_DESIGN`（实现轮，3A B 核心 + 3B A 侧执行工作持久化 + 生产化 EnqueueExecutionWork 均已交付）。
> 更新规则：**每一轮实现开始前必须阅读本文件，结束前必须更新本文件。** 本文件是 `plan.md` 的中文对照；Phase 2
> 详细设计的权威版本在 `plan.md` 的 `## Phase 2 — Workspace Settlement 详细设计`（§2.1–§2.15），Phase 3
> 详细设计的权威版本在 `plan.md` 的 `## Phase 3 — Session Start 详细设计`（§3.1–§3.17），以及 §12 决策
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

状态：**Phase 3A IMPLEMENTED（B side）；Phase 3B IMPLEMENTED（A side real seam → PHASE_3_DONE）**；Phase 4（Thread）未开始

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

状态：**PLANNED**

包含：

- Thread entries
- takeover hooks
- running transitions
- Thread command control seam
- API / SSE

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
- **G-009** `thread_entries` seq=1 被保留，Phase 4 需从 seq=2 续接 — **开放（OPEN）**，跨 phase 接缝约束；确认 Phase 3A 仅写 seq=1，不破坏。
- **G-010** 从 `sandbox_instances`/`node_instances` 派生 D6 `target` — **以最小确定性 `sessionStartTarget` 关闭**（`{workspace_id, sandbox_instance_id, node_id}`，存在 live sandbox/connected Node 时）。
- **G-011** starting 下 workspace/target 无效缺失的精确终态 — **部分（PARTIAL）**：fail-closed 路径（保持 `starting`，不写终态，重试循环再触达）已实现并测试（`runWorkspaceLive`/`TestAgentSessionStartSoftDeletedWorkspaceFailsClosed`）；choose-path 权威决策留待后续批准（本轮按 §6/§11 选 fail-closed，绝不因测试方便写 `status=failed`）。

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

`Phase 3 — Session Start（3A B 核心 + 3B A-side Execution Work + EnqueueExecutionWork）：DONE（PHASE_3_DONE）；Phase 4（Thread / running）NOT_STARTED`

### 当前 Slice

`Phase 3A + 3B 实现完成（D-012..D-018、G-007..G-011、T3-1..T3-14 + T3B-1..T3B-14 + §28 concurrency + §29 payload-mismatch、3A/3B 拆分落地）`

### 当前状态

`PHASE_3_DONE / READY_FOR_PHASE_4_DESIGN / NOT_READY_FOR_PHASE_4_IMPLEMENTATION`

### 本 Slice 计划输出（Phase 3A）

- `AgentRunSessionStart.StartSession`（authoritative in-tx re-read、cancel-first、seq=1 once-guard、A/B 原子）
- `renderAgentInitialTurn`（D-013/G-007）
- `scanStartingAgentRuns` / `StartQueuedAgentSessionsOnce`（D-015，10s，IMPLEMENTATION CHOICE）
- cmd/server 启动循环接线

### 本 Slice 计划输出（Phase 3B — 本轮新增）

- `0019_execution_work.sql`：A-owned `execution_work` + `execution_work_unregistered_once` partial-unique + `execution_work_pickup`（D-016/G-008）
- `StoreAgentRunControlPlane.EnqueueExecutionWork`（生产 seam，INSERT + ON CONFLICT 幂等 + payload-match 守卫 D-018）+ `agent_work_claim`（D-017）
- cmd/server/main.go 接线 `store.AgentRunControlPlane = core.NewStoreAgentRunControlPlane()`；control.go `agent_work_` dispatch
- 迁移测试 `TestMigration0019ExecutionWorkAppliesFreshAndUpgrades`

### 本 Slice 明确不包含（仍属后续 phase）

- `CreateRunWorkspace` / `DeleteRunWorkspace` / `EnqueueThreadCommand` 生产实现（G-001 PARTIAL：执行 seam CLOSED，其余 seam 由 2A/2B、Phase 5、Phase 4 拥有）
- `phase=running` / Thread takeover（`ThreadEventsTakenOver`，Phase 4；D-014/D-017：claim 与 running 无关）
- seq>=2、Thread API/SSE、SessionEnded / DeliverySettled / RunWorkspaceDeleted、cancel 公开 API、通用插件执行平面、完整 Node worker（§3）
- `node_executions` / `node_event_receipts` / `thread_commands` 建表（§14 除非 pickup 需要）

### 当前 Blockers

无 B-side design blocker。G-008 CLOSED；G-001 执行 seam CLOSED、整体 PARTIAL（其余 A seam 方法由后续 phase 拥有）。
本轮实现轮交付：Phase 3A（B 核心）+ Phase 3B（A 侧 `execution_work` 真实落库 + 生产化 EnqueueExecutionWork + Controller claim
读取）。`agent_work_claim` 是纯读（D-017），物理启动（`phase=running`）仍属 Phase 4，需 takeover 证据（D-014）。
G-009（roster-current/real-agent controller target）OPEN，Phase 4 拥有。

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
