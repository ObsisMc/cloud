# src: application source root

[中文](README.md) | [English](README.en.md)

## Responsibility

Browser entry point and composition root. This layer only wires things together: mount React, install global providers, render the root screen. Business logic, HTTP details and styling primitives live elsewhere.

## Contents

| File | Description |
| --- | --- |
| `main.tsx` | Browser entry: validates the mount point, creates the `QueryClient`, renders `<App />`. No exports; not unit-tested (the composition is covered by `app.test.tsx`). |
| `app.tsx` | Root screen component `App`: shows backend `/healthz` reachability and is where routing/layout will attach. |
| `app.test.tsx` | Renders both the success and failure paths against a fake HTTP adapter. |
| `index.css` | Tailwind entry and design tokens (colors, radius). Global theme variables only; component styles live with components. |

## Submodules

| Directory | Description |
| --- | --- |
| `api/` | **Generated** by orval; never hand-edited, see [`../README.en.md`](../README.en.md). |
| `components/ui/` | Presentational primitives (shadcn/ui) with no business meaning. |
| `lib/` | React-free infrastructure: HTTP client, class-name merging. |
| `test/` | Test scaffolding: jsdom cleanup and the fake HTTP adapter. |

## Dependency direction

`main.tsx → app.tsx → (api, components/ui)`; `api → lib`. Lower layers never import higher ones.

## Invariants

- `main.tsx` is the only top-level module with side effects (mounting the DOM).
- All HTTP goes through `AXIOS_INSTANCE` in `lib/api-client.ts`; tests isolate the network by swapping its adapter.
