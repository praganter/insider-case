import { describe, expect, it } from 'vitest'
import { describeRule, parseSSE, progress, runes, splitProblems } from './lib'
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
