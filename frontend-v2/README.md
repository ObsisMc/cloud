# frontend-v2

Cloud v2 的正式 Web 前端，中文界面。技术栈：Vite、React 19、TypeScript、Tailwind CSS 4、shadcn（base-nova）、TanStack Query、zustand、React Router，客户端由 orval 从 `api/openapi.json` 生成。

## 开发

需要 Node 24、Go、Docker（跑 PostgreSQL）和 [Task](https://taskfile.dev)。在仓库根目录：

```sh
task v2:dev    # PostgreSQL + v2-server（:8090）+ 前端（http://localhost:5173）
task v2:e2e    # Playwright 端到端测试
```

`task v2:dev` 不注入任何模型 provider，因此 Agent 与模型目录为空；要显示真实模型，设置
`V2_MODEL_*` / `V2_ANTHROPIC_MODEL_*`，或使用带 DeepSeek 配置的 `task v2:deepseek`。

打开页面后在登录页填预置账号的邮箱（默认 owner 是 `owner@demo.ora-space.dev`，见 `v2-server/users.json`）即可进入；没有密码，只用于演示。只改前端时也可以在本目录：

```sh
npm ci
npm run dev           # /api、/auth、/healthz 代理到 v2-server（V2_SERVER，默认 :8090）
npm run check         # 类型检查、lint、单元测试、构建、扫描构建产物里的演示 mock；CI 的 v2 workflow 跑同一条
npm run api:generate  # api/openapi.json 变化后重新生成 src/api
npm run e2e           # 端到端测试，自己拉起 v2-server（:8091）和前端（:5174）
```

录屏用的演示 mock 都在 `src/lib/demo/`，只在 `VITE_DEMO_MOCK=1` 时打开（`task v2:demo` 等录屏命令会设上）。开关是 `flags.ts` 里的构建时常量 `DEMO_MOCK`，调用处要直接读它（`DEMO_MOCK && …`），不要包进函数，这样不带开关的构建会把 mock 的数据和文案整段剔除。`npm run demo:check` 扫描 `dist`，发现 `registry.ts` 里登记的任一 mock 标记就失败；CI 另外用 `VITE_DEMO_MOCK=1` 构建一次，确认每个标记都还能找到。新加 mock 时在 `registry.ts` 登记并给出标记。

端到端测试默认连接 `task v2:db` 的 PostgreSQL（库 `ora_v2_e2e`，可用 `E2E_DATABASE_URL` 指定）。第一次运行前执行 `npx playwright install chromium`；本机已有 Chromium 时，也可以用 `PW_CHROMIUM_PATH` 指定可执行文件。

默认不运行真实 Agent，和 CI 一样。Playwright 拉起 v2-server 时传 `-test-fixtures`，fixture 只存在于测试启动的 v2-server 进程，正常启动不会出现在用户目录中。设了 `V2_NODE_ASSETS_DIR`（先 `task v2:node:prepare`）后，对话的端到端测试改为在真实 Node 和 Agent 上跑 fixture 模型：新建对话等到真实回复，停止另一个对话后只读，刷新后历史一致；工作流运行的用例改为断言两个 Agent 串起来的运行成功，并截三张图（结果、全图、节点会话）到测试输出目录。没有 Node 资源时，工作流运行的用例断言第一个 Agent 节点因没有 Agent 可用而失败。项目第一次启动 Node 要装插件，整条用例约一两分钟。`V2_DATA_DIR` 要用短路径（如 `mktemp -d /tmp/v2e2e.XXXX`），否则 Node 的 Unix socket 路径会超长；也别和正在运行的 `task v2:dev` 共用同一个数据目录。

## 结构

```text
src/
  app/          外壳：Provider、路由、布局、左侧图标栏、登录（auth/）
  features/     每个页面一个目录：chat、collaboration、workflows、schedules、dashboard
  api/          orval 生成的客户端，不手改
  components/   跨页面共用的组件：左栏拖宽手柄（sidebar-resize-handle）；ui/ 是 shadcn 生成的组件，不手改
  lib/          跨页面共用：HTTP 实例（api-client）、登录接口（auth-api）、代码高亮（highlighter，shadcn 生成）、细滚动条样式（scrollbar）
  hooks/        shadcn 生成的 hook
  test/         单元测试的公共设置和假后端
e2e/            Playwright 端到端测试
```

左侧图标栏的每个图标对应 `src/features/<name>/` 里的一个页面。新增页面：

1. 新建 `src/features/<name>/index.ts`，导出 `Feature`（路径、标题、图标、按需加载的页面）；页面写在同目录的 `page.tsx`。
2. 在 `src/app/features.ts` 里按导航顺序加一行。

各页面互不引用，可以并行开发；页面首次访问时才加载，构建时各自拆包。

## 对话

`src/features/chat/` 是对话页，界面对齐 desktop 的对话界面。组件用 shadcn 的 chat 组件（MessageScroller、Message、Bubble、Trace、Code Block、Attachment、Marker），复制进来的 AI Elements pattern 放在 `components/`。Code Block 的高亮依赖 shadcn 一起生成的 `src/lib/highlighter.tsx` 和 `src/hooks/use-highlighted-code.ts`。

- `model/`：把 ACP 事件（`session/update`、权限请求、轮次结束）归约成界面条目，再排成显示用的行（活动块、子代理分组、改动文件）。后端对话的历史快照由 `model/history.ts` 每次整份重建成轮次，走同一个归约器。
- `api/`：由 `v2-server/chat.openapi.json` 生成的客户端（`npm run api:generate:chat`），不手改。
- `remote.ts`：对后端的调用和事件流（Server-Sent Events）。`store.ts`：项目、对话和事件流的状态。

左栏按项目分组：项目和对话都来自后端，对话直接列在所属项目下；拖动项目可调整顺序（存到后端），拖动左栏右边缘可调宽度或收起（存在浏览器本地）。

新对话的排版照 desktop：没有对话选中时显示新对话，标题和输入框在页面中间，输入框上方选项目和启动模式（网页上只有云端），旁边一个"沙盒"标签说明每个对话在独立的沙盒中运行（悬停有说明）；第一条消息发出时才由后端建对话，输入框滑到底部。输入框右下角先选 Agent 再选模型，清单来自后端的执行器。

后端对话的规则跟 `v2-server` 的 chat 一致（见 `v2-server/README.md` 的"对话"一节）：

- 每个对话一个执行 session，在独立的沙盒里运行：代码从项目仓库 clone 而来，改动不会写回，对话之间互不占用；顶栏同样标"沙盒"。
- 停止只停这一轮：后端在停止后保留了 session 时，这一轮标为"已停止"，输入框照常可用，接着发消息还在同一个 session 里（排队中的消息随停止一起丢弃）。Node 不能保留 session 时停止会结束整个 session，服务重启也会。之后对话只读：线程末尾标出原因，输入框换成"新对话"按钮，不会在同一对话里重开 session。
- 上一轮还没结束时发的消息进入排队；后端如果先一步拒绝（`turn_running`），消息回到队首，等这一轮的结束事件再发。
- 只能发文字，对话不能换执行器；新建项目等后端支持后再开放（`backendSupports`）。
- 顶栏的「工作区」打开右侧面板（`src/components/workspace-panel/`，对话、任务运行和工作流运行共用）。「文件」浏览沙盒里的现场；「变更」和单文件 diff 读这个对话最近一次成功的回合交付（见 `v2-server/README.md` 的「回合交付」「Agent 工作区查看」），diff 固定读列表显示的那次交付。一轮进行中时提示「本轮进行中，显示的是上一轮的交付」，这时描述所显示交付的文字都说「上一轮」（如「上一轮没有改动」「上一轮交付失败：原因」）。对话和任务运行只在服务端排序下最近一次结果（`latestDelivery`）是失败时提示「本轮交付失败：原因」，后来的成功交付会取代它；工作流运行的各 Agent 节点是各自的尝试，只要有 `latestFailedDelivery` 就提示「节点 <nodeId> 的交付失败：原因」，其他节点之后成功交付也不会隐藏它（ADR D5）。列表始终是最近一次成功的交付。成功交付有「下载 patch」（下载期间按钮禁用），保存为 `delivery-<id>.patch`，可以在起点提交上 `git apply`；没有改动的交付不提供下载；超过下载上限时显示服务端的「改动过大，无法在线下载」。功能上线前的旧对话（`mode: working_directory`）照旧读沙盒，没有下载。
- 没有在看、也没有在等回复的对话会关掉事件流，再打开时从上次的序号接着读。

后端还不能运行 Agent 时，新对话上方会提示。

思考耗时根据服务端持久化的预览事件时间估算，显示“思考了约 X 秒”；它表示首次观察到思考至同一来源开始回答、调用工具或本轮结束的时间，受预览节流和传输延迟影响。最终快照不会用落盘时间覆盖这个估算，刷新后通过事件重放恢复。只有定型历史、没有观察到思考开始，或思考与回答首次出现在同一事件中时，只显示“思考”，不推算耗时。对话、工作流节点会话和任务会话共用此逻辑。

### 运行工作流

工作流的运行照 desktop 放在对话页里（`workflow-run/`）：项目行 `+` 菜单里的"运行工作流"列出服务端已发布的工作流（每次打开菜单时重新读取，读取失败时不显示旧列表，可以重试；顶部有搜索框，列表过长时滚动）。列表只给版本名，所以菜单会再逐个读出发布快照的 id；创建运行时按这个 id 核对：菜单打开后生效版本被换掉，或者同名版本删掉后重新发布成了另一张图，都拒绝创建，提示重新选择。选一个后给这次运行起名，在服务端创建出一次未开始的运行。运行和会话一起列在项目下面，带状态点；选中后右侧换成运行页，选会话或新建对话就换回来。对话页地址带 `?run=<id>` 时直接打开那个运行（定时任务的运行历史用它链接到运行页），打开后去掉这个参数。

新对话输入区的项目选择旁也有显眼的“运行工作流”按钮，与项目 `+` 菜单共用已发布列表、搜索、失败重试和版本快照校验。先选择项目，再选择工作流创建运行。成功的运行提供“在对话中追问”，把原需求、运行 ID 和交付说明填入该项目的新对话草稿，由用户确认后发送；草稿会说明新对话使用独立沙盒，不能假定工作流修改已出现在当前目录。

运行页和 desktop 一样有两个视图，标题栏切换：

- **舞台**：上方是执行路径，下方一次演一个节点的卡片，跟着正在执行的节点走；并行、条件、循环和迭代各有自己的展示。卡片右上角打开节点详情（Agent 配置、执行时间、成果、失败重试），右下角打开节点会话。运行结束后显示结果卡片。运行中进入运行页、或点"启动"后都先到舞台，Esc 回到全图。
- **全图**：冻结的整张图，每个节点显示状态；点节点回到舞台并聚焦它。

标题栏还有"启动"（先填开始节点的变量和初始提示词；冻结的图里执行路径上有 v2-server 不执行的节点——`prompt`、`tool`，desktop 也不执行——时，对话框先按服务端 analyze 的 `unsupportedNodes` 列出这些节点。分析完成前暂不可提交；分析失败时说明将由服务端校验；无法发起分析（例如没有租户）时按没有分析处理，不一直显示“正在检查”。警告只提示，启动仍由服务端拒绝；备用节点不提示）、"停止"、"再跑一次"（新建一次运行，旧的留在列表里），以及失败或已取消、且至少有一个节点失败或被取消的运行才有的"从失败处继续"（运行没开始就取消时只有"再跑一次"；任务的处理运行和拆分运行——运行详情 `run.taskLink` 为 `handle` 或 `decomposition`（owner 决定 D25、D32）——也不提供，节点详情里的续跑提示同样隐藏，用户回到任务页重新开始处理；确认表单的「重试任务创建」对处理运行同样隐藏（owner 决定 D30），改为提示回到任务页，任务创建阶段的失败说明同样指回任务、不显示「回到确认节点重试」，服务端也按 `task_handling` 拒绝；拆分运行同样隐藏（D32），提示回到任务页重新拆分，服务端按 `task_decomposition` 拒绝）：它和"再跑一次"分开，打开后先读服务端的续跑预览（`GET …/resume-preview`），列出从哪些失败或取消的节点继续（容器内的节点注明所在容器）、哪些 Loop / Iteration 从第一轮重来、随后还要重跑的下游节点，并说明工作区文件保持现状、不回滚；服务端拒绝时说明原因并禁用按钮：运行没结束；还有节点在运行（列出节点）；节点停在运行中不会再结束，运行停在没有提交的确认表单上，或已经提交的表单因为容器重来要被清掉（这几种都让用户"再跑一次"；提交后任务创建失败时让用户去确认页点"重试任务创建"）；任务的处理运行（回到任务页重新开始处理）；没有失败节点；上一次执行还在收尾（稍后再试）。确认后同一次运行回到执行中，运行页切到舞台实时跟随，再次结束时照常提示"运行已结束"；续跑被服务端拒绝时（包括预览没能预见的"还在收尾"），对话框直接显示拒绝原因并重新读取预览，不弹通用错误；拒绝只在预览没变时有效，预览一变（例如收尾结束）就清掉，预览自己拒绝时只显示预览的原因，不重复提示；预览说"还在收尾"或有拒绝时，对话框开着就定时重读预览；下次打开对话框时也清掉上次的拒绝。节点详情里的"可在顶部点击「从失败处继续」…"只在续跑能帮上忙时显示（`resume-eligibility.ts`）：失败详情 `resumable` 为假的配置类失败（如 `missing_agent_ref`）不显示，除非下次尝试会被告知这次失败。侧栏行的菜单可以删除运行，执行中的运行先取消再删除。Agent 节点的对话只在运行页里看，不进聊天记录，desktop 也是这样。

| 文件 | 来自 desktop `packages/app-shell/src/features/workflow-run` |
|---|---|
| `run-theater*`、`run-act-*`、`run-overview-*`、`run-status-*`、`run-loop-*`、`run-condition-stage.tsx`、`run-result-act.tsx`、`run-brief-popover.tsx`、`run-hitl-composer.tsx`、`use-theater-hitl.tsx`、`run-path-structure.ts`、`run-focus.ts`、`run-node-completion.ts` 和其余同名小文件 | 同名文件，只改了 import 路径；`run-act-agent-config.tsx` 另外挂上 `run-act-v2-execution.tsx`，`run-act-inspector.tsx` 把节点状态传给它 |
| `run-act-v2-execution.tsx` | 新写：节点详情里 Agent 实际使用的执行器和模型，以及 v2 没有应用的设置（见下文） |
| `workflow-run-start-dialog.tsx`、`translations.ts` | 同名文件；`translations.ts` 里的 `workflowRun.v2.*` 是 v2 新写组件的文案 |
| `run-node-session-chat.tsx`、`node-session.ts`、`conversation-turns.ts` | 新写：desktop 在这里嵌入完整的聊天视图读取节点的真实会话；这里按节点尝试订阅服务端的节点会话事件流（`node-session.ts`，见下文），用对话页的会话归并得到完整对话；读不到时用服务端运行带回的对话记录，`conversation-turns.ts` 把它们转成对话页的轮次（发给 Agent 的消息开启一轮，之后的回复、思考和工具调用归入这一轮）。两者都交给对话页的 `Thread` 渲染，所以节点会话和对话页的样式一致；只有交互式节点能在里面接着发消息（见下文） |
| `run-node-session-actions.tsx`、`run-starter.ts` | 新写：交互式 Agent 节点会话下方发起人的输入框、“停止本轮”和“完成”，以及“当前用户是否发起人”的上下文（见下文） |
| `run-events.ts` | 新写：订阅运行的失效事件流（见下文） |
| `hitl-text-editor.tsx` | 新写：人工介入回答框，用纯文本输入框代替 desktop 的 TipTap 编辑器 |
| `demo-host.ts` | 新写：Theater 调用的 desktop hook（保存启动输入、回答人工介入、错误提示等），同名同参数；保存启动输入走服务端，其余见下文 |
| `run-pane.tsx`、`run-sidebar.tsx` | 新写：desktop 的运行页、侧栏行和运行对话框的精简版 |
| `run-resume-dialog.tsx`、`resume-eligibility.ts` | 新写：desktop 续跑对话框的精简版，只有“保留现状”一种方式，内容来自服务端的续跑预览；文案在 `translations.ts` 的 `workflowRun.resume.*` |
| `task-owned-retry.ts` | 新写：任务所有的运行（处理运行、拆分运行）不能「重试任务创建」时指回任务页的文字（D30、D32），确认表单和任务创建阶段共用 |
| `server-runs.ts`、`server-run-adapter.ts` | 新写：调用服务端的运行接口，把服务端的运行投影成 Theater 读取的 `GraphWorkflowRun`；`test-run-server.ts` 是单元测试的假服务端 |

运行由 v2-server 执行（见 `v2-server/README.md` 的"工作流执行"一节），客户端和工作流定义一样由 `v2-server/workflows.openapi.json` 生成。`server-runs.ts` 把创建、启动、修改启动输入、取消、删除、再跑一次、续跑预览和从失败处继续接到生成的客户端上（续跑预览挂在运行的查询键下面，运行每次失效时一起重新读取，不缓存），拒绝同样转成 `ContractError`；运行详情、冻结的图和项目下的运行列表由 TanStack Query 读取。运行没结束时，运行页照 desktop 订阅它的失效事件流（`GET …/workflow-runs/{rid}/events`，`run-events.ts`）：每条 `workflow_run_invalidated` 都让运行详情和项目运行列表重新读取，事件本身不带状态，页面只信 HTTP 读到的持久状态。轮询是兜底（`runRefreshMs`）：事件流连着时每 4 秒（desktop 的兜底间隔），没连上或断开时每 1 秒；运行结束后关闭事件流。侧边栏的项目运行列表照旧在有未结束的运行时每 1 秒读一次。刷新页面后运行还在，执行也不依赖页面开着。

`server-run-adapter.ts` 把服务端的节点运行按 desktop 读取后端运行时的规则投影成节点状态：节点状态、开始结束时间、输入输出、失败原因和失败详情（`payload.error_detail`）照搬。运行详情的 `variables`（根变量池）和 `failedAttempts`（被替换的失败尝试）也照 desktop 的运行适配器读：开始节点显示这次运行的初始输入，输入变量显示运行变量池里的值（声明了但没赋值的显示为空；运行开始前还没有变量池，先保留快照里的值），所以节点详情的“输入变量”是这次运行实际用的值；之前失败的尝试按节点归到“之前失败的尝试”，从旧到新，带着各自的会话和失败原因，根作用域、Iteration 轮和 Loop 轮内的尝试各归各轮（v2 对非交互 Agent 一律自动重试，是否重试由 v2-server 决定，见其 README 的「Agent 自动重试」）；“已自动重试”“已安排自动重试”只按服务端持久化的 `retry_chain` 标，不从配置推导。“已手动续跑”照 desktop 的 `markReplacements`，对根作用域的节点和 Iteration 成员（每一轮按同一 `(节点, 迭代轮)` 的当前行比较，不拿别的轮的行来标）；Loop 成员在续跑开出的新轮里尝试号从 1 重新数，旧轮没有可比的当前行，所以不标：自动重试总会把被替换的尝试记进当前行的 `retry_chain`，所以比这条链（没有链时比当前行）更早的最新一次尝试是被“从失败处继续”（见上文）替换的；知道尝试号时要求两者相邻，中间隔着被清掉的行就不标。desktop 还要求当前行自己记下尝试号，v2 对刚续跑、还没记下号的新行也标，因为 v2 里只有续跑会软删根作用域的失败行而不留链接。等待重试的尝试显示倒计时；运行已结束时，留下的等待行显示为已取消。运行列表行与运行头部在服务端返回了发起人姓名时展示“由 <发起人> 发起”；节点会话按节点尝试（`nodeRunId`，适配器从节点行带上）读：尝试绑定了会话后，`node-session.ts` 订阅 `GET …/workflow-runs/{rid}/nodes/{nodeRunId}/events`，服务端校验当前用户的租户成员身份后按启动者的会话读出完整事件流（没有记录发起人的运行回退为调用者；续跑这样的运行会把续跑的人记为发起人），帧格式和对话页的事件流相同，用对话页的 `applySessionEvent` / `conversationOf` 归并，所以节点会话对整个租户可见，包括 Agent 读取的文件内容和工具调用，在共享运行里选用个人 Skill 就等于把它的内容共享给能看到这次运行的所有成员（从任务评论里发起的运行同样适用，发起人就是评论人），运行中和历史回放都会完整呈现。执行桥在一轮结束后就结束节点的会话，所以不显示对话页“会话已结束”的标记。若事件流尚未派发或被服务端拒收（浏览器看到事件流一开始就被永久关闭），节点会话提示无法读取实时会话，不再一直显示“正在等待”；还没有收到任何事件时，节点会话退回两条消息：发给 Agent 的任务（服务端记下的渲染后任务 `payload.task`，没有时用节点的输入）和 Agent 的回答。成功的 Agent 还带着 v2 自己记下的三项：实际使用的执行器（`payload.executor`，Agent 名和插件版本）、模型（`payload.model`；新运行里配置的模型找不到或没配时节点以 `workflow_model_not_found` 失败、不启动会话，所以 `fallback` 总为假，只有旧运行可能为真）和没有应用的设置（`payload.unapplied`）。节点详情在配置的"Agent 模型"下面显示"实际执行"；旧运行退回过默认模型的，仍说明原因。

节点详情列出"v2 未应用"，按服务端的顺序，前端不从节点配置推导（规则在 v2-server，见其 README）：尝试在结果 `payload.unapplied` 里记下了列表（v2-server 只在成功时写）就显示它，哪怕是空的；没有记录时（还没运行、运行中、失败、取消、中断，或没有这项记录的旧运行）显示 v2-server 对这次运行冻结图的 analyze 结果里该节点的 `unappliedSettings`。这个结果按当前服务端的规则给出，不是那次运行当时的规则，所以根 Agent 自动重试上线前就已结束、又没有记录列表的运行不会显示自动重试的提示，虽然它当时没有重试。运行面板对它展示的每次运行都分析一次冻结图（按运行缓存、不重新请求），启动对话框的预检读的也是这一次分析。打开节点会话可查看原选择、当前配置、ACP 已送达的版本和宿主健康。重新加载后按原节点尝试读取；宿主失联时显示未知。发起人可对已配置的成员重新探测。旧 Node 缺少生命周期能力时显示 mcp_node_capability_missing。插件或设置问题显示稳定失败码及相关插件 ID、设置 ID，Node 或服务级错误不归因到某个插件。新运行不再标记 MCP 为未应用。历史运行记下的 `unapplied` 仍保留，所以旧运行不会被改写成已经应用 Skill。

交互式 Agent 节点（简化版，服务端规则见 `v2-server/README.md` 的“交互式 Agent 节点”）：服务端从首轮开始就在 `payload.interactive` 里记下会话摘要，只给它当作交互会话执行的节点记；适配器据此得到 `interactiveSession`（轮数、会话是否已结束、能否完成、阻止原因、完成会采用的回答、发起人发过几条消息、最后一条是否没有送达），运行详情的 `startedBy` 得到发起人。是否把节点当交互会话看只认这份摘要，不看 `agentConfig.interactive`：Loop 里开了交互模式的 Agent 由 v2 自动执行、没有摘要，所以不显示“停止本轮”和简化版说明。舞台卡片和路径条显示“等待输入”；打开节点会话后，发起人（`run-pane.tsx` 用登录用户和 `startedBy` 比较，经 `RunStarterContext` 传下去）看到输入框（Enter 发送）、执行中的“停止本轮”和“完成”，以及“当前为简化版：由发起人确认完成”的说明；停止本轮后会话还在时可以接着发消息，会话结束后（停止时没能保留会话，或 Agent 断开；运行里其他 Agent 开始执行不影响等待中的会话）输入框收起，只能用最后一次正常结束的回答完成，不能完成时按钮禁用并说明原因。发消息的接口先返回成功、服务端之后才把消息交给会话；交不进去时摘要带 `last_turn_error: "not_delivered"`，说的是第 `sent` 条消息，输入框上方提示“上一条消息未送达，请重发”。未送达提示只对发起人显示：只有发起人能重发，其他成员看到的仍是只读说明。发出这条消息的视图记着它的内容和发消息接口返回的序号（响应里的 `sent`，由服务端编号，不从屏幕上的摘要推算，所以发起人的另一个标签页同时在发也不会认错），序号和摘要的 `sent` 对得上时可以“重新填入”（会话已结束的只提示，不能重发：提示说消息没送到、会话也已结束，并给出两条出路：用上一轮的回答完成这一步，或者先停止运行再“从失败处继续”重新开始这一步，不再另显示“会话已结束”）；被服务端拒绝的发送不记；屏幕上的摘要比这个视图刚发出的消息还旧时，提示说的是更早的消息，先不显示。下一条消息被接受后提示消失。其他成员同样能看到完整会话和回放，但没有输入框、“停止本轮”和“完成”；会话还没派发或读不到时，退回任务和到目前为止的回答。消息、停止和完成调用 `server-runs.ts` 的 `sendNodeTurn`、`stopNodeTurn`、`completeNode`（生成自 `v2-server/workflows.openapi.json`），回答经节点会话事件流到达。desktop 的步骤卡片、确认门禁和强制完成不搬。交互节点会话没能建立时，服务端用 v2 自己的失败类型 `interactive_session`（不自动重试），`node-failure-kinds.ts` 在 desktop 的列表后加了这一项和它的文案。运行的发起人已无法在工作区执行（被移出团队、账号停用或工作区暂停）而 Agent 被拒时，服务端用另一个 v2 专有类型 `starter_unavailable`（不自动重试，规则见 `v2-server/README.md` 的「从失败处继续」），同样加在这个列表里；续跑和「重试任务创建」遇到这种运行时返回 409 `starter_unavailable`，续跑对话框和确认表单都改为说明原因、让用户「再跑一次」。

Interactive Agent nodes (simplified; server rules in `v2-server/README.md`, "Interactive Agent nodes"): from its first turn on, v2-server records a session summary in `payload.interactive`, only for a node it runs as an interactive session, and the adapter reads it into `interactiveSession` (turns, whether the session ended, whether it can complete, the blocker, the answer completion would accept, how many messages the starter has sent and whether the last of them was not delivered) and the run detail's `startedBy` into the run. Only that summary makes a node an interactive session, never `agentConfig.interactive`: a Loop member with interactive mode runs automatically in v2 and has no summary, so it shows neither "停止本轮" nor the simplified-version note. The stage card and the path rail say "等待输入"; in the node session the starter (compared with the signed-in user in `run-pane.tsx` and passed down through `RunStarterContext`) gets a composer (Enter sends), "停止本轮" while a turn runs and "完成", with the note that completion is confirmed by the starter in this simplified version; after "停止本轮" the starter can keep talking while the session lasts. Once the session has ended (it could not be kept when the turn was stopped, or the agent disconnected; other agents of the run starting leave a waiting session alone) the composer goes away and only completion with the last normal answer is left; a blocked completion is disabled with its reason. Sending a message returns before the server hands it to the session; when that fails the summary carries `last_turn_error: "not_delivered"`, which is about message number `sent`, and a notice above the composer says the last message was not delivered. Only the starter sees the notice: nobody else can send again, so other members keep the read-only line. The view that sent the message remembers its text and the number the send answered with (`sent` in the response, assigned by the server rather than counted from the summary on screen, so another tab of the starter's sending at the same time is never mistaken for it), and offers to put the text back when that number equals the summary's `sent` (a session that has ended gets the notice only: it says the message was not delivered and the session has ended, and offers both ways on: complete the step with the previous answer, or stop the run and resume from the failure to start the step again; the separate “session ended” line is not shown). A send the server refused is not remembered. A summary older than the message this view just sent has a notice about an earlier message, so it is not shown yet. The notice goes once the next message is accepted. Other members can read the whole session and its replay but get no composer, "停止本轮" or "完成"; before dispatch, or when the session cannot be read, they fall back to the task and the answer so far. Turns, stops and completion call `sendNodeTurn`, `stopNodeTurn` and `completeNode` in `server-runs.ts`, generated from `v2-server/workflows.openapi.json`; answers arrive over the node's session events. Desktop's step card, confirmation gates and forced completion are not ported. When an interactive node's session cannot be set up, the server uses v2's own failure kind `interactive_session` (never auto-retried); `node-failure-kinds.ts` appends it to desktop's list, with its labels. A second v2-only kind, `starter_unavailable` (never auto-retried; server rules under “Resume from failure” in `v2-server/README.md`), marks an Agent refused because the run's starter can no longer act in the workspace (removed, account disabled, or the workspace paused), and is appended to the same list; resume and “Retry task creation” answer such a run with 409 `starter_unavailable`, and the resume dialog and the review form explain it and point to “Run again”.

