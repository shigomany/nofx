import { BarChart3, ExternalLink } from 'lucide-react'
import { Container } from '../components/common/Container'
import { useLanguage } from '../contexts/LanguageContext'

const labels = {
  en: {
    heading: 'Market data',
    description:
      'Explore market trends and analytics on Vergex, an external market data platform.',
    open: 'Open Vergex',
    newTab: 'Opens in a new tab.',
  },
  zh: {
    heading: '市场数据',
    description: '在外部市场数据平台 Vergex 查看市场趋势与分析。',
    open: '打开 Vergex',
    newTab: '将在新标签页中打开。',
  },
  id: {
    heading: 'Data pasar',
    description:
      'Jelajahi tren dan analisis pasar di Vergex, platform data pasar eksternal.',
    open: 'Buka Vergex',
    newTab: 'Dibuka di tab baru.',
  },
} as const

export function DataPage() {
  const { language } = useLanguage()
  const copy = labels[language]

  return (
    <Container
      as="main"
      maxWidthClass="max-w-3xl"
      className="flex min-h-[calc(100vh-64px)] items-center py-12"
    >
      <section className="w-full rounded-2xl border border-[rgba(26,24,19,0.14)] bg-nofx-bg-lighter p-6 text-center sm:p-10">
        <div className="mx-auto mb-6 flex h-14 w-14 items-center justify-center rounded-xl bg-nofx-gold-dim text-nofx-gold">
          <BarChart3 className="h-7 w-7" aria-hidden="true" />
        </div>
        <h1 className="text-2xl font-semibold text-nofx-text sm:text-3xl">
          {copy.heading}
        </h1>
        <p className="mx-auto mt-4 max-w-lg text-sm leading-6 text-nofx-text-muted">
          {copy.description}
        </p>
        <a
          href="https://vergex.trade/trending"
          target="_blank"
          rel="noopener noreferrer"
          className="mt-8 inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-nofx-gold px-6 py-3 text-sm font-semibold text-white transition-colors hover:bg-nofx-gold-highlight focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-nofx-gold"
        >
          {copy.open}
          <ExternalLink className="h-4 w-4" aria-hidden="true" />
        </a>
        <p className="mt-3 text-xs text-nofx-text-muted">{copy.newTab}</p>
      </section>
    </Container>
  )
}
