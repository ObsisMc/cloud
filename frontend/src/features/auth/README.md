# auth：会话与登录边界

[中文](README.md) | [English](README.en.md)

## 职责

前端唯一知道"用户是谁、是否登录"的模块。它负责：

- 通过 Gateway 完成登录与登出（`GET /auth/providers` 询问有哪些登录方式，`POST /auth/login` → 跳转 provider；`POST /auth/logout`），这是 OpenAPI 之外唯一手写的 HTTP 面；
- 用 `GET /api/v1/me` 探测会话并把结果暴露为 `Session`（`loading` / `signed-out` / `unavailable` / `signed-in`）；
- 401 策略：任何请求得到 401 即结束会话，页面据此跳转登录而不是逐个查询失败；
- 路由门禁 `RequireSession` 与登录页 `LoginPage`。

不负责：租户与空间解析（`features/spaces`）、任何 token 的持有——会话是 Gateway 的 HttpOnly Cookie，本模块也读不到它。

## 文件

| 文件 | 说明 |
| --- | --- |
| `api.ts` | `fetchLoginProviders`（Gateway 的 provider 列表，过滤为 `github` / `dev`）、`startLogin`（拿 `authorizationUrl` 后 `navigateExternal`）、`logoutSession`、`fetchSessionUser`（401 → `null`，其它错误抛出） |
| `session.tsx` | `SessionProvider`（会话查询 + 订阅 `onUnauthorized`）、`useSession`、`Session` 类型、`SESSION_QUERY_KEY` |
| `require-session.tsx` | `RequireSession`：加载中不渲染；未登录跳 `loginPath(当前地址)`；后端不可达就地提示 |
| `login-page.tsx` | Gateway 提供几种登录方式就显示几个按钮："使用 GitHub 登录"，以及本地 Gateway 开启 `login.development_provider` 时的"开发者登录"（Gateway 自己的表单，输入任意身份即可登录）；`?returnTo=` 经 `safeReturnTo` 收窄；已登录直接跳转 |
| `auth.test.tsx` | 上述全部行为的测试 |

## 依赖方向

依赖：`src/api`（`getApiV1Me`）、`src/lib/api-client`（`customInstance`、`onUnauthorized`、`isUnauthorizedError`）、`src/lib/navigation`、`src/lib/paths`、TanStack Query、react-router。

可被依赖：`main.tsx`（挂载 `SessionProvider`）、`routes.tsx`、布局组件、`features/spaces`（用 `useSession` 门控查询）、任何需要显示当前用户的页面。

## 不变量

- `SessionProvider` 在应用里只挂载一次，位于 router 之外、QueryClient 之内。
- `fetchSessionUser` 只把 401 当作"未登录"；网络错误或 5xx 是 `unavailable`，`RequireSession` 不会把它误判为未登录而丢掉 `returnTo`。
- `signOut` 先调 Gateway 再清缓存：会话置空，其余查询全部移除，避免下一个登录者看到上一个人的数据。
- 登录页从不构造 provider URL，也不解析 callback；那是 Gateway 的事。它也从不自行判断开发者登录是否存在：只有 `/auth/providers` 列出 `dev` 时才显示按钮，而 Gateway 只在 loopback 开发 origin 上允许该 provider。

## 测试

`auth.test.tsx` 用 MSW 覆盖 `/auth/*` 与 `/api/v1/me`，用 `installFakeNavigation` 观察跳转目标。基线 MSW 服务器把 `/api/v1/me` 答成 401，未登录场景不需要额外 handler。