v2 只做核心执行，有几项界面在、但不会出现或不可用：服务端不执行 desktop 的人工介入（HITL）闸门，回答人工介入的 hook 直接拒绝（交互式 Agent 节点见上文）；非交互 Agent（根作用域与 Loop / Iteration 成员）都自动重试，没有从失败处恢复，Iteration 内重试耗尽后按配置继续；AI 诊断、会话索引和文件变更评审没有搬。条件分支没走的节点投影为“未激活分支”，规则沿用 desktop 的 `computeInactiveNodes`。根决策读取 `conditionDecisions`；Iteration 以 `iterationConditionDecisions` 和 0 基 `iteration` 投影 `roundStates`，包含本轮未走的成员；Loop 按 scope 的 1 基 `roundIndex`、独立决策和节点行投影 `rounds`。切换轮次只展示该轮会话与输出，循环结果区分条件满足、达到上限和 LoopExit 主动退出。

## 工作流

`src/features/workflows/` 是工作流页，从 desktop（`ora-space/desktop` main `cab59a3c`）的工作流编辑器原样移植：左栏是工作流列表（新建、复制、重命名、删除、导入、导出），右边是 React Flow 画布、节点检查器、撤销重做、草稿自动保存、发布和版本历史。图结构就是 desktop 的工作流文档，导出的文件和 desktop 互通。

