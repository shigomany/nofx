import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useLanguage } from '../contexts/LanguageContext'
import type { Language } from '../i18n/translations'
import { DataPage } from './DataPage'

vi.mock('../contexts/LanguageContext', () => ({
  useLanguage: vi.fn(),
}))

describe('DataPage', () => {
  it.each([
    ['en', 'Market data', 'Open Vergex', 'Opens in a new tab.'],
    ['zh', '市场数据', '打开 Vergex', '将在新标签页中打开。'],
    ['id', 'Data pasar', 'Buka Vergex', 'Dibuka di tab baru.'],
  ] as const)(
    'offers external market data in %s without embedding Vergex',
    (language: Language, heading, linkLabel, newTabLabel) => {
      vi.mocked(useLanguage).mockReturnValue({
        language,
        setLanguage: vi.fn(),
      })

      const { container } = render(<DataPage />)

      expect(
        screen.getByRole('heading', { level: 1, name: heading })
      ).toBeVisible()
      expect(screen.getByText(newTabLabel)).toBeVisible()
      const link = screen.getByRole('link', { name: linkLabel })
      expect(link).toHaveAttribute('href', 'https://vergex.trade/trending')
      expect(link).toHaveAttribute('target', '_blank')
      expect(link).toHaveAttribute('rel', 'noopener noreferrer')
      expect(container.querySelector('iframe')).toBeNull()
    }
  )
})
