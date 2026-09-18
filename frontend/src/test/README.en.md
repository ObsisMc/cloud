# test: test scaffolding

[中文](README.md) | [English](README.en.md)

## Responsibility

Environment setup and test doubles shared by every test. The goal is that each test runs deterministically in jsdom without a network or a real backend, and that no state leaks between cases.

## Contents

| File | Description |
| --- | --- |
| `setup.ts` | vitest `setupFiles`: unmounts Testing Library trees after each case. |
| `http.ts` | `installFakeHttp(body, status)`: swaps the adapter of `AXIOS_INSTANCE`, records requests and answers with a fixed response; restored automatically when the test finishes. |
| `http.test.ts` | Verifies the fake adapter's own recording and error-status semantics, which other tests rely on. |

## Dependency direction

Depends on `@/lib/api-client` (to swap its adapter) and `vitest`. Production code **never** imports this directory.

## Conventions

- Doubles replace boundaries (the HTTP adapter), never internal modules; add a new file with its own test when another boundary needs a double.
- Coverage excludes this directory (see `vite.config.ts`).
