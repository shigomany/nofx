import { ArrowRight, Check } from 'lucide-react'
import type { AIModel } from '../../types'
import { t, type Language } from '../../i18n/translations'
import { getModelIcon } from '../common/ModelIcons'
import { getShortName } from './model-constants'

interface ModelCardProps {
  model: AIModel
  selected: boolean
  onClick: () => void
  configured?: boolean
  language: Language
}

export function ModelCard({
  model,
  selected,
  onClick,
  configured,
  language,
}: ModelCardProps) {
  const provider = model.provider || model.id
  const title =
    provider === 'codex'
      ? 'OpenAI Codex'
      : provider === 'zai'
        ? 'Z.ai GLM'
        : getShortName(model.name)
  const connectionLabel = t(
    provider === 'codex'
      ? 'modelConfig.subscriptionLabel'
      : provider === 'claw402'
        ? 'modelConfig.usageLabel'
        : 'modelConfig.apiKeyLabel',
    language
  )
  const description = t(
    provider === 'codex'
      ? 'modelConfig.codexCardDescription'
      : provider === 'zai'
        ? 'modelConfig.zaiCardDescription'
        : provider === 'claw402'
          ? 'modelConfig.claw402CardDescription'
          : 'modelConfig.apiCardDescription',
    language
  )

  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={title}
      className={`group flex h-full min-w-0 flex-col items-start rounded-2xl border p-5 text-left cursor-pointer transition-[background-color,border-color,box-shadow] duration-200 motion-reduce:transition-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-nofx-accent focus-visible:ring-offset-4 focus-visible:ring-offset-nofx-bg-lighter ${
        selected
          ? 'border-nofx-accent bg-nofx-gold-dim shadow-sm'
          : 'border-[rgba(26,24,19,0.12)] bg-white/70 hover:border-nofx-accent/50 hover:bg-white hover:shadow-md'
      }`}
    >
      <div className="mb-5 flex w-full items-center justify-between gap-2">
        <div className="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-2xl border border-black/[0.06] bg-white shadow-sm">
          {getModelIcon(provider, {
            width: provider === 'claw402' || provider === 'zai' ? 56 : 34,
            height: provider === 'claw402' || provider === 'zai' ? 56 : 34,
            className: 'object-contain',
          }) || (
            <span className="text-xl font-bold text-nofx-accent">
              {title[0]}
            </span>
          )}
        </div>
        <span
          className="rounded-full px-2.5 py-1 text-[10px] font-semibold leading-4"
          style={{ background: '#F1ECE2', color: '#615B50' }}
        >
          {connectionLabel}
        </span>
      </div>
      <span className="text-base font-semibold leading-6 text-nofx-text">
        {title}
      </span>
      <span
        className="mt-2 mb-6 text-xs leading-5"
        style={{
          color: '#615B50',
          fontFamily: 'Inter, system-ui, sans-serif',
        }}
      >
        {description}
      </span>
      <div className="mt-auto flex w-full items-center justify-between gap-2 border-t border-black/[0.06] pt-4">
        <span
          className={`flex items-center gap-1.5 text-[11px] font-medium ${configured || selected ? 'text-nofx-success' : 'text-nofx-text'}`}
        >
          {configured || selected ? (
            <>
              <Check className="h-3.5 w-3.5" aria-hidden="true" />
              {t(
                selected
                  ? 'modelConfig.selectedProvider'
                  : 'modelConfig.configuredProvider',
                language
              )}
            </>
          ) : (
            t('modelConfig.selectProvider', language)
          )}
        </span>
        <ArrowRight
          className="h-4 w-4 shrink-0 text-nofx-text-muted transition-colors group-hover:text-nofx-accent group-focus-visible:text-nofx-accent motion-reduce:transition-none"
          aria-hidden="true"
        />
      </div>
    </button>
  )
}