对话页也要读工作流库、运行工作流，所以编辑器以外的部分放在 `src/lib/workflows/`，两个页面共用；只有工作流页用的留在 `src/features/workflows/`。

| 目录 | 来自 desktop |
|---|---|
| `features/workflows/editor/` | `packages/app-shell/src/features/workflow-editor`（`translations.ts`、`workflow-transfer.ts` 在 `lib/workflows/editor/`） |
| `features/workflows/composer-core/`、`prompt-composer/` | `packages/editor` 的 composer 和 app-shell 的提示词编辑器（tiptap） |
| `lib/workflows/node-chrome/` | `packages/app-shell/src/features/workflow-node-chrome` |
| `lib/workflows/mock/`、`runtime/` | `packages/workflow-mock`、`packages/workflow-runtime` |
| `lib/workflows/data/workflows.ts`、`data/workflow-analysis.ts` | `packages/app-shell/src/state/data` |
| `lib/workflows/contracts/dto/` | `packages/contracts` 生成的 DTO 类型 |

复制来的文件只改了 import 路径、几处 v2 的 lint 或 TypeScript 设置不接受的写法，以及几处网页上的差别：编辑器标题栏去掉了返回按钮（页面上没有可返回的地方）；草稿只存在服务端：页面刷新或关闭（`pagehide`）时立即发起补存，不等 1 秒的防抖，但请求可能来不及完成，所以有未保存的改动时浏览器会先确认（`beforeunload`）；在应用里切到别的页面时先保存，保存失败才询问是否仍要离开（`editor/workflow-leave-guard.tsx`）；标题旁的保存状态在服务端确认之前显示"未保存"或"保存中…"；列表用共用的细滚动条。desktop 依赖的宿主能力在 `lib/workflows/host/` 里换成浏览器实现：`ui.ts` 转出 v2 生成的 shadcn 组件；`i18n.ts` 只加载 desktop 的 zh-CN 文案；`platform.tsx` 用浏览器下载代替保存对话框；`window-chrome.tsx` 去掉了 desktop 的窗口拖拽和窗口按钮。页面自带一个 QueryClient，沿用 desktop 的缓存策略（`staleTime` 30 秒），编辑器的缓存补丁和自动保存依赖这一点。

