import { describe, expect, it } from 'vitest'
import { messages, translator } from './i18n'

const placeholders = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort()

describe('i18n', () => {
  it('has the same keys and placeholders in both languages, none empty', () => {
    expect(Object.keys(messages.tr).sort()).toEqual(Object.keys(messages.en).sort())
    for (const [key, en] of Object.entries(messages.en)) {
      const tr = messages.tr[key as keyof typeof messages.en]
      expect(tr.trim(), key).not.toBe('')
      expect(placeholders(tr), key).toEqual(placeholders(en))
    }
  })
  it('fills placeholders and leaves unknown ones visible', () => {
    expect(translator('en')('over', { n: 5 })).toBe('5 over')
    expect(translator('tr')('recipients', { n: 12 })).toBe('12 alıcı')
    expect(translator('en')('over')).toBe('{n} over')
  })
})
