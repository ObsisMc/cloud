# scripts：仓库门禁脚本

[中文](README.md) | [English](README.en.md)

## 职责

把 `AGENTS.md` 中无法用现成 lint 表达的规则变成机器检查。全部是纯 Node ESM，无额外依赖（只用 `typescript` 做语法解析），由 `package.json` 的 `check:*` 脚本调用，CI 与 `task frontend:check` 走同一入口。

## 内容

| 文件 | 说明 |
| --- | --- |
| `module-files.mjs` | 共享的文件分类策略：什么算模块、什么算实现文件、什么是生成物。两个检查共用，避免定义漂移。 |
| `check-modules.mjs` | 模块卫生：每个含源码的目录必须有 `README.md` + `README.en.md`，含实现文件的目录必须有测试；`--base <ref>` 模式下，改了实现的模块必须同时改 README 和测试，可用 commit trailer `Docs-Unchanged:` / `Tests-Unchanged:` 加理由豁免。 |
| `check-exports-documented.mjs` | 每个导出符号必须有 JSDoc，且描述不能只是复述名字。 |

## 约定

- 脚本改动同样需要更新本 README；行为改动要在 PR 描述里给出前后输出。
- 不在这里放业务逻辑或构建步骤；构建由 Vite 负责。
