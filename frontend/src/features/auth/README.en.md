# auth: session and login boundary

[中文](README.md) | [English](README.en.md)

## Responsibility

The only module in the frontend that knows who the user is and whether they are signed in. It:

- completes login and logout through the gateway (`POST /auth/login` → redirect to the provider; `POST /auth/logout`), the one hand-written HTTP surface outside OpenAPI;
- probes the session with `GET /api/v1/me` and exposes it as a `Session` (`loading` / `signed-out` / `unavailable` / `signed-in`);
- owns the 401 policy: any request answered with 401 ends the session, so screens redirect to login instead of failing query by query;
- provides the route gate `RequireSession` and the `LoginPage`.

It does not resolve tenants or spaces (`features/spaces`) and never holds a token: the session is the gateway's HttpOnly cookie, which this module cannot read either.

## Files

| File | Description |
| --- | --- |
| `api.ts` | `startLogin` (fetches `authorizationUrl`, then `navigateExternal`), `logoutSession`, `fetchSessionUser` (401 → `null`, anything else throws) |
| `session.tsx` | `SessionProvider` (session query + `onUnauthorized` subscription), `useSession`, the `Session` type, `SESSION_QUERY_KEY` |
| `require-session.tsx` | `RequireSession`: renders nothing while loading; redirects a signed-out tab to `loginPath(current location)`; reports an unreachable backend in place |
| `login-page.tsx` | A single "sign in with GitHub" button; `?returnTo=` is narrowed by `safeReturnTo`; a signed-in tab is redirected straight away |
| `auth.test.tsx` | Tests for all of the above |

## Dependency direction

Depends on: `src/api` (`getApiV1Me`), `src/lib/api-client` (`customInstance`, `onUnauthorized`, `isUnauthorizedError`), `src/lib/navigation`, `src/lib/paths`, TanStack Query, react-router.

May be consumed by: `main.tsx` (mounts `SessionProvider`), `routes.tsx`, layout components, `features/spaces` (gates its queries on `useSession`) and any page that shows the current user.

## Invariants

- `SessionProvider` is mounted exactly once, outside the router and inside the QueryClient.
- `fetchSessionUser` treats only 401 as "signed out"; network errors and 5xx are `unavailable`, so `RequireSession` never mistakes them for a sign-out and never loses `returnTo` over them.
- `signOut` calls the gateway first, then clears the cache: the session becomes null and every other query is removed, so the next member never sees the previous one's data.
- The login page never builds a provider URL or parses a callback; that is the gateway's job.

## Testing

`auth.test.tsx` covers `/auth/*` and `/api/v1/me` with MSW and observes redirects through `installFakeNavigation`. The baseline MSW server answers `/api/v1/me` with 401, so signed-out scenarios need no extra handler.
