# spaces：协作空间接入层

## 职责

把生成的 orval 客户端包装为页面可用的领域 hooks，并维护空间 SSE 订阅。它负责：

- 从 `GET /api/v1/me/tenants` 解析租户，再加载 `GET /spaces` 空间列表；
- 空间成员 / 空间内项目的查询与变更（创建/改名/归档/成员 upsert），成功后失效对应查询；
- 订阅 `/spaces/:spaceId/events` SSE 流，事件到达时只做 query invalidation（事件不携带业务状态）。

鉴权沿用 ora-web cookie 会话（前端不持有或签发任何 token）。不负责：页面 UI、路由定义、身份签发、mock 数据。

## 文件

| 文件 | 说明 |
|---|---|
| `api.ts` | 领域 hooks（useSpaces / useSpaceMembers / useSpaceProjects / useCreateSpace / useUpdateSpace / useArchiveSpace / useUpdateSpaceMember） |
| `use-space-events.ts` | `parseSSEFrames`（纯函数）+ `useSpaceEvents`（fetch 流订阅与失效） |
| `create-space-dialog.tsx` | 新建空间对话框：slug 自动小写、创建后回调 slug 供跳转 |
| `spaces-page.tsx` | 空间管理页：列出已加入空间、新建、归档，选中后订阅 SSE |
| `spaces.test.tsx` | 上述行为的测试 |

## 依赖与使用

依赖：`src/api`（生成客户端）、TanStack Query。

可被依赖：路由与布局组件（`routes.tsx`、`app-sidebar`）。

## 不变量

- 云查询全部以 `tenantId` 为开关（`enabled: !!tenantId`），无租户时绝不发请求；
- SSE 事件仅触发失效，任何状态都以 REST 重新拉取为准；
- `parseSSEFrames` 是纯函数，坏帧跳过不抛异常。

## 测试

`spaces.test.tsx` 用 MSW 动态 handler 模拟真实 API；测试不依赖网络。