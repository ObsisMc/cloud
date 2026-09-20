# spaces：云协作空间接入层

## 职责

把生成的 orval 客户端包装为页面可用的领域 hooks，并维护"当前空间"上下文与 SSE 订阅。它负责：

- 云会话激活时，从 `GET /api/v1/me/tenants` 解析租户，再加载 `GET /spaces` 空间列表；
- 把路由的 `:workspaceSlug` 解析为真实空间（未加入的 slug 解析为空）；
- 空间成员 / 空间内项目的查询与变更（创建/改名/归档/成员 upsert），成功后失效对应查询；
- 订阅 `/spaces/:sid/events` SSE 流，事件到达时只做 query invalidation（事件不携带业务状态）。

不负责：页面 UI、路由定义、身份签发（由 devgateway 承担）、mock 数据。

## 文件

| 文件 | 说明 |
|---|---|
| `api.ts` | 领域 hooks（useSpaces / useSpaceMembers / useSpaceProjects / useCreateSpace / useUpdateSpace / useArchiveSpace / useUpdateSpaceMember） |
| `current-space.tsx` | `CurrentSpaceProvider` + `useCurrentSpace`：云/mock 双模式解析当前空间 |
| `use-space-events.ts` | `parseSSEFrames`（纯函数）+ `useSpaceEvents`（fetch 流订阅与失效） |
| `create-space-dialog.tsx` | 新建工作区对话框：slug 自动小写、创建后回调 slug 供跳转 |
| `spaces.test.tsx` | 上述行为的测试 |

## 依赖与使用

依赖：`src/api`（生成客户端）、`src/lib/cloud-session`（凭证会话）、TanStack Query。

可被依赖：页面与布局组件（`projects`、`members`、`settings`、`dashboard-layout`、`app-sidebar`）。

## 不变量

- 云查询全部以 `tenantId` 为开关（`enabled: !!tenantId`），无凭证/无租户时绝不发请求；
- 凭证只存在 `cloud-session`，本模块只读不写（登录除外，登录也在 `cloud-session`）；
- SSE 事件仅触发失效，任何状态都以 REST 重新拉取为准；
- `parseSSEFrames` 是纯函数，坏帧跳过不抛异常。

## 测试

`spaces.test.tsx` 用 MSW 动态 handler 模拟真实 API；无云会话时走 mock 回退路径（断言 `cloudMode=false` 且空间为空）。测试不依赖网络。
