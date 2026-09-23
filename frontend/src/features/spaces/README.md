# spaces：云协作空间接入层

## 职责

把生成的 orval 客户端包装为页面可用的领域 hooks，并维护"当前空间"上下文与 SSE 订阅。它负责：

- 会话确认已登录后，从 `GET /api/v1/me/tenants` 解析租户（产品不展示租户，取最早创建的一个；后端按创建时间升序返回，选择是确定的），再加载 `GET /spaces` 空间列表（`useJoinedSpaces`）；
- 为无租户的首次登录用户提供 `useCreateTenant`（`POST /api/v1/tenants`，后端顺带建首个空间并把调用者设为 owner）；
- 把路由的 `:workspaceSlug` 解析为真实空间（未加入的 slug 解析为空）；
- 空间成员 / 空间内项目的查询与变更（创建/改名/归档/成员 upsert），成功后失效对应查询；
- 订阅 `/spaces/:sid/events` SSE 流，事件到达时只做 query invalidation（事件不携带业务状态）。

不负责：页面 UI、路由定义、会话探测与登录（`features/auth`）、mock 数据。

## 文件

| 文件 | 说明 |
|---|---|
| `api.ts` | 领域 hooks（useJoinedSpaces / useCreateTenant / useSpaces / useSpaceMembers / useSpaceProjects / useCreateSpace / useUpdateSpace / useArchiveSpace / useUpdateSpaceMember） |
| `current-space.tsx` | `CurrentSpaceProvider` + `useCurrentSpace`：把路由 slug 解析为已加入的真实空间，暴露 `isPending` / `isError` |
| `slug.ts` | 与后端一致的 slug 规则 `isValidSlug` 与从名称派生 slug 的 `slugFromName`，被创建对话框与 onboarding 共用 |
| `use-space-events.ts` | `parseSSEFrames`（纯函数）+ `reconnectDelay` + `useSpaceEvents`（Cookie 会话下的 fetch 流订阅、指数退避重连与失效） |
| `create-space-dialog.tsx` | 新建工作区对话框：slug 自动小写、创建后回调 slug 供跳转 |
| `spaces.test.tsx` | 上述行为的测试 |

## 依赖与使用

依赖：`src/api`（生成客户端）、`src/features/auth/session`（登录态门控）、TanStack Query。

可被依赖：页面与布局组件（`projects`、`members`、`settings`、`dashboard-layout`、`app-sidebar`）。

## 不变量

- 租户列表只在会话为 `signed-in` 时请求；空间及以下查询全部以 `tenantId` 为开关（`enabled: !!tenantId`）；
- `useJoinedSpaces` 对"无租户"返回 `spaces: []` 而非 `undefined`，调用方只凭 `isPending` 区分"没加入"与"还在加载"；
- 本模块不接触任何凭证；请求靠浏览器自动携带的 Gateway Cookie；
- SSE 事件仅触发失效，任何状态都以 REST 重新拉取为准；断线按 1s→30s 指数退避重连，重连成功后失效该租户下全部查询，401 则终止订阅；
- `parseSSEFrames` 是纯函数，坏帧跳过不抛异常。

## 测试

`spaces.test.tsx` 用 MSW 动态 handler 模拟真实 API；未登录基线下断言不发租户请求且保持 pending。slug 规则与重连退避是纯函数测试。测试不依赖网络。
