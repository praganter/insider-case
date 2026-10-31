// @vitest-environment jsdom
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { useCampaign } from './composables/useCampaign'

// EventSource is missing in jsdom. This one keeps its listeners so a test can emit events by hand.
class FakeEventSource {
  static all: FakeEventSource[] = []
  closed = false
  listeners: Record<string, (e: MessageEvent) => void> = {}
  constructor(public url: string) {
    FakeEventSource.all.push(this)
  }
  addEventListener(name: string, fn: (e: MessageEvent) => void) {
    this.listeners[name] = fn
  }
  close() {
    this.closed = true
  }
  emit(name: string, data: unknown) {
    this.listeners[name](new MessageEvent(name, { data: JSON.stringify(data) }))
  }
}

const campaign = (status: string) => ({ id: 7, brief: 'b', channels: ['sms'], ab_test: false, status, contents: null })
let serverStatus = 'sending'
const fetchMock = vi.fn(async (url: string) => Response.json(url === '/api/mode' ? { mode: 'mock' } : campaign(serverStatus)))
const campaignFetches = () => fetchMock.mock.calls.filter(([u]) => u === '/api/campaigns/7').length

const c = useCampaign()
beforeAll(() => {
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.stubGlobal('fetch', fetchMock)
  c.start() // registers the hashchange listener once
})

// Opening a campaign that is already sending makes the composable follow it.
async function openSending() {
  location.hash = '7'
  await vi.waitFor(() => expect(FakeEventSource.all).toHaveLength(1))
  return FakeEventSource.all[0]
}

afterEach(() => {
  c.reset()
  FakeEventSource.all = []
  fetchMock.mockClear()
  serverStatus = 'sending'
})

describe('progress stream', () => {
  it('follows the campaign events and shows the counts', async () => {
    const es = await openSending()
    expect(es.url).toBe('/api/campaigns/7/events')
    es.emit('counts', { status: 'sending', counts: { sent: 3, pending: 7 } })
    expect(c.dash.value.counts).toEqual({ sent: 3, pending: 7 })
    expect(campaignFetches()).toBe(1) // same status: no reload
  })

  it('reloads the campaign when its status changes on its own, like a schedule firing', async () => {
    serverStatus = 'scheduled'
    const es = await openSending()
    serverStatus = 'sending'
    es.emit('counts', { status: 'sending', counts: { pending: 10 } })
    await vi.waitFor(() => expect(c.status.value).toBe('sending'))
    expect(campaignFetches()).toBe(2)
  })

  it('closes the stream and reloads on done', async () => {
    const es = await openSending()
    serverStatus = 'completed'
    es.emit('done', { status: 'completed', counts: { sent: 10 } })
    expect(es.closed).toBe(true)
    await vi.waitFor(() => expect(c.status.value).toBe('completed'))
  })

  it('closes the stream when the user leaves the campaign', async () => {
    const es = await openSending()
    c.reset()
    expect(es.closed).toBe(true)
  })
})
