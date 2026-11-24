import '@/lib/workflows/test/dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { TooltipProvider } from '@/components/ui/tooltip'
import { createContractsClient } from '@/lib/workflows/data/contracts-client'
import { ContractsClientProvider } from '@/lib/workflows/data/contracts-client-provider'
import { LocalWorkflowBackend } from '@/lib/workflows/data/local-workflow-backend'
import { createWorkflowI18n, type WorkflowLocale } from '@/lib/workflows/host/i18n'
import { createMockWorkflowCapabilities, type WorkflowNodeData } from '@/lib/workflows/mock'
import type { GraphWorkflowNodeState } from '@/lib/workflows/runtime'
import { localWorkflowContracts, memoryWorkflowStorage } from '@/lib/workflows/test/workflow-server'
import { RunActInspector } from './run-act-inspector'
import { workflowRunTranslations } from './translations'

const nodeData: WorkflowNodeData = {
  kind: 'agent',
  title: '重试任务',
  description: '',
  agentConfig: {
    ...createMockWorkflowCapabilities('zh-CN').defaultAgentConfig,
    executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' },
    prompt: 'return structured summary',
  },
}

const injectedText = '## 上一次尝试（第 1 次）失败信息\n\n失败类型：结构化输出不合格'

function renderInspector(locale: WorkflowLocale, state: GraphWorkflowNodeState) {
  const i18n = createWorkflowI18n(['zh-CN', 'en-US'], workflowRunTranslations)
  void i18n.changeLanguage(locale)
  const client = createContractsClient(
    localWorkflowContracts(new LocalWorkflowBackend({ storage: memoryWorkflowStorage() })),
    'inspector-test',
  )
  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ContractsClientProvider client={client}>
          <MemoryRouter>
            <TooltipProvider>
              <RunActInspector
                nodeId="agent"
                data={nodeData}
                state={state}
                selectedRound={null}
                onRoundChange={vi.fn()}
                artifacts={[]}
                revealedArtifactId={null}
              />
            </TooltipProvider>
          </MemoryRouter>
        </ContractsClientProvider>
      </QueryClientProvider>
    </I18nextProvider>,
  )
}

describe('RunActInspector injectedFailureContext', () => {
  it('renders a collapsible injected failure block in zh-CN with the full failure context', () => {
    renderInspector('zh-CN', {
      status: 'failed',
      errorMessage: '结构化输出不合格',
      injectedFailureContext: injectedText,
    })

    const summary = screen.getByText('本次尝试注入的上次失败信息')
    const details = summary.closest('details')
    expect(details).not.toBeNull()
    expect(details).not.toHaveAttribute('open')

    const pre = details!.querySelector('pre')
    expect(pre).not.toBeNull()
    expect(pre).toHaveTextContent('上一次尝试（第 1 次）失败信息')
    expect(pre).toHaveTextContent('失败类型：结构化输出不合格')

    fireEvent.click(summary)
    expect(details).toHaveAttribute('open')
  })

  it('localizes the injected failure summary in en-US', () => {
    renderInspector('en-US', {
      status: 'failed',
      injectedFailureContext: injectedText,
    })

    const summary = screen.getByText('Previous-failure context injected into this attempt')
    const pre = summary.closest('details')?.querySelector('pre')
    expect(pre).toHaveTextContent('失败类型：结构化输出不合格')
  })

  it.each([undefined, ''])('omits the injected failure block when injectedFailureContext is %j', (value) => {
    renderInspector('zh-CN', {
      status: 'failed',
      injectedFailureContext: value,
    })

    expect(screen.queryByText('本次尝试注入的上次失败信息')).toBeNull()
  })
})
