/**
 * Reserved path segment every workspace lives under. Keeping workspaces below
 * one prefix means a slug can never collide with an application route such as
 * `/login` or `/onboarding`, and the prefix itself is off-limits as a slug.
 */
export const WORKSPACE_ROUTE_PREFIX = '/w'

/** Router pattern of the workspace subtree; pages read `:workspaceSlug` from it. */
export const WORKSPACE_ROUTE_PATTERN = `${WORKSPACE_ROUTE_PREFIX}/:workspaceSlug`

/**
 * Builds the workspace-scoped routes used by the application.
 *
 * @param slug - Workspace slug included in every route.
 * @returns Route strings and builders for workspace resources.
 */
export function workspacePaths(slug: string) {
  const base = `${WORKSPACE_ROUTE_PREFIX}/${slug}`
  return {
    root: base,
    inbox: `${base}/inbox`,
    myIssues: `${base}/my-issues`,
    chat: `${base}/chat`,
    issues: `${base}/issues`,
    issueDetail: (id: string) => `${base}/issues/${id}`,
    projects: `${base}/projects`,
    projectDetail: (id: string) => `${base}/projects/${id}`,
    agents: `${base}/agents`,
    agentDetail: (id: string) => `${base}/agents/${id}`,
    squads: `${base}/squads`,
    squadDetail: (id: string) => `${base}/squads/${id}`,
    skills: `${base}/skills`,
    runtimes: `${base}/runtimes`,
    members: `${base}/settings/members`,
    billing: `${base}/settings/billing`,
    settings: `${base}/settings`,
  }
}

/**
 * Builds the login route that brings the user back to `returnTo` after the
 * provider round-trip. `returnTo` is kept as a query parameter so a reload of
 * the login page preserves the destination.
 */
export function loginPath(returnTo: string): string {
  return `/login?returnTo=${encodeURIComponent(returnTo)}`
}

/**
 * Narrows an untrusted `returnTo` (query string, storage) to a same-origin
 * path the gateway accepts: a single leading slash, never `//` or `/\\`, which
 * browsers would treat as another origin. Anything else falls back to `/`.
 */
export function safeReturnTo(candidate: string | null | undefined): string {
  if (!candidate || !candidate.startsWith('/')) return '/'
  const second = candidate.charAt(1)
  if (second === '/' || second === '\\') return '/'
  return candidate
}

/**
 * The fixed, user-visible part of every workspace URL (`host/w/`), shown in
 * front of the slug input so a member sees exactly where the workspace will
 * live without being able to edit the reserved prefix.
 */
export function workspaceUrlPrefix(): string {
  return `${window.location.host}${WORKSPACE_ROUTE_PREFIX}/`
}
