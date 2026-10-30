export type Msg = { channel: 'sms' | 'email'; subject: string; body: string }
export type Counts = Record<string, number>
export type Campaign = {
  id: number
  brief: string
  channels: string[]
  status: string
  segment_rule: unknown
  contents: Msg[] | null
  message_counts: Counts
}

// Same unit as the backend's utf8.RuneCountInString: code points, not UTF-16 units.
export const runes = (s: string) => [...s].length

export const LIMITS = { sms: 160, subject: 80 }

// Splits a chunk of an SSE stream into events. A trailing partial event is
// returned in `rest` and must be prepended to the next chunk.
export function parseSSE(buf: string): { events: { event: string; data: any }[]; rest: string } {
  const parts = buf.split('\n\n')
  const rest = parts.pop() ?? ''
  const events = parts.map((p) => {
    let event = 'message'
    let data = ''
    for (const line of p.split('\n')) {
      if (line.startsWith('event: ')) event = line.slice(7)
      else if (line.startsWith('data: ')) data += line.slice(6)
    }
    return { event, data: JSON.parse(data) }
  })
  return { events, rest }
}

type Rule = {
  match?: string
  conditions?: Rule[]
  field?: string
  op?: string
  value?: unknown
  event?: string
  within_days?: number
  exists?: boolean
}
const FIELDS: Record<string, string> = { city: 'City', language: 'Language', gender: 'Gender', age: 'Age', created_at: 'Signed up' }
const OPS: Record<string, string> = {
  eq: 'is', neq: 'is not', in: 'is one of', gt: 'is greater than', gte: 'is at least', lt: 'is less than', lte: 'is at most',
}
const EVENTS: Record<string, [string, string]> = {
  cart_add: ['Added to cart', 'Did not add to cart'],
  purchase: ['Purchased', 'Did not purchase'],
  page_view: ['Viewed a page', 'Did not view a page'],
}

// Turns a segment rule into indented plain-language lines for the marketer.
export function describeRule(n: Rule, depth = 0): { depth: number; text: string }[] {
  if (n.conditions) {
    const head = { depth, text: n.match === 'any' ? 'Matches any of:' : 'Matches all of:' }
    return [head, ...n.conditions.flatMap((c) => describeRule(c, depth + 1))]
  }
  if (n.event) {
    const [yes, no] = EVENTS[n.event] ?? [n.event, 'No ' + n.event]
    const d = n.within_days ?? 0
    return [{ depth, text: `${n.exists === false ? no : yes} in the last ${d} day${d === 1 ? '' : 's'}` }]
  }
  const v = Array.isArray(n.value) ? n.value.join(', ') : String(n.value)
  return [{ depth, text: `${FIELDS[n.field ?? ''] ?? n.field} ${OPS[n.op ?? ''] ?? n.op} ${v}` }]
}

// "done" counts messages that reached a final state, failed ones included.
export function progress(counts: Counts) {
  const total = Object.values(counts).reduce((a, b) => a + b, 0)
  const done = (counts.sent ?? 0) + (counts.failed ?? 0)
  return { total, done, pct: total ? Math.round((done / total) * 100) : 0 }
}

// problems are the server's messages; retry means "the LLM was unavailable, try again".
export class ApiError extends Error {
  constructor(public problems: string[], public retry = false) {
    super(problems.join('; '))
  }
}

async function fail(res: Response): Promise<never> {
  const body = await res.json().catch(() => ({}))
  throw new ApiError(body.errors ?? [`request failed (${res.status})`], !!body.retry)
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api' + path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) return fail(res)
  return res.json()
}

export const api = {
  mode: () => call<{ mode: string }>('GET', '/mode'),
  create: (brief: string, channels: string[]) => call<Campaign>('POST', '/campaigns', { brief, channels }),
  get: (id: number) => call<Campaign>('GET', `/campaigns/${id}`),
  suggest: (brief: string, channel: string) =>
    call<{ rule: unknown; count: number }>('POST', '/audience/suggest', { brief, channel }),
  setAudience: (id: number, rule: unknown) =>
    call<{ counts: Counts }>('PUT', `/campaigns/${id}/audience`, { rule }),
  saveContent: (id: number, messages: Msg[]) =>
    call<{ messages: Msg[]; problems: string[] | null }>('PUT', `/campaigns/${id}/content`, { messages }),
  approve: (id: number) => call('POST', `/campaigns/${id}/approve`),
  send: (id: number) => call('POST', `/campaigns/${id}/send`),
}

// Content generation is a POST that streams, so EventSource (GET only) cannot be used.
export async function generateContent(
  id: number,
  onDelta: (text: string) => void,
  onReset: () => void,
): Promise<{ messages: Msg[]; problems: string[] | null }> {
  const res = await fetch(`/api/campaigns/${id}/content`, { method: 'POST' })
  if (!res.ok) return fail(res)
  const reader = res.body!.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) throw new ApiError(['the stream ended unexpectedly'], true)
    const parsed = parseSSE((buf += decoder.decode(value, { stream: true })))
    buf = parsed.rest
    for (const e of parsed.events) {
      if (e.event === 'delta') onDelta(e.data.text)
      else if (e.event === 'reset') onReset()
      else if (e.event === 'done') return e.data
      else if (e.event === 'error') throw new ApiError(e.data.errors ?? ['generation failed'], !!e.data.retry)
    }
  }
}
