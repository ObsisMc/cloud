import type { Agent } from '@/mocks/data/types'

export const AGENT_STATUS_LABELS: Record<Agent['status'], string> = {
  online: '在线',
  busy: '忙碌',
  idle: '空闲',
  offline: '离线',
}