左栏和对话页的聊天记录用同一个拖宽手柄（`src/components/sidebar-resize-handle.tsx`）和同一种细滚动条（`src/lib/scrollbar.ts`）：拖动右边缘调宽度，拖过最小宽度就收起，也可以聚焦边缘后用方向键调整、按回车切换；收起后从编辑器标题栏的按钮展开。宽度和是否收起保存在浏览器本地（`lib/workflows/host/ui-store.ts`）。提示条右上角有关闭按钮。

`lib/workflows/samples/` 是样例工作流：`authored.ts` 写了两份，代码审查（开始 → Agent → 条件分支 → 两个 Agent → 变量聚合器 → 输出）和逐段翻译（迭代）；另外三份是 desktop 自带的 Loop 样例（`artifacts/workflow-loop-examples/`），原样复制，导入时把 Agent 改成演示执行器、去掉角色。五份合起来覆盖全部八种可执行节点。工作流库是整个团队共用的，所以样例不再自动加入：左栏标题旁的"添加样例工作流"按钮按导入对话框的同一条路径把它们加进库、发布为 `v1`；同名的跳过，发布被拒绝的留作草稿并提示。

工作流存在 v2-server 上（`v2-server/workflows.openapi.json`，客户端由 `npm run api:generate:workflows` 生成到 `lib/workflows/api/`，不手改）。`data/contracts-client.ts` 把 desktop 编辑器调用的 14 个定义操作接到生成的客户端上：服务端的毫秒数转成 DTO 的 `bigint`，拒绝转成编辑器按 `errors.<code>` 本地化的 `ContractError`，所以编辑器本身不用改。页面按当前用户的团队建客户端和查询缓存，换团队时整页重建。发布要服务端能检查图；服务端还不能检查时，发布返回的错误照常提示，不当作成功。

