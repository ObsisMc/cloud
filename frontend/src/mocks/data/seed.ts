import { faker } from '@faker-js/faker'
import type {
  Agent,
  Autopilot,
  ChatMessage,
  ChatSession,
  InboxItem,
  Invoice,
  Issue,
  IssuePriority,
  IssueStatus,
  Project,
  Runtime,
  Skill,
  Squad,
  UsagePoint,
  User,
  Workspace,
  WorkspaceMember,
} from './types'

faker.seed(42)

const AVATAR_COLORS = [
  '#f97316',
  '#f43f5e',
  '#8b5cf6',
  '#3b82f6',
  '#10b981',
  '#eab308',
  '#06b6d4',
  '#ec4899',
]

const ISSUE_STATUSES: IssueStatus[] = ['backlog', 'todo', 'in_progress', 'in_review', 'blocked', 'done']
const ISSUE_PRIORITIES: IssuePriority[] = ['none', 'low', 'medium', 'high', 'urgent']
const LABELS = ['bug', 'feature', 'design', 'infra', 'docs', 'perf', 'security']

function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/)
  return parts
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? '')
    .join('')
}

function colorFor(seed: string): string {
  let hash = 0
  for (let i = 0; i < seed.length; i++) hash = (hash * 31 + seed.charCodeAt(i)) >>> 0
  return AVATAR_COLORS[hash % AVATAR_COLORS.length]
}

export const currentUserId = 'user-you'

export const workspace: Workspace = {
  id: 'ws-1',
  slug: 'ora-demo',
  name: 'Ora Demo',
  avatarColor: colorFor('Ora Demo'),
  plan: 'pro',
}

export const otherWorkspaces: Workspace[] = [
  { id: 'ws-2', slug: 'ora-labs', name: 'Ora Labs', avatarColor: colorFor('Ora Labs'), plan: 'free' },
  { id: 'ws-3', slug: 'personal', name: 'Personal', avatarColor: colorFor('Personal'), plan: 'free' },
]

export const workspaces: Workspace[] = [workspace, ...otherWorkspaces]

export const users: User[] = [
  {
    id: currentUserId,
    type: 'user',
    name: 'Ruihao Zhang',
    email: 'ruihao053@gmail.com',
    avatarColor: colorFor('Ruihao Zhang'),
    initials: 'RZ',
    role: 'owner',
  },
  ...Array.from({ length: 7 }, () => {
    const name = faker.person.fullName()
    return {
      id: faker.string.uuid(),
      type: 'user' as const,
      name,
      email: faker.internet.email({ firstName: name.split(' ')[0] }).toLowerCase(),
      avatarColor: colorFor(name),
      initials: initialsOf(name),
      role: faker.helpers.arrayElement(['admin', 'member', 'member', 'member'] as const),
    }
  }),
]

export const members: WorkspaceMember[] = [
  ...users.map((u) => ({
    userId: u.id,
    workspaceId: workspace.id,
    role: u.role,
    status: 'active' as const,
    joinedAt: faker.date.past({ years: 1 }).toISOString(),
  })),
  ...otherWorkspaces.map((ws) => ({
    userId: currentUserId,
    workspaceId: ws.id,
    role: 'owner' as const,
    status: 'active' as const,
    joinedAt: faker.date.past({ years: 1 }).toISOString(),
  })),
]

const AGENT_ROLES = [
  { role: 'Backend Engineer', model: 'claude-sonnet-5' },
  { role: 'Frontend Engineer', model: 'claude-sonnet-5' },
  { role: 'QA Engineer', model: 'claude-haiku-4-5' },
  { role: 'Code Reviewer', model: 'claude-opus-5' },
  { role: 'Support Triage', model: 'claude-haiku-4-5' },
  { role: 'Release Manager', model: 'claude-sonnet-5' },
]

export const squads: Squad[] = [
  {
    id: 'squad-platform',
    workspaceId: workspace.id,
    name: 'Platform',
    description: 'Owns core services, infra, and the deployment pipeline.',
    color: colorFor('Platform'),
    memberIds: [],
    projectIds: [],
    createdAt: faker.date.past({ years: 1 }).toISOString(),
  },
  {
    id: 'squad-product',
    workspaceId: workspace.id,
    name: 'Product',
    description: 'Ships customer-facing features end to end.',
    color: colorFor('Product'),
    memberIds: [],
    projectIds: [],
    createdAt: faker.date.past({ years: 1 }).toISOString(),
  },
]

