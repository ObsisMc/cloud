# lib: React-free infrastructure

[中文](README.md) | [English](README.en.md)

## Responsibility

Utilities shared by generated code and components that contain neither React nor domain concepts. Every file here must be testable in plain Node.

## Contents

| File | Description |
| --- | --- |
| `api-client.ts` | Shared axios instance `AXIOS_INSTANCE` and the orval mutator `customInstance`. The single home for cross-cutting HTTP policy (baseURL, auth headers, interceptors); also reconciles the two cancellation sources, react-query's `AbortSignal` and orval's `cancel()`. The request interceptor attaches the devgateway-issued dual credentials (gateway service JWT plus caller-bound user JWT) when a cloud session exists and supplies `Idempotency-Key` for POST/DELETE; the response interceptor clears the session on 401. |
| `api-client.test.ts` | Verifies body unwrapping and that both cancellation paths abort the request and reject with `CanceledError`. |
| `cloud-session.ts` | Cloud credential session: stores only the dual JWTs and expiry the gateway returns (sessionStorage, isolated per tab so several accounts can verify in parallel), shape-checks storage at the boundary, and treats corrupted or token-missing entries as no session. Keys and signing live only in the devgateway process. |
| `cloud-session.test.ts` | Covers credential storage, explicit clearing, rejection of corrupted entries, and the devgateway login failure path. |
| `mock-api-client.ts` | Axios client for the MSW-mocked domain (`/mock-api/*`), kept separate from the real-backend generated client. In cloud sessions it rewrites the real space slug to the seeded demo workspace so mock pages keep showing demo data in any space until they gain real API counterparts. |
| `utils.ts` | Re-exports `cn` (Tailwind-aware class merging); shadcn components import it via `@/lib/utils`. |

## Dependency direction

Third-party libraries and sibling modules in this directory only. **Never** import `react`, `@/components` or `@/api` (`@/api` depends on this module; the reverse would be a cycle).

## Invariants

- The first parameter of `customInstance` must accept orval's `signal: AbortSignal | undefined` (an explicit `undefined` under `exactOptionalPropertyTypes`). Run `npm run typecheck` after touching the signature to confirm the generated client still compiles.
- The request interceptor must stay synchronous (`synchronous: true`); otherwise axios delays dispatch until the interceptor settles and `AbortSignal` can lose the race.
- Credentials are replayed tokens only; no file in this directory may hold or generate keys.
