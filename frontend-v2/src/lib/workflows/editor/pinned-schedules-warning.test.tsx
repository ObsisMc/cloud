import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { listScheduleConfigs } from '@/lib/schedules-api/generated'
import { PinnedSchedulesWarning } from './pinned-schedules-warning'
vi.mock('@/lib/schedules-api/generated',()=>({listScheduleConfigs:vi.fn()}))
vi.mock('../data/contracts-client',()=>({useContractsClient:()=>({tenantId:'tenant'})}))
beforeEach(()=>vi.resetAllMocks())
it('warns only for this workflow fixed targets, including saved targets without the new field',async()=>{
 const target={kind:'workflow',workflowId:'wf',snapshotId:'s1',input:''}
 vi.mocked(listScheduleConfigs).mockResolvedValue({items:[
  {id:'1',name:'旧固定任务',target},
  {id:'2',name:'显式固定任务',target:{...target,versionPolicy:'fixed'}},
  {id:'3',name:'自动任务',target:{kind:'workflow',workflowId:'wf',versionPolicy:'latest',input:''}},
  {id:'4',name:'其他流程',target:{...target,workflowId:'other'}},
  {id:'5',name:'对话任务',target:{kind:'chat',prompt:'hello'}},
 ] as never,nextCursor:''})
 render(<QueryClientProvider client={new QueryClient()}><PinnedSchedulesWarning workflowId="wf"/></QueryClientProvider>)
 expect(await screen.findByText(/旧固定任务、显式固定任务/)).toBeInTheDocument()
 expect(screen.queryByText(/自动任务|其他流程|对话任务/)).not.toBeInTheDocument()
 expect(listScheduleConfigs).toHaveBeenCalledWith('tenant',undefined,expect.any(AbortSignal))
})
it('makes a failed check visible instead of claiming there are no affected schedules',async()=>{
 vi.mocked(listScheduleConfigs).mockRejectedValue(new Error('unavailable'))
 render(<QueryClientProvider client={new QueryClient()}><PinnedSchedulesWarning workflowId="wf"/></QueryClientProvider>)
 expect(await screen.findByRole('alert')).toHaveTextContent('无法检查定时任务')
})

it('shows the fixed-version consequence even when publication is confirmed before the check resolves', async () => {
 let resolve!: (value: Awaited<ReturnType<typeof listScheduleConfigs>>) => void
 vi.mocked(listScheduleConfigs).mockReturnValue(new Promise(done => { resolve = done }))
 const confirm = vi.fn(() => {
  expect(screen.getByText(/发布不会更新固定版本的定时任务/)).toBeVisible()
  expect(screen.getByText('正在检查受影响的定时任务…')).toBeVisible()
 })
 render(<QueryClientProvider client={new QueryClient()}><PinnedSchedulesWarning workflowId="wf"/><button onClick={confirm}>发布</button></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button', {name:'发布'}))
 expect(confirm).toHaveBeenCalledOnce()
 await act(async () => resolve({items:[{id:'1',name:'延迟读取的固定任务',target:{kind:'workflow',workflowId:'wf',snapshotId:'s1',input:''}}] as never,nextCursor:''}))
 expect(await screen.findByText(/延迟读取的固定任务/)).toBeVisible()
 expect(screen.getByText(/发布不会更新固定版本的定时任务/)).toBeVisible()
})