export const agents: Agent[] = AGENT_ROLES.map(({ role, model }, i) => {
  const name = faker.person.firstName() + ' ' + role.split(' ')[0]
  const squadId = squads[i % squads.length].id
  const id = `agent-${i + 1}`
  squads.find((s) => s.id === squadId)!.memberIds.push(id)
  return {
    id,
    type: 'agent' as const,
    workspaceId: workspace.id,
    name,
    avatarColor: colorFor(name),
    initials: initialsOf(name),
    role,
    model,
    status: faker.helpers.arrayElement(['online', 'busy', 'idle', 'offline']),
    description: `Autonomous ${role.toLowerCase()} agent handling ${faker.helpers.arrayElement(['triage', 'implementation', 'review', 'monitoring'])} for the workspace.`,
    squadId,
  }
})

export const projects: Project[] = [
  'Onboarding revamp',
  'Billing v2',
  'Mobile app parity',
  'Agent marketplace',
  'Observability overhaul',
].map((title, i) => {
  const id = `project-${i + 1}`
  const squad = squads[i % squads.length]
  squad.projectIds.push(id)
  return {
    id,
    workspaceId: workspace.id,
    title,
    description: faker.lorem.sentence({ min: 8, max: 16 }),
    icon: faker.helpers.arrayElement(['Rocket', 'Layers', 'Boxes', 'GitBranch', 'Gauge']),
    color: colorFor(title),
    status: faker.helpers.arrayElement(['planned', 'in_progress', 'in_progress', 'completed', 'paused']),
    leadId: faker.helpers.arrayElement(users).id,
    targetDate: faker.date.soon({ days: 90 }).toISOString(),
    createdAt: faker.date.past({ years: 1 }).toISOString(),
  }
})

const allActorIds = [...users.map((u) => u.id), ...agents.map((a) => a.id)]

export const issues: Issue[] = Array.from({ length: 48 }, (_, i) => {
  const createdAt = faker.date.past({ years: 1 })
  const status = faker.helpers.arrayElement(ISSUE_STATUSES)
  return {
    id: `issue-${i + 1}`,
    workspaceId: workspace.id,
    identifier: `ORA-${100 + i}`,
    title: faker.hacker.phrase().replace(/^./, (c) => c.toUpperCase()),
    description: faker.lorem.paragraphs({ min: 1, max: 3 }, '\n\n'),
    status,
    priority: faker.helpers.arrayElement(ISSUE_PRIORITIES),
    assigneeId: faker.helpers.maybe(() => faker.helpers.arrayElement(allActorIds), { probability: 0.85 }) ?? null,
    projectId: faker.helpers.maybe(() => faker.helpers.arrayElement(projects).id, { probability: 0.75 }) ?? null,
    labels: faker.helpers.arrayElements(LABELS, { min: 0, max: 3 }),
    createdAt: createdAt.toISOString(),
    updatedAt: faker.date.between({ from: createdAt, to: new Date() }).toISOString(),
    commentCount: faker.number.int({ min: 0, max: 12 }),
  }
})

export const autopilots: Autopilot[] = [
  {
    name: 'Nightly triage',
    description: 'Labels and prioritizes new issues every night at 2am.',
    trigger: 'schedule',
    schedule: '0 2 * * *',
  },
  {
    name: 'PR review bot',
    description: 'Reviews every pull request opened against main.',
    trigger: 'pull_request.opened',
    schedule: 'event-driven',
  },
  {
    name: 'Stale issue sweep',
    description: 'Closes issues with no activity for 30 days.',
    trigger: 'schedule',
    schedule: '0 6 * * 1',
  },
  {
    name: 'Customer reply drafts',
    description: 'Drafts replies for inbound support conversations.',
    trigger: 'inbox.message_received',
    schedule: 'event-driven',
  },
].map((a, i) => ({
  id: `autopilot-${i + 1}`,
  workspaceId: workspace.id,
  ...a,
  status: faker.helpers.arrayElement(['active', 'active', 'paused', 'draft']),
  lastRunAt: faker.helpers.maybe(() => faker.date.recent({ days: 5 }).toISOString(), { probability: 0.8 }) ?? null,
  successRate: faker.number.int({ min: 82, max: 100 }),
  runsCount: faker.number.int({ min: 3, max: 480 }),
}))

export const chatSessions: ChatSession[] = agents.slice(0, 5).map((agent, i) => ({
  id: `chat-${i + 1}`,
  workspaceId: workspace.id,
  title: `${agent.name}`,
  agentId: agent.id,
  updatedAt: faker.date.recent({ days: 3 }).toISOString(),
  unreadCount: faker.number.int({ min: 0, max: 4 }),
}))

