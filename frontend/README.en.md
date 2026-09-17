# frontend: Ora Cloud Web Frontend

[中文](README.md) | [English](README.en.md)

React 19 + TypeScript + Vite 8 + Tailwind CSS 4 (shadcn/ui components). The API layer is not hand-written: `src/api/` is compiled by [orval](https://orval.dev) from the backend-generated [`api/openapi.json`](../api/openapi.json) into typed [TanStack Query](https://tanstack.com/query) hooks.

## Layout

| Path | Purpose |
| --- | --- |
| `src/api/<tag>/<tag>.ts` | **Generated.** One directory per OpenAPI tag (`me`, `projects`, `workspaces`, `internal`, …), with a `useXxx` / `getXxxQueryKey` / `getXxxQueryOptions` set per operation; never edit by hand |
| `src/api/generated.schemas.ts` | **Generated.** All request, response, and parameter TypeScript types; never edit by hand |
| `src/api/index.ts` | **Generated.** Re-exports every tag directory |
| `src/lib/api-client.ts` | The axios instance (`AXIOS_INSTANCE`) and mutator shared by every generated hook; auth headers, interceptors, and `baseURL` go here |
| `orval.config.ts` | Generator config: input `../api/openapi.json`, `client: 'react-query'`, `clean: true` |
| `vite.config.ts` | `@` → `src` alias; dev proxy for `/api`, `/internal`, `/healthz` to `http://localhost:8080` |

## Commands

```sh
npm ci                # install (same as CI)
npm run dev           # Vite dev server, http://localhost:5173 by default
npm run api:generate  # regenerate src/api from ../api/openapi.json
npm run lint          # oxlint
npm run build         # tsc -b && vite build
```

Task wrappers at the repository root:

- `task frontend:install`: `npm ci`.
- `task frontend:dev`: Vite dev server only (start the backend separately with `task run`).
- `task dev`: Go backend (:8080) and Vite (:5173) together; the single entry point for day-to-day development.
- `task frontend:generate`: runs `task openapi` (Go contract → `api/openapi.json`), then `npm run api:generate`. Run this after any backend API change and commit `api/openapi.json` together with `frontend/src/api`.
- `task frontend:check`: the same gate as the CI `frontend` job: regenerate, `git diff --exit-code -- frontend/src/api` to detect drift, then lint and build.

## Invariants

- **One source for the contract**: `internal/contract` → `api/openapi.json` → `src/api`. Uncommitted drift at any link fails either the Go test (`TestPublishedOpenAPIIsValidAndCurrent`) or the CI `frontend` job.
- **No hand-written files inside the generated directory**: `clean: true` wipes `src/api/` before every run. Shared code belongs in `src/lib/`.
- **Generation is deterministic**: the same `openapi.json` yields byte-identical output on every run, which is what makes the drift check meaningful.

## Local end-to-end

The backend needs a real PostgreSQL. Follow the root [README](../README.en.md) to start the database and `task run` (listens on `:8080`), then `npm run dev`. The dev proxy only exists under Vite; production deployments must either serve the frontend from the API origin or set `baseURL` in `src/lib/api-client.ts`.
