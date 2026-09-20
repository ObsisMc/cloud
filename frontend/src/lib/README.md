# lib：与 React 无关的基础设施

[中文](README.md) | [English](README.en.md)

## 职责

被生成代码和组件共用、但本身不含 React 与业务概念的工具。这里的每个文件都应能在 Node 中独立测试。

## 内容

| 文件 | 说明 |
| --- | --- |
| `api-client.ts` | 共享 axios 实例 `AXIOS_INSTANCE` 与 orval mutator `customInstance`。跨切面 HTTP 策略（baseURL、鉴权头、拦截器）唯一的落点；同时处理 react-query 的 `AbortSignal` 与 orval 的 `cancel()` 两种取消来源。 |
| `api-client.test.ts` | 验证响应体解包，以及两条取消路径都会中止请求并以 `CanceledError` 拒绝。 |
| `utils.ts` | 重新导出 `cn`（Tailwind 感知的类名合并），shadcn 组件通过 `@/lib/utils` 引用。 |

## 依赖方向

只依赖第三方库。**禁止** import `react`、`@/components`、`@/api`（`@/api` 反向依赖这里，否则成环）。

## 不变量

- `customInstance` 的第一个参数类型必须接受 orval 生成的 `signal: AbortSignal | undefined`（`exactOptionalPropertyTypes` 下的显式 `undefined`）。改签名前先跑 `npm run typecheck` 看生成代码是否还能编译。
