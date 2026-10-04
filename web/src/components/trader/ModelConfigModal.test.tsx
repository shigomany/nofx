import { StrictMode } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ModelConfigModal } from './ModelConfigModal'

const apiMocks = vi.hoisted(() => ({
  connectCodex: vi.fn(),
  getCodexStatus: vi.fn(),
  getCodexModels: vi.fn(),
  testCodex: vi.fn(),
  cancelCodex: vi.fn(),
  disconnectCodex: vi.fn(),
}))

vi.mock('../../lib/api', () => ({ api: apiMocks }))

const codexModel = {
  id: 'codex',
  name: 'OpenAI Codex',
  provider: 'codex',
  enabled: false,
}

const zaiModel = {
  id: 'zai',
  name: 'Z.ai',
  provider: 'zai',
  enabled: false,
}

afterEach(() => {
  vi.clearAllMocks()
  vi.useRealTimers()
})

describe('ModelConfigModal subscription providers', () => {
  it('gates Codex save until device auth connects and selects the catalog default', async () => {
    const onSave = vi.fn()
    apiMocks.connectCodex.mockResolvedValue({
      loginId: 'login-1',
      verificationUrl: 'https://example.test/device',
      userCode: 'ABCD-EFGH',
    })
    apiMocks.getCodexStatus.mockResolvedValue({
      connected: true,
      status: 'connected',
      planType: 'Plus',
    })
    apiMocks.getCodexModels.mockResolvedValue({
      data: [
        {
          id: 'gpt-6-astra',
          supportedReasoningEfforts: [
            { reasoningEffort: 'low' },
            { reasoningEffort: 'high' },
          ],
          defaultReasoningEffort: 'low',
        },
        {
          id: 'gpt-6.1-sol',
          isDefault: true,
          supportedReasoningEfforts: [
            { reasoningEffort: 'low' },
            { reasoningEffort: 'medium' },
            { reasoningEffort: 'high', description: 'Deeper reasoning' },
            { reasoningEffort: 'ultra' },
          ],
          defaultReasoningEffort: 'medium',
        },
        {
          id: 'gpt-6-sol',
          supportedReasoningEfforts: [
            { reasoningEffort: 'none' },
            { reasoningEffort: 'low' },
          ],
          defaultReasoningEffort: 'low',
        },
      ],
    })
    apiMocks.testCodex.mockResolvedValue({ ok: true, response: 'ok' })

    render(
      <ModelConfigModal
        allModels={[codexModel]}
        configuredModels={[]}
        editingModelId={null}
        initialModelId="codex"
        onSave={onSave}
        onDelete={vi.fn()}
        onClose={vi.fn()}
        language="en"
      />
    )

    expect(screen.getByRole('button', { name: 'Save Codex' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Connect Codex' }))

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Connected (Plus)'
    )
    expect(screen.getByLabelText('Codex model')).toHaveValue('gpt-6.1-sol')
    expect(screen.getByLabelText('Reasoning effort')).toHaveValue('medium')
    expect(
      screen
        .getByLabelText('Reasoning effort')
        .querySelector('option[value="ultra"]')
    ).not.toBeInTheDocument()
    expect(
      screen.getAllByLabelText('Codex model')[0].querySelectorAll('option')[0]
    ).toHaveTextContent('gpt-6.1-sol')
    fireEvent.change(screen.getByLabelText('Reasoning effort'), {
      target: { value: 'high' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Test Connection' }))
    await waitFor(() =>
      expect(apiMocks.testCodex).toHaveBeenCalledWith('gpt-6.1-sol', 'high')
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save Codex' }))

    expect(onSave).toHaveBeenCalledWith('codex', '', '', 'gpt-6.1-sol', 'high')
  })

  it('connects successfully after StrictMode replays mount effects', async () => {
    apiMocks.connectCodex.mockResolvedValue({
      loginId: 'strict-login',
      verificationUrl: 'https://example.test/device',
      userCode: 'STRICT-1',
    })
    apiMocks.getCodexStatus.mockResolvedValue({
      connected: true,
      status: 'connected',
    })
    apiMocks.getCodexModels.mockResolvedValue({
      data: [
        {
          id: 'gpt-6.1-sol',
          supportedReasoningEfforts: [
            { reasoningEffort: 'low' },
            { reasoningEffort: 'high' },
          ],
          defaultReasoningEffort: 'low',
        },
      ],
    })

    render(
      <StrictMode>
        <ModelConfigModal
          allModels={[codexModel]}
          configuredModels={[]}
          editingModelId={null}
          initialModelId="codex"
          onSave={vi.fn()}
          onDelete={vi.fn()}
          onClose={vi.fn()}
          language="en"
        />
      </StrictMode>
    )

    fireEvent.click(screen.getByRole('button', { name: 'Connect Codex' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Connected')
    expect(apiMocks.cancelCodex).not.toHaveBeenCalledWith('strict-login')
  })

  it('shows the device code while polling and cancels the login on close', async () => {
    apiMocks.connectCodex.mockResolvedValue({
      loginId: 'login-2',
      verificationUrl: 'https://example.test/device',
      userCode: 'WXYZ-1234',
    })
    apiMocks.getCodexStatus.mockResolvedValue({
      connected: false,
      status: 'pending',
    })
    apiMocks.cancelCodex.mockResolvedValue(undefined)
    const onClose = vi.fn()
    const view = render(
      <ModelConfigModal
        allModels={[codexModel]}
        configuredModels={[]}
        editingModelId={null}
        initialModelId="codex"
        onSave={vi.fn()}
        onDelete={vi.fn()}
        onClose={onClose}
        language="en"
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Connect Codex' }))
    expect(await screen.findByTestId('codex-user-code')).toHaveTextContent(
      'WXYZ-1234'
    )
    view.unmount()

    await waitFor(() =>
      expect(apiMocks.cancelCodex).toHaveBeenCalledWith('login-2')
    )
  })

  it('cancels a login that starts after the modal has already unmounted', async () => {
    let resolveConnect!: (value: {
      loginId: string
      verificationUrl: string
      userCode: string
    }) => void
    apiMocks.connectCodex.mockReturnValue(
      new Promise((resolve) => {
        resolveConnect = resolve
      })
    )
    apiMocks.cancelCodex.mockResolvedValue(undefined)

    const view = render(
      <ModelConfigModal
        allModels={[codexModel]}
        configuredModels={[]}
        editingModelId={null}
        initialModelId="codex"
        onSave={vi.fn()}
        onDelete={vi.fn()}
        onClose={vi.fn()}
        language="en"
      />
    )

    fireEvent.click(screen.getByRole('button', { name: 'Connect Codex' }))
    view.unmount()
    resolveConnect({
      loginId: 'late-login',
      verificationUrl: 'https://example.test/device',
      userCode: 'LATE-CODE',
    })

    await waitFor(() =>
      expect(apiMocks.cancelCodex).toHaveBeenCalledWith('late-login')
    )
  })

  it('loads the catalog automatically when reopening a configured Codex account', async () => {
    apiMocks.getCodexStatus.mockResolvedValue({
      connected: true,
      status: 'connected',
      planType: 'Plus',
    })
    apiMocks.getCodexModels.mockResolvedValue({
      data: [
        {
          id: 'gpt-6.1-sol',
          supportedReasoningEfforts: [
            { reasoningEffort: 'low' },
            { reasoningEffort: 'high' },
          ],
          defaultReasoningEffort: 'low',
        },
      ],
    })

    render(
      <ModelConfigModal
        allModels={[codexModel]}
        configuredModels={[
          {
            ...codexModel,
            id: 'user-123_codex',
            enabled: true,
            has_api_key: true,
            customModelName: 'gpt-6.1-sol',
            reasoningEffort: 'high',
          },
        ]}
        editingModelId="user-123_codex"
        onSave={vi.fn()}
        onDelete={vi.fn()}
        onClose={vi.fn()}
        language="en"
      />
    )

    expect(await screen.findByRole('status')).toHaveTextContent('Connected')
    expect(apiMocks.connectCodex).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Codex model')).toHaveValue('gpt-6.1-sol')
    expect(screen.getByLabelText('Reasoning effort')).toHaveValue('high')
  })

  it('disconnects a saved Codex account only from the explicit button', async () => {
    apiMocks.getCodexStatus.mockResolvedValue({
      connected: true,
      status: 'connected',
    })
    apiMocks.getCodexModels.mockResolvedValue({
      data: [{ id: 'gpt-5.6-luna', displayName: 'GPT-5.6 Luna' }],
    })
    apiMocks.disconnectCodex.mockResolvedValue(undefined)

    render(
      <ModelConfigModal
        allModels={[codexModel]}
        configuredModels={[
          {
            ...codexModel,
            id: 'user-123_codex',
            enabled: true,
            has_api_key: true,
            customModelName: 'gpt-5.6-luna',
          },
        ]}
        editingModelId="user-123_codex"
        onSave={vi.fn()}
        onDelete={vi.fn()}
        onClose={vi.fn()}
        language="en"
      />
    )

    fireEvent.click(
      await screen.findByRole('button', { name: 'Disconnect Codex account' })
    )
    await waitFor(() => expect(apiMocks.disconnectCodex).toHaveBeenCalledOnce())
    expect(screen.getByRole('button', { name: 'Save Codex' })).toBeDisabled()
  })

  it('uses standard Z.ai API defaults and explains separate billing', () => {
    render(
      <ModelConfigModal
        allModels={[zaiModel]}
        configuredModels={[]}
        editingModelId={null}
        initialModelId="zai"
        onSave={vi.fn()}
        onDelete={vi.fn()}
        onClose={vi.fn()}
        language="en"
      />
    )

    expect(screen.getByRole('note')).toHaveTextContent(
      'separately billed standard API'
    )
    expect(screen.getByRole('note')).toHaveTextContent(
      'does not use Coding Plan quota'
    )
    expect(
      screen.getByDisplayValue('https://api.z.ai/api/paas/v4')
    ).toBeInTheDocument()
    expect(screen.getByDisplayValue('glm-5.3')).toBeInTheDocument()
  })
})
