# src：应用源码根

[中文](README.md) | [English](README.en.md)

## 职责

浏览器入口与组合根。这里只做"把各部分接起来"：挂载 React、装配全局 Provider、渲染根页面。业务逻辑、HTTP 细节、样式原语都不放在这一层。

## 内容

| 文件 | 说明 |
| --- | --- |
| `main.tsx` | 浏览器入口：校验挂载点、创建 `QueryClient`、渲染 `<App />`。无导出，不做单元测试（组合结果由 `app.test.tsx` 覆盖）。 |
| `app.tsx` | 根页面组件 `App`：展示后端 `/healthz` 可达性，是后续路由/布局的挂载位置。 |
| `app.test.tsx` | 用假 HTTP 适配器验证成功/失败两条路径的渲染结果。 |
| `index.css` | Tailwind 入口与设计令牌（颜色、圆角）。只放全局主题变量，组件样式写在组件里。 |

## 子模块

| 目录 | 说明 |
| --- | --- |
| `api/` | **生成物**（orval），禁止手改，见 [`../README.md`](../README.md)。 |
| `components/ui/` | 无业务语义的展示原语（shadcn/ui）。 |
| `lib/` | 与 React 无关的基础设施：HTTP 客户端、类名合并。 |
| `test/` | 测试脚手架：jsdom 清理、假 HTTP 适配器。 |

## 依赖方向

`main.tsx → app.tsx → (api, components/ui)`；`api → lib`。下层永远不 import 上层。

## 不变量

- `main.tsx` 是唯一有副作用的顶层模块（挂载 DOM）。
- 所有 HTTP 只经过 `lib/api-client.ts` 的 `AXIOS_INSTANCE`，测试通过替换其 adapter 隔离网络。
