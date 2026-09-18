# lib: React-free infrastructure

[中文](README.md) | [English](README.en.md)

## Responsibility

Utilities shared by generated code and components that contain neither React nor domain concepts. Every file here must be testable in plain Node.

## Contents

| File | Description |
| --- | --- |
| `api-client.ts` | Shared axios instance `AXIOS_INSTANCE` and the orval mutator `customInstance`. The single home for cross-cutting HTTP policy (baseURL, auth headers, interceptors); also reconciles the two cancellation sources, react-query's `AbortSignal` and orval's `cancel()`. |
| `api-client.test.ts` | Verifies body unwrapping and that both cancellation paths abort the request and reject with `CanceledError`. |
| `utils.ts` | Re-exports `cn` (Tailwind-aware class merging); shadcn components import it via `@/lib/utils`. |

## Dependency direction

Third-party libraries only. **Never** imports `react`, `@/components` or `@/api` (`@/api` depends on this module; the reverse would be a cycle).

## Invariants

- The first parameter of `customInstance` must accept orval's `signal: AbortSignal | undefined` (an explicit `undefined` under `exactOptionalPropertyTypes`). Run `npm run typecheck` after touching the signature to confirm the generated client still compiles.