以前的版本把工作流存在浏览器 localStorage 里。`data/legacy-library.ts` 只读这份旧数据：浏览器里还有旧工作流时，左栏顶部提示一次，点"导入"按名字逐个建到服务端（只带草稿，旧的发布版本要重新发布；同名的跳过），点"不导入"就不再提示；两种情况都不删浏览器里的旧数据。`data/local-workflow-backend.ts` 照搬 desktop Rust 实现的规则（名称去空格后不能为空、不能重名；一个可编辑的 `draft` 加不可变的发布版本；不能删除草稿和当前生效的版本等），现在用来读取旧数据，也是单元测试的假服务端（`lib/workflows/test/workflow-server.ts`）。

Agent 和模型清单读取与 Chat 相同的 `/chat/executors`，保存后端返回的 `agentId` / `modelId`，目录为空或读取失败时不提供演示选项。旧占位配置保留并提示重新选择。角色目录读取工作区服务端目录（与设置页同一份数据）；Skill 清单合并当前用户的服务端技能库与租户各空间已确认安装的 Skill 插件，插件标识保留 `namespace/identifier`，运行时检查目标项目的安装回执。MCP 候选读取租户真实安装目录，展示配置是否完整。卸载后已有绑定保留并提示不可用。每个节点最多启用 16 个 MCP，配置使用团队共享设置。安装和选择可以保存，非空执行须待 Node 与服务端完整生命周期接入；当前明确报错，不会忽略已启用的 MCP 后继续运行。

