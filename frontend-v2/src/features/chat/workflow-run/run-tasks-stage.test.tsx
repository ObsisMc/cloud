import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { expect, it } from 'vitest'
import type { GraphWorkflowNodeState } from '@/lib/workflows/runtime'
import { RunTasksStage } from './run-tasks-stage'

const succeeded = (output: string): GraphWorkflowNodeState => ({ status: 'succeeded', output: { summary: output, detail: output } })

it('lists created and reused tasks with their own links', () => {
  const state = succeeded(JSON.stringify({ tasks: [
    { key: 'api', taskId: 'task-created', action: 'created' },
    { key: 'disk', taskId: 'task-reused', action: 'reused' },
  ] }))
  render(<MemoryRouter><RunTasksStage nodeTitle="创建或关联协作任务" state={state} /></MemoryRouter>)
  expect(screen.getByRole('region', { name: '任务处理结果' })).toBeInTheDocument()
  expect(screen.getByText('已创建任务')).toBeInTheDocument()
  expect(screen.getByText('已关联现有任务')).toBeInTheDocument()
  expect(screen.getAllByRole('link', { name: '打开任务' })).toHaveLength(2)
})

it('does not invent tasks when the form confirmed nothing', () => {
  const state = succeeded(JSON.stringify({ tasks: [] }))
  render(<MemoryRouter><RunTasksStage nodeTitle="创建或关联协作任务" state={state} /></MemoryRouter>)
  expect(screen.getByText('没有需要创建的任务')).toBeInTheDocument()
  expect(screen.queryByRole('link', { name: '打开任务' })).not.toBeInTheDocument()
})