export const chatMessages: ChatMessage[] = chatSessions.flatMap((session) => {
  const count = faker.number.int({ min: 4, max: 14 })
  let t = faker.date.recent({ days: 3 })
  return Array.from({ length: count }, (_, i) => {
    t = new Date(t.getTime() + faker.number.int({ min: 60_000, max: 3_600_000 }))
    const isUser = i % 2 === 0
    return {
      id: `${session.id}-msg-${i + 1}`,
      sessionId: session.id,
      authorId: isUser ? currentUserId : session.agentId,
      authorType: isUser ? ('user' as const) : ('agent' as const),
      content: faker.lorem.sentences({ min: 1, max: 3 }),
      createdAt: t.toISOString(),
    }
  })
})

export const inboxItems: InboxItem[] = Array.from({ length: 20 }, (_, i) => {
  const type = faker.helpers.arrayElement(['mention', 'assignment', 'comment', 'invite', 'run_complete'] as const)
  const issue = faker.helpers.maybe(() => faker.helpers.arrayElement(issues), { probability: 0.7 })
  return {
    id: `inbox-${i + 1}`,
    workspaceId: workspace.id,
    type,
    title:
      type === 'mention'
        ? `You were mentioned in ${issue?.identifier ?? 'a discussion'}`
        : type === 'assignment'
          ? `Assigned to ${issue?.identifier ?? 'an issue'}`
          : type === 'comment'
            ? `New comment on ${issue?.identifier ?? 'an issue'}`
            : type === 'invite'
              ? 'Workspace invitation'
              : 'Autopilot run completed',
    body: faker.lorem.sentence(),
    actorId: faker.helpers.arrayElement(allActorIds),
    issueId: issue?.id ?? null,
    read: faker.datatype.boolean({ probability: 0.4 }),
    createdAt: faker.date.recent({ days: 10 }).toISOString(),
  }
})

const SKILL_CATEGORIES = ['Research', 'Code', 'Communication', 'Data', 'Ops']
export const skills: Skill[] = [
  'Web search',
  'Code review',
  'SQL query builder',
  'Slack notifier',
  'PDF summarizer',
  'Test generator',
  'Changelog writer',
  'Incident triage',
].map((name, i) => ({
  id: `skill-${i + 1}`,
  workspaceId: workspace.id,
  name,
  description: faker.lorem.sentence({ min: 6, max: 12 }),
  category: SKILL_CATEGORIES[i % SKILL_CATEGORIES.length],
  enabled: faker.datatype.boolean({ probability: 0.75 }),
  usageCount: faker.number.int({ min: 0, max: 2400 }),
}))

export const runtimes: Runtime[] = Array.from({ length: 6 }, (_, i) => ({
  id: `runtime-${i + 1}`,
  workspaceId: workspace.id,
  name: `runtime-${faker.word.adjective()}-${i + 1}`,
  type: faker.helpers.arrayElement(['sandbox', 'container', 'vm']),
  status: faker.helpers.arrayElement(['running', 'running', 'stopped', 'provisioning']),
  region: faker.helpers.arrayElement(['us-east-1', 'us-west-2', 'eu-west-1', 'ap-southeast-1']),
  cpu: faker.helpers.arrayElement([1, 2, 4, 8]),
  memoryGb: faker.helpers.arrayElement([2, 4, 8, 16]),
  createdAt: faker.date.past({ years: 1 }).toISOString(),
}))

export const invoices: Invoice[] = Array.from({ length: 6 }, (_, i) => ({
  id: `invoice-${i + 1}`,
  workspaceId: workspace.id,
  date: faker.date.past({ years: 1 }).toISOString(),
  amount: faker.number.int({ min: 49, max: 899 }),
  status: i === 0 ? 'pending' : 'paid',
}))

export const usageSeries: UsagePoint[] = Array.from({ length: 30 }, (_, i) => {
  const date = new Date()
  date.setDate(date.getDate() - (29 - i))
  return {
    date: date.toISOString().slice(0, 10),
    agentMinutes: faker.number.int({ min: 40, max: 420 }),
    apiCalls: faker.number.int({ min: 200, max: 4000 }),
    storageGb: faker.number.float({ min: 1, max: 40, fractionDigits: 1 }),
  }
})

export function actorById(id: string | null): User | Agent | undefined {
  if (!id) return undefined
  return [...users, ...agents].find((a) => a.id === id)
}
