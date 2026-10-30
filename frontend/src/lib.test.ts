import { describe, expect, it } from 'vitest'
import { describeRule, groupMessages, initialLang, initialTheme, parseSSE, progress, runes, splitProblems, statusBars } from './lib'
import { translator } from './i18n'
import type { Msg } from './lib'

describe('runes', () => {
  it('counts characters, not bytes or UTF-16 units', () => {
    expect(runes('İşÇğ')).toBe(4)
    expect(runes('😀')).toBe(1)
    expect(runes('a'.repeat(161))).toBe(161)
  })
})

describe('parseSSE', () => {
  it('parses complete events and keeps the partial one', () => {
    const { events, rest } = parseSSE('event: delta\ndata: {"text":"a"}\n\nevent: done\ndata: {"x":1}\n\nevent: del')
    expect(events).toEqual([
      { event: 'delta', data: { text: 'a' } },
      { event: 'done', data: { x: 1 } },
    ])
    expect(rest).toBe('event: del')
  })

  it('handles an event split across chunks', () => {
    const first = parseSSE('event: delta\ndata: {"te')
    expect(first.events).toEqual([])
    const second = parseSSE(first.rest + 'xt":"ş"}\n\n')
    expect(second.events).toEqual([{ event: 'delta', data: { text: 'ş' } }])
  })
})

describe('progress', () => {
  it('counts sent and failed as finished', () => {
    expect(progress({ pending: 5, retrying: 2, sent: 2, failed: 1 })).toEqual({ total: 10, done: 3, pct: 30 })
  })
  it('is zero for no messages', () => {
    expect(progress({})).toEqual({ total: 0, done: 0, pct: 0 })
  })
})

describe('describeRule', () => {
  it('describes fields, events and nested groups in plain language', () => {
    const rule = {
      match: 'all',
      conditions: [
        { field: 'city', op: 'eq', value: 'İstanbul' },
        { event: 'cart_add', within_days: 14, exists: true },
        { match: 'any', conditions: [{ event: 'purchase', within_days: 1, exists: false }] },
      ],
    }
    expect(describeRule(rule)).toEqual([
      { depth: 0, text: 'Matches all of:' },
      { depth: 1, text: 'City is İstanbul' },
      { depth: 1, text: 'Added to cart in the last 14 days' },
      { depth: 1, text: 'Matches any of:' },
      { depth: 2, text: 'Did not purchase in the last 1 day' },
    ])
  })
})

describe('splitProblems', () => {
  const m = (variant: string, language: string): Msg => ({ channel: 'sms', variant, language, subject: '', body: '' })
  it('gives each message its own problems and keeps campaign-wide ones apart', () => {
    const tr = m('A', 'tr')
    const en = m('B', 'en')
    const problems = ['sms A tr: body is 170 characters after personalization, max 160', 'sms: the audience is empty']
    const { own, general } = splitProblems(problems, [tr, en])
    expect(own(tr)).toEqual([problems[0]])
    expect(own(en)).toEqual([])
    expect(general).toEqual([problems[1]])
  })
})

describe('describeRule in Turkish', () => {
  it('uses the Turkish words and keeps unknown names as they are', () => {
    const rule = {
      match: 'any',
      conditions: [
        { field: 'gender', op: 'eq', value: 'male' },
        { event: 'purchase', within_days: 1, exists: false },
        { event: 'cart_add', within_days: 14, exists: true },
        { field: 'shoe_size', op: 'eq', value: 42 },
      ],
    }
    expect(describeRule(rule, 0, translator('tr')).map((l) => l.text)).toEqual([
      'Herhangi biriyle eşleşir:',
      'Cinsiyet şudur: male',
      'Satın almadı son 1 günde',
      'Sepete ekledi son 14 günde',
      'shoe_size şudur: 42',
    ])
  })
})

describe('groupMessages', () => {
  const m = (channel: 'sms' | 'email', variant: string, language: string): Msg => ({ channel, variant, language, subject: '', body: '' })
  it('groups channel, then language, then variant, in a fixed order', () => {
    const grouped = groupMessages([m('email', 'B', 'en'), m('sms', 'B', 'tr'), m('email', 'A', 'tr'), m('sms', 'A', 'tr'), m('email', 'A', 'en')])
    expect(grouped.map((g) => g.channel)).toEqual(['sms', 'email'])
    expect(grouped[1].languages.map((l) => l.language)).toEqual(['tr', 'en'])
    expect(grouped[0].languages[0].items.map((x) => x.variant)).toEqual(['A', 'B'])
    expect(grouped[1].languages[1].items.map((x) => x.variant)).toEqual(['A', 'B'])
  })
  it('keeps the same message objects, so editing a group edits the message', () => {
    const one = m('sms', 'A', 'tr')
    expect(groupMessages([one])[0].languages[0].items[0]).toBe(one)
  })
  it('is empty without messages', () => {
    expect(groupMessages([])).toEqual([])
  })
})

describe('statusBars', () => {
  it('gives every status a share of the group total, in a fixed order', () => {
    expect(statusBars({ sent: 6, failed: 2, pending: 2 })).toEqual([
      { status: 'pending', n: 2, pct: 20 },
      { status: 'retrying', n: 0, pct: 0 },
      { status: 'sent', n: 6, pct: 60 },
      { status: 'failed', n: 2, pct: 20 },
    ])
  })
  it('is all zero without messages', () => {
    expect(statusBars({}).every((b) => b.n === 0 && b.pct === 0)).toBe(true)
  })
})

describe('initialLang and initialTheme', () => {
  it('prefers the stored choice, then the browser', () => {
    expect(initialLang('en', 'tr-TR')).toBe('en')
    expect(initialLang(null, 'tr-TR')).toBe('tr')
    expect(initialLang('fr', 'de-DE')).toBe('en')
    expect(initialTheme('light', true)).toBe('light')
    expect(initialTheme(null, true)).toBe('dark')
    expect(initialTheme('blue', false)).toBe('light')
  })
})
