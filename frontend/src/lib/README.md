# lib：与 React 无关的基础设施

[中文](README.md) | [English](README.en.md)

## 职责

被生成代码和组件共用、但本身不含 React 与业务概念的工具。这里的每个文件都应能在 Node 中独立测试。

## 内容

| 文件 | 说明 |
| --- | --- |
| `api-client.ts` | 共享 axios 实例 `AXIOS_INSTANCE` 与 orval mutator `customInstance`。跨切面 HTTP 策略（baseURL、鉴权头、拦截器）唯一的落点；同时处理 react-query 的 `AbortSignal` 与 orval 的 `cancel()` 两种取消来源。请求拦截器在存在云会话时附加 devgateway 签发的双凭证（gateway service JWT + caller 绑定的 user JWT），并为 POST/DELETE 补发 `Idempotency-Key`；响应拦截器在 401 时清除过期会话。 |
| `api-client.test.ts` | 验证响应体解包，以及两条取消路径都会中止请求并以 `CanceledError` 拒绝。 |
| `cloud-session.ts` | 云凭证会话：只存取 gateway 返回的双 JWT 与过期时间（sessionStorage，按 tab 隔离，支持多账号并行验收），在边界校验存储形状，损坏或缺失 token 视为无会话。密钥与签发逻辑只存在于 devgateway 进程。 |
| `cloud-session.test.ts` | 验证凭证存取、显式清除、损坏条目拒绝与 devgateway 登录失败路径。 |
| `mock-api-client.ts` | MSW mock 域（`/mock-api/*`）的 axios 客户端，与真实后端生成客户端分离。云会话下把真实 space slug 重写为 demo 种子 workspace，使 mock 页面在任意 Space 下继续显示演示数据，直到它们接入真实 API。 |
| `utils.ts` | 重新导出 `cn`（Tailwind 感知的类名合并），shadcn 组件通过 `@/lib/utils` 引用。 |

## 依赖方向

只依赖第三方库与本目录内的兄弟模块。**禁止** import `react`、`@/components`、`@/api`（`@/api` 反向依赖这里，否则成环）。

## 不变量

- `customInstance` 的第一个参数类型必须接受 orval 生成的 `signal: AbortSignal | undefined`（`exactOptionalPropertyTypes` 下的显式 `undefined`）。改签名前先跑 `npm run typecheck` 看生成代码是否还能编译。
- 请求拦截器必须保持同步（`synchronous: true`），否则 axios 在拦截器完成前不会把请求交给 adapter，`AbortSignal` 可能输掉竞态。
- 凭证只重放 gateway 签发的 token；本目录任何文件都不得持有或生成密钥。
