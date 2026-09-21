/**
 * Builds the workspace-scoped routes used by the application.
 *
 * @param slug - Workspace slug included in every route.
 * @returns Route strings and builders for workspace resources.
 */
export function workspacePaths(slug: string) {
  const base = `/${slug}`
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

/** The origin this app is served from, for previews of absolute URLs. */
export function appOrigin(): string {
  return window.location.origin
}
