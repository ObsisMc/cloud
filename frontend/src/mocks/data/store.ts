import * as seed from './seed'
import type {
  Agent,
  Autopilot,
  ChatMessage,
  ChatSession,
  InboxItem,
  Invoice,
  Issue,
  Project,
  Runtime,
  Skill,
  Squad,
  User,
  UsagePoint,
  Workspace,
  WorkspaceMember,
} from './types'

/**
 * Mutable in-memory copies of the seed data. MSW handlers read and write
 * through this module so create/update/delete calls persist for the life
 * of the tab without a real backend.
 */
const workspaces = seed.workspaces.map((w) => ({ ...w })) as Workspace[]

export const db = {
  /** The default workspace, used wherever a workspace isn't resolved from a route param. */
  workspace: workspaces[0],
  workspaces,
  users: [...seed.users] as User[],
  members: [...seed.members] as WorkspaceMember[],
  agents: [...seed.agents] as Agent[],
  squads: [...seed.squads] as Squad[],
  projects: [...seed.projects] as Project[],
  issues: [...seed.issues] as Issue[],
  autopilots: [...seed.autopilots] as Autopilot[],
  chatSessions: [...seed.chatSessions] as ChatSession[],
  chatMessages: [...seed.chatMessages] as ChatMessage[],
  inboxItems: [...seed.inboxItems] as InboxItem[],
  skills: [...seed.skills] as Skill[],
  runtimes: [...seed.runtimes] as Runtime[],
  invoices: [...seed.invoices] as Invoice[],
  usageSeries: [...seed.usageSeries] as UsagePoint[],
}

export const currentUserId = seed.currentUserId

export function actorById(id: string | null | undefined) {
  if (!id) return undefined
  return [...db.users, ...db.agents].find((a) => a.id === id)
}

let nextIssueSeq = db.issues.length + 1
export function nextIssueIdentifier(): string {
  return `ORA-${100 + nextIssueSeq++}`
}

export function workspaceBySlug(slug: string | undefined): Workspace | undefined {
  return db.workspaces.find((w) => w.slug === slug)
}

let idCounter = 1
export function nextId(prefix: string): string {
  return `${prefix}-${Date.now()}-${idCounter++}`
}
