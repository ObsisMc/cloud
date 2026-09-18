# 开发进度（新员工向）

一眼看清 Ora Cloud 目前**做到哪了、在进行什么、刻意没做什么**。更新于 Issue Board 第三波
协作地基的第一个编码批次（3A — issue-owned 地基）完成后、3B+ 待规划。

图例：✅ 已完成 · 🚧 进行中 · 🧭 规划中（仅架构方案，未编码） · ⏸️ 刻意暂缓 · ❌ 未开始

## 平台核心

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| 租户 / 用户 / 成员 | ✅ | 含角色（admin/member）、最后管理员保护 |
| 身份认证（双 JWT） | ✅ | service + user 两层凭证 |
| 项目（project） | ✅ | 绑定 Git 仓库，生命周期管理 |
| 工作区（workspace） | ✅ | main / isolated，Git worktree |
| 异步操作（operation） | ✅ | 持久化、可重试、分步推进 |
| 幂等 + 乐观并发 | ✅ | Idempotency-Key + version |
| 真实节点 / 沙箱 / 存储 | ⏸️ | 目前用内存模拟器（`internal/simulator`）；生产 Substrate 是后续阶段 |

## Issue 看板（迁移自 Multica）

### 第一波 — 核心看板 ✅

| 功能 | 状态 |
| --- | --- |
| 7 个标准看板列 | ✅ |
| 5 级优先级 | ✅ |
| 标题 / 描述 / 负责人 / 创建人 / 父子任务 | ✅ |
| 拖拽排序（分数 position） | ✅ |
| 增删改查 + 跨列移动 | ✅ |

### 第二波 — 看板周边 ✅

| 功能 | 状态 | 备注 |
| --- | --- | --- |
| 自定义状态列（状态目录） | ✅ | 惰性播种 7 列，可加列、归档 |
| 卡片编号 `#42` | ✅ | 每租户递增 |
| 自定义字段 `properties` | ✅ | 无模式 jsonb |
| 评论 | ✅ | 增删改查 |
| 标签 | ✅ | 含挂/摘、软删复用名 |
| 订阅/关注 | ✅ | 存关系，暂无通知 |
| 搜索 `?q=` | ✅ | 标题/描述子串匹配 |
| 批量操作 | ✅ | 一次改多卡状态/优先级/负责人 |
| 保存视图 | ✅ | 存 filter，暂未在服务端执行 |
| 分组视图 | ✅ | 按状态/优先级/负责人分桶 |

### 第三波 — Issue 协作地基

架构方案见 [12-collaboration-architecture.md](../../migrations/multica-issue-board/12-collaboration-architecture.md)
（rev. 2：Issues 只拥有 Issue 域，外部能力一律走稳定 port）。已落地 **3A — issue-owned 地基**
（migration `0007` + 持久化/API-contract 脊柱）；其余为 3B+ 待编码。

#### 3A — issue-owned 地基 ✅（已编码）

| 功能 | 状态 | 说明 |
| --- | --- | --- |
| 多态 assignee（Assign ≠ Execute） | ✅ | `assignee_type ∈ user/agent/team` + `assignee_id`，`user` 镜像写 `assignee_user_id`；agent/team 为 opaque ref |
| 评论线程 + author ActorRef | ✅ | `parent_id`（同 issue 校验）+ 统一 `author_type/author_id`（user/agent/team/system）；无 per-actor 列 |
| IssueRun（Issue 拥有） | ✅ | `issue_runs`：`executor_type/executor_id` 多态（agent/team/workflow）+ opaque 外引用；≠ operation/execution_ticket |
| Run 状态机 | ✅ | 7 态；SQL WHERE 守卫（非中心校验器）；terminal ≠ deleted，无 delete-run API |
| Timeline（Projection） | ✅ | `issue_activities` + 评论共享 per-issue `seq`（Option-C `GREATEST(MAX,MAX)+1`）；投影非事件源 |
| Sub-Issue context | ✅ | `issue_context_refs`（引用而非复制，硬删） |
| 迁移 / API-contract 脊柱 | ✅ | `0007` + `PublicRequest`/`router`/`validField`/OpenAPI（`IssueRun`/`ContextRef` schema） |

#### 3B+ — 待编码 🧭

| 功能 | 状态 | 说明 |
| --- | --- | --- |
| 模块边界 + 集成端口 | 🧭 | Issues 拥有 Issue 域；Agent/Team/Workflow/Runtime/通知/实时/PR/日志走稳定 contract/port，三态 Unavailable/Simulator/Real |
| Issue Detail（协作产品面） | 🧭 | 左栏 Activity/Timeline + 右栏 Properties/Development/Execution（投影 + UI） |
| Agent/Team 引用（临时调试目录） | 🧭 | Issue 只存 actor ref；dev 用 `sim_*` 目录 + FakeResolver，明确临时、有替换边界 |
| 显式 @targets + 对话目标 | 🧭 | 显式解析目标为 contract（非纯正则）；ConversationTarget 续接当前执行者 |
| Workflow Invocation | 🧭 | 可执行 capability（非 Actor）；schema → 预填 → 补缺 → 用户确认 → 调用 |
| 模拟执行器（adapter） | 🧭 | Fake 适配器实现同一 port；Issue 核心无 `if runtimeExists` |

### 刻意暂缓（本期不做）

| 功能 | 状态 | 原因 |
| --- | --- | --- |
| 附件 / 文件上传 | ⏸️ | Cloud 无文件存储 |
| 卡片绑定 project | ⏸️ | Cloud 的 project 语义不同（开发环境，非轻量分组） |
| 关联 Pull Request | ⏸️ | 需接外部 Git 服务 |
| 实时推送（WebSocket） | ⏸️ | 无事件总线 |
| 机器人 / 小组负责人 / Workflow / Autopilot | 🧭⏸️ | agent/team/workflow 走第三波的端口 + 模拟适配器；Autopilot 本体仍暂缓 |
| 富表格 / 图形视图 | ⏸️ | 分组视图已是一等端点，任意视图引擎未做 |

## 已知限制

- 看板列表不分页（一次返回整个租户的所有卡片）。
- 搜索只是标题/描述的子串匹配，没有全文索引，也不搜标签/编号。
- 保存视图的 `filter` 只存不执行（由客户端解释）。
- 订阅只是存了关系，没有通知管线。
- 全局 advisory 锁是明确的吞吐上限。

## 里程碑时间线

| 时间 | 里程碑 |
| --- | --- |
| 阶段一 | Cloud 核心 + 模拟器（tenants/projects/workspaces/operations） |
| — | Issue 看板 第一波（核心看板，migration 0005） |
| 最近 | Issue 看板 第二波（周边功能，migration 0006）+ 文档体系 |
| — | Issue 协作地基 3A（issue-owned 地基，migration 0007） |
| 下一步 | Issue 协作地基 3B+（ports → 模拟适配器 → Issue Detail 投影）＋ 生产 Substrate / 看板分页与全文搜索 / 实时推送 |

> 想看每个功能对应的接口和表，去 [../agent/api-reference.md](../agent/api-reference.md) 和
> [../agent/database.md](../agent/database.md)。想看迁移的完整决策记录，去
> [../../migrations/multica-issue-board/](../../migrations/multica-issue-board/)。
