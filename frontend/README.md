# frontend: Ora Cloud Web 前端

[中文](README.md) | [English](README.en.md)

React 19 + TypeScript + Vite 8 + Tailwind CSS 4（shadcn/ui 组件）。API 层不手写：`src/api/` 由 [orval](https://orval.dev) 从后端生成的 [`api/openapi.json`](../api/openapi.json) 编译而来，产出带类型的 [TanStack Query](https://tanstack.com/query) hooks。

## 目录

| 路径 | 说明 |
| --- | --- |
| `src/api/generated.ts` | **生成物**，每个 OpenAPI operation 一组 `useXxx` / `getXxxQueryKey` / `getXxxQueryOptions`；禁止手改 |
| `src/api/model/` | **生成物**，请求/响应/参数 TypeScript 类型；禁止手改 |
| `src/lib/api-client.ts` | 所有生成 hook 共用的 axios 实例（`AXIOS_INSTANCE`）与 mutator；鉴权头、拦截器、`baseURL` 在这里配 |
| `orval.config.ts` | 生成配置：输入 `../api/openapi.json`，`client: 'react-query'`，`clean: true` |
| `vite.config.ts` | `@` → `src` 别名；dev 代理 `/api`、`/internal`、`/healthz` 到 `http://localhost:8080` |

## 命令

```sh
npm ci                # 安装（CI 同款）
npm run dev           # Vite dev server，默认 http://localhost:5173
npm run api:generate  # 从 ../api/openapi.json 重新生成 src/api
npm run lint          # oxlint
npm run build         # tsc -b && vite build
```

仓库根目录的 Task 封装：

- `task frontend:generate`：先 `task openapi`（Go 契约 → `api/openapi.json`），再 `npm run api:generate`。改了后端接口就跑这个，并把 `api/openapi.json` 和 `frontend/src/api` 一起提交。
- `task frontend:check`：与 CI `frontend` job 相同的门禁：重新生成后 `git diff --exit-code -- frontend/src/api` 检测漂移，再 lint、build。

## 不变量

- **前后端契约只有一个来源**：`internal/contract` → `api/openapi.json` → `src/api`。任何一环有未提交的漂移，Go 测试（`TestPublishedOpenAPIIsValidAndCurrent`）或 CI `frontend` job 会失败。
- **不要在生成目录里放手写文件**：`clean: true` 会在每次生成前清空 `src/api/`。共用代码放 `src/lib/`。
- **生成物是确定性的**：同一份 `openapi.json` 多次生成得到逐字节相同的输出，这是漂移检测成立的前提。

## 本地联调

后端需要真实 PostgreSQL，按根目录 [README](../README.md) 起库并 `task run`（监听 `:8080`），再 `npm run dev`。dev 代理只在 Vite 下生效；生产部署需自行让前端与 API 同源或在 `src/lib/api-client.ts` 设置 `baseURL`。