Agent 检查器在「必需 Skill」下方提示“运行时读取发起人同名的个人 Skill，其内容在节点会话中对团队可见”（工作流创建/启动对话框、任务处理/拆分/评论 `@` 工作流和定时任务表单在所选工作流启用了个人 Skill 时同步提示），并在交互模式旁标「暂不生效」和一句原因（`editor/workflow-unapplied-setting-hint.tsx`）。标哪几项只看 v2-server analyze 返回的 `unappliedSettings`，它和运行结果里的 `payload.unapplied` 出自同一段服务端代码；前端只知道每项放在哪个字段旁、原因怎么说，不自己判断。角色、Skill、MCP 与重试已经应用，新分析不再列出它们；历史运行 payload 里保存的 `role`、`skill`、`retry` 标记仍按原样显示。服务端列出某项时提示出现，不再列出时自动消失；列出前端还没有对应字段的项时，在 Agent 设置末尾统一说明。浏览器里的旧工作流库（`local-workflow-backend.ts`）不执行运行，这两项总是空的。工作流页本身不运行工作流，运行在对话页里（见"运行工作流"）。

### Unapplied settings and start warnings (English)

The Agent inspector places a “Not applied yet” hint and a short reason beside the interactive
field only when server analysis lists that setting in `unappliedSettings`.
The server derives this list from the same decision as the `Node.Agent().Unapplied` used in
attempt payloads, with the node's Iteration or Loop, including spare Agents. Roles, Skills, MCP and
retry now apply, so new analyses list none of them; the markers persisted in older run
payloads still display as recorded. Removing a setting from server policy removes its hint
without a frontend change. Settings without an inspector field appear together at the bottom. The
legacy local workflow library runs nothing and reports empty policy lists.

