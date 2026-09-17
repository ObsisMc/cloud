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
    autopilots: `${base}/autopilots`,
    agents: `${base}/agents`,
    agentDetail: (id: string) => `${base}/agents/${id}`,
    squads: `${base}/squads`,
    squadDetail: (id: string) => `${base}/squads/${id}`,
    skills: `${base}/skills`,
    runtimes: `${base}/runtimes`,
    usage: `${base}/usage`,
    members: `${base}/settings/members`,
    billing: `${base}/settings/billing`,
    settings: `${base}/settings`,
  }
}
