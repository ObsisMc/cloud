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