The run inspector's "Not applied in v2" line shows server data only, in the server's order; the
page derives nothing from the node configuration. If the attempt recorded `payload.unapplied`
(v2-server writes it on success), that list is shown, even when empty. Without a record (not run
yet, running, failed, cancelled, interrupted, or an old run without one) the line shows the node's
`unappliedSettings` from v2-server's analysis of the run's frozen graph. The run pane analyzes the
frozen graph once for every run it shows (cached per run, never refetched); the start dialog's
preflight reads the same analysis.

Before a pending run starts, the dialog analyzes its stored, frozen graph and names the reachable
`prompt` and `tool` nodes in `unsupportedNodes`; spare nodes are omitted. Desktop does not execute
these kinds either. Submission waits for analysis; if analysis fails, the dialog explains that
start will validate on the server. An analysis that cannot be requested (for example without a
tenant) counts as none, so the dialog never stays in “checking”. Warnings remain informational and the server still rejects
unsupported runs. This follows desktop `crates/application/src/workflow_run/engine/node_type.rs:56`
at `ebd5b725a8a21d72ecf5e6fb7ced10d007b72dfe`.

## 设置

主导航的「设置」包含成员、角色、模型连接、主题、Plugins、Skills 六个独立子页面，默认进入成员页。成员页读取真实的 `GET /api/v1/tenants/{tid}/members`，owner 还能填邮箱邀请（`POST /api/v2/tenants/{tid}/members`，契约之外，见 v2-server README）；主题真实作用于整个 V2；Plugins 使用真实市场目录与空间选择；Skills 使用按登录用户隔离的服务端技能库，支持新建和导入 `SKILL.md`，并会在新 Agent 会话中物化；角色使用工作区共享的服务端目录（`v2-server/roles.openapi.json`），可新建、编辑、删除和导入 Markdown，工作流 Agent 节点按名称选择它，运行时读取内容并注入提示词。模型连接状态只保存在当前浏览器，部署版因此隐藏模型连接页（角色与工作流的角色选择照常提供，见 settings README）；MCP 使用服务端租户安装和配置；市场 Agent、Hook 只浏览，演示安装只在 `VITE_DEMO_MOCK=1` 时由 `lib/demo/plugin-installs.ts` 记录到浏览器本地。模型连接示例不发起真实模型调用；详情见 [`src/features/settings/README.md`](src/features/settings/README.md)。

