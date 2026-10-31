import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, generateContent } from './lib'

const enc = new TextEncoder()

// A fetch reply whose body arrives in the given chunks, like a real SSE response.
function streamOf(chunks: string[], init: ResponseInit = {}) {
  const body = new ReadableStream({
    start(c) {
      chunks.forEach((s) => c.enqueue(enc.encode(s)))
      c.close()
    },
  })
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(body, init)))
}

const sse = (event: string, data: unknown) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`
const done = { messages: [{ channel: 'sms', variant: 'A', language: 'tr', subject: '', body: 'hi' }], problems: null }

afterEach(() => vi.unstubAllGlobals())

describe('generateContent', () => {
  it('passes deltas through and returns the done payload, even when an event is split across chunks', async () => {
    const whole = sse('delta', { text: 'Hel' }) + sse('delta', { text: 'lo' }) + sse('done', done)
    streamOf([whole.slice(0, 20), whole.slice(20, 60), whole.slice(60)])
    const deltas: string[] = []
    const res = await generateContent(1, (d) => deltas.push(d), () => {})
    expect(deltas).toEqual(['Hel', 'lo'])
    expect(res).toEqual(done)
  })

  it('tells the caller to clear what it showed when the server restarts the text', async () => {
    streamOf([sse('delta', { text: 'a' }), sse('reset', {}), sse('delta', { text: 'b' }), sse('done', done)])
    let shown = ''
    await generateContent(1, (d) => (shown += d), () => (shown = ''))
    expect(shown).toBe('b')
  })

  it('throws the stream error with its problems and retry flag', async () => {
    streamOf([sse('error', { errors: ['the model refused'], retry: true })])
    const err = await generateContent(1, () => {}, () => {}).catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.problems).toEqual(['the model refused'])
    expect(err.retry).toBe(true)
  })

  it('throws a retryable error when the stream ends without done', async () => {
    streamOf([sse('delta', { text: 'a' })])
    const err = await generateContent(1, () => {}, () => {}).catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.retry).toBe(true)
  })

  it('reads the error body of a failed request', async () => {
    streamOf([JSON.stringify({ errors: ['campaign not found'] })], { status: 404 })
    const err = await generateContent(1, () => {}, () => {}).catch((e) => e)
    expect(err.problems).toEqual(['campaign not found'])
    expect(err.retry).toBe(false)
  })
})
