import * as seed from './seed'
import type {
  Agent,
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
  Workspace,
  WorkspaceMember,
} from './types'

/**
 * Mutable in-memory copies of the seed data. MSW handlers read and write
 * through this module so create/update/delete calls persist for the life
 * of the tab without a real backend.
 */
const workspaces = seed.workspaces.map((w) => ({ ...w })) as Workspace[]

type NonEmpty<T> = [T, ...T[]]

function asNonEmpty<T>(items: T[], name: string): NonEmpty<T> {
  if (items.length === 0) {
    throw new Error(`${name} seed data must not be empty`)
  }
  return items as NonEmpty<T>
}

const defaultWorkspace = workspaces[0]
if (!defaultWorkspace) {
  throw new Error('workspace seed data must not be empty')
}

export const db = {
  /** The default workspace, used wherever a workspace isn't resolved from a route param. */
  workspace: defaultWorkspace,
  workspaces,
  users: asNonEmpty([...seed.users] as User[], 'users'),
  members: [...seed.members] as WorkspaceMember[],
  agents: asNonEmpty([...seed.agents] as Agent[], 'agents'),
  squads: asNonEmpty([...seed.squads] as Squad[], 'squads'),
  projects: asNonEmpty([...seed.projects] as Project[], 'projects'),
  issues: asNonEmpty([...seed.issues] as Issue[], 'issues'),
  chatSessions: asNonEmpty([...seed.chatSessions] as ChatSession[], 'chat sessions'),
  chatMessages: [...seed.chatMessages] as ChatMessage[],
  inboxItems: asNonEmpty([...seed.inboxItems] as InboxItem[], 'inbox items'),
  skills: asNonEmpty([...seed.skills] as Skill[], 'skills'),
  runtimes: asNonEmpty([...seed.runtimes] as Runtime[], 'runtimes'),
  invoices: asNonEmpty([...seed.invoices] as Invoice[], 'invoices'),
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