端到端测试可通过 `E2E_WEB_PORT`、`E2E_SERVER_PORT` 指定独立端口，默认仍为 5174、8091。与其他应用共用机器时选择空闲端口；测试不会复用已启动的服务。

## 定时任务

`src/features/schedules/` 通过列表和右侧抽屉管理服务端持久配置。直接对话从共享目录选择真实模型、项目和执行器，保存时由后端校验绑定；无项目对话使用模型，有项目对话使用所选项目和执行器。页面不提供示例项目、内存演示配置或模拟运行。

页面已接入立即运行、取消此次运行及历史分页和结果读取。手动运行使用稳定请求键，重试不创建替代执行；取消未确认时保留原状态。历史按时间倒序展示，完整答案和部分结果分别标识，配置暂停不取消已经开始的执行。

处理协作任务时可选择本人负责的任务，沿用项目与协作执行器。计划默认仅一次，过程和结果保存在协作任务里；从历史可直达该次运行。已保存的旧引用只读保留，不从工作流编辑器的演示库生成参数或执行请求。职责边界、候选契约和验证范围见 [定时任务模块说明](src/features/schedules/README.md)。

工作流定时任务可选择固定版本（兼容旧配置）或“跟随最新发布版本”。后者在每次触发创建运行时解析发布快照，历史不随再发布变化。工作流发布弹窗检查当前团队中本人可见的固定版本计划，列出会继续使用旧版本的任务；不会自动修改它们。固定版本任务保留原版本的后果在读取期间也始终显示，读取失败时明确提示。

## 登录

和正式 Cloud 一样，以 `GET /api/v1/me` 判断是否登录：`401` 时在当前位置显示登录页，登录后回到原页面；`403` 表示账号已停用。登录页按 `GET /auth/providers` 显示登录方式：v2-server 只提供 `email`，表单填邮箱，`POST /auth/login` 带上邮箱后跳转到返回的地址；不在演示团队里的邮箱得到 `email_not_invited`，页面据此提示找 owner 邀请。其他 provider 仍按按钮呈现，接入正式 Gateway 时前端不用改。

当前用户的角色不在 `GET /api/v1/me` 里（契约禁止额外字段），由 `GET /api/v1/me/tenants` 的 `role` 得出，见 `app/auth/user.ts` 的 `useRole()`；邀请入口只对 owner 显示。

## 巡检动态表单

工作流目录新增动态表单和创建任务节点。表单配置固定字段、报告与数组变量以及确认人；运行页展示冻结报告、问题确认、人员/Agent/小队选择和备注，支持服务端草稿、提交记录、现有任务提示和结果链接。创建任务在后续节点执行。运行链接保留 run 参数供刷新和分享；窄屏打开运行自动收起聊天列表。

DTS 模拟通过 lib/demo/dts.ts 提供，只有 VITE_DEMO_MOCK=1 才显示。回执按真实任务 ID 稳定生成，明确标为模拟，失败重试不改变 Cloud 任务。协作页项目筛选保存在 URL 的 project 参数中。

## 任务工作流 / Task workflows

任务的「工作流」页签经 `task-workflows.openapi.json` 生成的客户端读取真实处理、拆分、巡检来源记录，
在页签内打开运行；不再使用标题匹配的 demo mock。工作流处理权限与 Agent 一致：人类负责人本人、小队成员，或 Agent 任务的任一成员；其他人看到原因。
对话框要求显式选择发布工作流，可填写需求和 Start 变量；状态由服务器推进。手机上工作区使用全宽，关闭后返回任务。

The task workflow tab uses the generated v2-only task-workflow client for handling, decomposition
and inspection sources, opening each run in place. The handling action uses the same human/squad/Agent-assigned policy as Agent runs;
others see the reason. The dialog requires an explicit published workflow choice and accepts input
and Start values. The server owns status flow-back. Phone workspaces use the available full width.
Run `npm run api:generate:task-workflows` after changing the candidate contract.
