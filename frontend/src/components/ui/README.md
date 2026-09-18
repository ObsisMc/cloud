# components/ui：展示原语

[中文](README.md) | [English](README.en.md)

## 职责

由 [shadcn/ui](https://ui.shadcn.com)（`base-nova` 风格，底层为 Base UI）生成、可按需微调的无业务语义组件。它们只负责外观与可访问性：接收 props、渲染 DOM、暴露 `data-slot` 供样式定位。

## 内容

| 文件 | 说明 |
| --- | --- |
| `button.tsx` | `Button` 组件与 `buttonVariants` 类名配方（variant × size）。 |
| `button.test.tsx` | 验证默认/自定义 variant、className 合并与原生属性透传。 |

## 依赖方向

只依赖 `@/lib`（`cn`）与第三方 UI 库。**禁止** import `@/api`、页面或业务模块；也不发起数据请求。

## 约定

- 新组件通过 `npx shadcn add <name>` 加入后必须跑 `npm run format` 并补 JSDoc 与测试，才能通过门禁。
- 修改生成代码时在 JSDoc 中说明与上游的差异，方便日后重新生成时比对。
