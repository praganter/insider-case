// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MessageCard from './MessageCard.vue'
import { useCampaign } from '../composables/useCampaign'
import { t } from '../composables/usePrefs'
import type { Msg } from '../lib'

const sms = (body: string): Msg => ({ channel: 'sms', variant: 'A', language: 'tr', subject: '', body })
const email = (subject: string): Msg => ({ channel: 'email', variant: 'A', language: 'tr', subject, body: 'x' })

const c = useCampaign()
function show(msgs: Msg[], problems: string[] = []) {
  c.view.value = { id: 1, status: 'content_ready', ab_test: false } as any
  c.msgs.value = msgs
  c.problems.value = problems
}

beforeEach(() => {
  // Typing schedules a save; answer it so nothing reaches the network.
  vi.stubGlobal('fetch', vi.fn(async () => Response.json({ messages: [], problems: null })))
})
afterEach(() => {
  c.view.value = null
  c.msgs.value = []
  c.problems.value = []
  c.saved.value = true
  vi.unstubAllGlobals()
})

describe('MessageCard', () => {
  it('renders nothing before there is a copy to show', () => {
    expect(mount(MessageCard).find('textarea').exists()).toBe(false)
  })

  it('counts characters, not bytes, against the SMS limit', () => {
    show([sms('Ş'.repeat(100))]) // 200 bytes, 100 characters
    const line = mount(MessageCard).find('.count-line')
    expect(line.text()).toContain('100 / 160')
    expect(line.classes()).not.toContain('over')
  })

  it('flags a text over the limit and by how much', () => {
    show([sms('a'.repeat(165))])
    const line = mount(MessageCard).find('.count-line')
    expect(line.classes()).toContain('over')
    expect(line.text()).toContain(t('over', { n: 5 }))
  })

  it('uses the 80 character limit for an email subject', () => {
    show([email('s'.repeat(81))])
    const line = mount(MessageCard).find('.count-line')
    expect(line.text()).toContain('81 / 80')
    expect(line.classes()).toContain('over')
  })

  it('updates the counter while typing and marks the copy as unsaved', async () => {
    show([sms('hi')])
    const w = mount(MessageCard)
    await w.find('textarea').setValue('a'.repeat(161))
    expect(w.find('.count-line').text()).toContain('161 / 160')
    expect(c.saved.value).toBe(false)
  })

  it('shows a problem next to its own message and marks the field invalid', () => {
    show([sms('a')], ['sms A tr: too long'])
    const w = mount(MessageCard)
    expect(w.find('.warn').text()).toBe('sms A tr: too long')
    expect(w.find('textarea').attributes('aria-invalid')).toBe('true')
  })

  it('blocks Approve while a problem is open and allows it once the problem is gone', async () => {
    show([sms('a')], ['sms A tr: too long'])
    const w = mount(MessageCard)
    const approve = () => w.findAll('button').find((b) => b.text() === t('approveSend'))!
    expect(approve().attributes('disabled')).toBeDefined()
    c.problems.value = []
    await w.vm.$nextTick()
    expect(approve().attributes('disabled')).toBeUndefined()
  })
})
