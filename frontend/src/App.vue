<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { ApiError, LIMITS, api, describeRule, generateContent, progress, runes } from './lib'
import type { Campaign, Counts, Msg } from './lib'

const mode = ref('')
const brief = ref('')
const channels = ref<string[]>(['sms'])
const view = ref<Campaign | null>(null)
const suggestion = ref<{ rule: unknown; count: number } | null>(null)
const audienceCounts = ref<Counts>({})
const streamText = ref('')
const msgs = ref<Msg[]>([])
const problems = ref<string[]>([])
const saved = ref(true)
const dash = ref<{ status: string; counts: Counts }>({ status: '', counts: {} })

const busy = ref(false)
const working = ref('')
const errors = ref<string[]>([])
const canRetry = ref(false)
const errorBox = ref<HTMLElement | null>(null)
let last: { fn: () => Promise<void>; label: string } | null = null

// Every action goes through here so a failure is shown the same way, with a
// retry when trying again can help (LLM or network trouble). Actions with a
// label tell the user what is happening and move focus to the error if one occurs.
async function run(fn: () => Promise<void>, label = '') {
  last = { fn, label }
  busy.value = true
  working.value = label
  errors.value = []
  canRetry.value = false
  try {
    await fn()
  } catch (e) {
    if (e instanceof ApiError) {
      errors.value = e.problems
      canRetry.value = e.retry
    } else {
      errors.value = ['Could not reach the server.']
      canRetry.value = true
    }
    if (label) nextTick(() => errorBox.value?.focus())
  } finally {
    busy.value = false
    working.value = ''
  }
}
const retry = () => last && run(last.fn, last.label)

const status = computed(() => view.value?.status ?? '')
const isMock = computed(() => mode.value === 'mock')
const sending = computed(() => status.value === 'sending' || status.value === 'completed')
const prog = computed(() => progress(dash.value.counts))
const audienceEmpty = computed(
  () => Object.keys(audienceCounts.value).length > 0 && Object.values(audienceCounts.value).some((n) => n === 0),
)

const STEPS = ['Brief', 'Audience', 'Message', 'Delivery']
const stepIndex = computed(() => {
  if (!view.value) return 0
  if (status.value === 'draft') return 1
  if (status.value === 'audience_ready' || status.value === 'content_ready') return 2
  return status.value === 'completed' ? 4 : 3
})

async function load(id: number) {
  const v = await api.get(id)
  view.value = v
  location.hash = String(id)
  brief.value = v.brief
  channels.value = v.channels
  if (v.contents?.length) {
    msgs.value = v.contents
    saved.value = true
  }
  if (v.status === 'sending' || v.status === 'completed') watchProgress(v.id)
}

function createAndSuggest() {
  return run(async () => {
    const c = await api.create(brief.value, channels.value)
    view.value = c
    location.hash = String(c.id)
    await suggest()
  }, 'Creating the campaign and suggesting an audience…')
}

async function suggest() {
  suggestion.value = await api.suggest(brief.value, channels.value[0])
}
const suggestAgain = () => run(suggest, 'Suggesting an audience…')

function useAudience() {
  return run(async () => {
    const { counts } = await api.setAudience(view.value!.id, suggestion.value!.rule)
    audienceCounts.value = counts
    await load(view.value!.id)
  }, 'Saving the audience…')
}

function generate() {
  return run(async () => {
    streamText.value = ''
    const res = await generateContent(
      view.value!.id,
      (d) => (streamText.value += d),
      () => (streamText.value = ''),
    )
    msgs.value = res.messages
    problems.value = res.problems ?? []
    saved.value = true
    await load(view.value!.id)
  }, 'Writing the copy…')
}

// Edits are saved shortly after typing stops; the reply says what still breaks a limit.
let timer: number | undefined
function edited() {
  saved.value = false
  clearTimeout(timer)
  timer = window.setTimeout(() => run(save), 600)
}
async function save() {
  const r = await api.saveContent(view.value!.id, msgs.value)
  problems.value = r.problems ?? []
  saved.value = true
}

function approveAndSend() {
  return run(async () => {
    await api.approve(view.value!.id)
    await api.send(view.value!.id)
    await load(view.value!.id)
  }, 'Approving and sending…')
}

let es: EventSource | null = null
function watchProgress(id: number) {
  if (es) return
  es = new EventSource(`/api/campaigns/${id}/events`)
  es.addEventListener('counts', (e) => (dash.value = JSON.parse((e as MessageEvent).data)))
  es.addEventListener('done', (e) => {
    dash.value = JSON.parse((e as MessageEvent).data)
    es?.close()
    es = null
    load(id)
  })
}

function reset() {
  es?.close()
  es = null
  view.value = null
  suggestion.value = null
  audienceCounts.value = {}
  streamText.value = ''
  msgs.value = []
  problems.value = []
  errors.value = []
  dash.value = { status: '', counts: {} }
  location.hash = ''
}

const forChannel = (ch: string) => problems.value.filter((p) => p.startsWith(ch + ':'))
const limitFor = (m: Msg) => (m.channel === 'sms' ? LIMITS.sms : LIMITS.subject)
const sizeFor = (m: Msg) => runes(m.channel === 'sms' ? m.body : m.subject)
const overBy = (m: Msg) => Math.max(0, sizeFor(m) - limitFor(m))
const describedBy = (m: Msg) => `count-${m.channel}` + (forChannel(m.channel).length ? ` problems-${m.channel}` : '')

onMounted(async () => {
  api.mode().then((m) => (mode.value = m.mode)).catch(() => {})
  const id = Number(location.hash.slice(1))
  if (id) run(() => load(id))
})
onUnmounted(() => es?.close())
</script>

<template>
  <main>
    <header>
      <h1>Smart Campaign Composer</h1>
      <span v-if="mode" class="badge" :class="{ mock: isMock }" :title="isMock ? 'No API key: rule-based mock LLM' : 'Live model'">
        {{ isMock ? 'MOCK' : 'LIVE' }}
      </span>
      <button v-if="view" class="link" @click="reset">New campaign</button>
    </header>

    <ol class="steps" aria-label="Progress">
      <li v-for="(s, i) in STEPS" :key="s" :class="{ done: i < stepIndex, current: i === stepIndex }"
        :aria-current="i === stepIndex ? 'step' : undefined">
        <span class="dot" aria-hidden="true">{{ i < stepIndex ? '✓' : i + 1 }}</span>{{ s }}
      </li>
    </ol>

    <p v-if="busy && working" class="working" role="status">{{ working }}</p>

    <div v-if="errors.length" ref="errorBox" class="error" role="alert" tabindex="-1">
      <p v-for="e in errors" :key="e">{{ e }}</p>
      <button v-if="canRetry && last" :disabled="busy" @click="retry">Try again</button>
    </div>

    <section>
      <h2>Brief</h2>
      <label for="brief">What should this campaign do, and for whom?</label>
      <textarea id="brief" v-model="brief" rows="3" :disabled="!!view || busy"
        placeholder="e.g. Send a 10% discount SMS to Istanbul users who added to cart in the last 14 days but did not buy"></textarea>
      <div class="row">
        <fieldset>
          <legend>Channels</legend>
          <label v-for="ch in ['sms', 'email']" :key="ch" class="check">
            <input type="checkbox" :value="ch" v-model="channels" :disabled="!!view || busy" /> {{ ch }}
          </label>
        </fieldset>
        <button v-if="!view" :disabled="busy || !brief.trim() || !channels.length" @click="createAndSuggest">
          Suggest audience
        </button>
      </div>
    </section>

    <section v-if="view">
      <h2>Audience</h2>
      <template v-if="status === 'draft'">
        <template v-if="suggestion">
          <p>
            <strong>{{ suggestion.count }}</strong> users match for {{ channels[0] }}. Only users who agreed to
            {{ channels[0] }} are counted.
          </p>
          <ul class="rule">
            <li v-for="(l, i) in describeRule(suggestion.rule as any)" :key="i" :style="{ paddingLeft: l.depth * 20 + 'px' }">
              {{ l.text }}
            </li>
          </ul>
          <details>
            <summary>Show the raw rule</summary>
            <pre>{{ JSON.stringify(suggestion.rule, null, 2) }}</pre>
          </details>
          <div class="row">
            <button :disabled="busy || suggestion.count === 0" @click="useAudience">Use this audience</button>
            <button class="secondary" :disabled="busy" @click="suggestAgain">Suggest again</button>
          </div>
          <p v-if="suggestion.count === 0" class="warn">Nobody matches. Start a new campaign with a different brief.</p>
        </template>
        <button v-else :disabled="busy" @click="suggestAgain">Suggest audience</button>
      </template>
      <template v-else>
        <ul class="rule">
          <li v-for="(l, i) in describeRule(view.segment_rule as any)" :key="i" :style="{ paddingLeft: l.depth * 20 + 'px' }">
            {{ l.text }}
          </li>
        </ul>
        <p v-if="Object.keys(audienceCounts).length">
          <span v-for="(n, ch) in audienceCounts" :key="ch" class="chip">{{ ch }}: {{ n }} recipients</span>
        </p>
        <p v-if="audienceEmpty" class="warn">A channel has no recipients, so this campaign cannot be approved.</p>
      </template>
    </section>

    <section v-if="status === 'audience_ready' || status === 'content_ready' || streamText">
      <h2>Message</h2>
      <button v-if="status === 'audience_ready'" :disabled="busy" @click="generate">Write the copy</button>
      <pre v-if="busy && streamText" class="stream" aria-label="Copy being written">{{ streamText }}</pre>

      <template v-if="status === 'content_ready'">
        <p class="hint">
          <code>&#123;&#123;first_name&#125;&#125;</code>, <code>&#123;&#123;last_name&#125;&#125;</code> and
          <code>&#123;&#123;city&#125;&#125;</code> are
          filled in per person. Counters show the text as written; the check under each message uses the longest names
          and cities in your audience, so nobody is cut off. Changes are saved as you type.
        </p>
        <div v-for="m in msgs" :key="m.channel" class="msg">
          <h3>{{ m.channel.toUpperCase() }}</h3>
          <template v-if="m.channel === 'email'">
            <label :for="'subject-' + m.channel">Subject</label>
            <input :id="'subject-' + m.channel" v-model="m.subject" @input="edited"
              :aria-invalid="forChannel(m.channel).length > 0" :aria-describedby="describedBy(m)" />
          </template>
          <label :for="'body-' + m.channel">{{ m.channel === 'email' ? 'Email body' : 'SMS text' }}</label>
          <textarea :id="'body-' + m.channel" v-model="m.body" rows="4" @input="edited"
            :aria-invalid="forChannel(m.channel).length > 0" :aria-describedby="describedBy(m)"></textarea>
          <small :id="'count-' + m.channel" :class="{ over: overBy(m) > 0 }">
            {{ m.channel === 'sms' ? 'Characters' : 'Subject characters' }}: {{ sizeFor(m) }} / {{ limitFor(m) }}
            <template v-if="overBy(m) > 0"> ({{ overBy(m) }} over)</template>
          </small>
          <div :id="'problems-' + m.channel">
            <p v-for="p in forChannel(m.channel)" :key="p" class="warn">{{ p }}</p>
          </div>
          <p v-if="saved && !forChannel(m.channel).length" class="ok">Fits for everyone in the audience.</p>
        </div>
        <div class="row">
          <button class="secondary" :disabled="busy" @click="generate">Write again</button>
          <button :disabled="busy || !saved || problems.length > 0" @click="approveAndSend">Approve and send</button>
        </div>
        <p class="hint">Nothing is sent until you approve.</p>
      </template>
    </section>

    <section v-if="sending">
      <h2>Delivery</h2>
      <p>
        Status: <strong>{{ dash.status || status }}</strong> · {{ prog.done }} of {{ prog.total }} finished
      </p>
      <div class="bar" role="progressbar" aria-label="Delivery progress" :aria-valuenow="prog.pct" aria-valuemin="0" aria-valuemax="100">
        <div :style="{ width: prog.pct + '%' }"></div>
      </div>
      <p>
        <span v-for="s in ['pending', 'retrying', 'sent', 'failed']" :key="s" class="chip" :class="s">
          {{ s }}: {{ dash.counts[s] ?? 0 }}
        </span>
      </p>
      <p v-if="dash.counts.retrying" class="hint">
        Retrying messages hit a temporary provider error and are tried again automatically.
      </p>
      <p v-if="(dash.status || status) === 'completed'" class="ok" role="status">Delivery finished.</p>
    </section>
  </main>
</template>

<style>
:root {
  --bg: #f8fafc;
  --card: #fff;
  --text: #1e293b;
  --muted: #475569;
  --line: #e2e8f0;
  --field: #94a3b8;
  --primary: #2563eb;
  --link: #2563eb;
  --bad: #dc2626;
  --good: #15803d;
  --warn: #c2410c;
  color-scheme: light dark;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0f172a;
    --card: #1e293b;
    --text: #f1f5f9;
    --muted: #a8b3c4;
    --line: #334155;
    --field: #64748b;
    --link: #60a5fa;
    --bad: #f87171;
    --good: #4ade80;
    --warn: #fb923c;
  }
}
* {
  box-sizing: border-box;
}
body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font: 16px/1.5 'Plus Jakarta Sans Variable', system-ui, sans-serif;
}
main {
  max-width: 760px;
  margin: 0 auto;
  padding: 24px 16px 64px;
}
header {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}
h1 {
  font-size: 22px;
  margin: 0;
  flex: 1;
}
h2 {
  font-size: 18px;
  margin: 0 0 12px;
}
h3 {
  font-size: 13px;
  letter-spacing: 0.04em;
  margin: 0 0 6px;
  color: var(--muted);
}
section {
  background: var(--card);
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 20px;
  margin-top: 16px;
}
label,
legend {
  display: block;
  font-weight: 600;
  font-size: 14px;
  margin: 0 0 4px;
}
fieldset {
  border: 0;
  margin: 0;
  padding: 0;
}
.check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  margin: 0 16px 0 0;
  font-weight: 500;
  cursor: pointer;
}
.check input {
  width: 18px;
  height: 18px;
}
textarea,
input:not([type]) {
  display: block;
  width: 100%;
  padding: 10px 12px;
  margin-bottom: 8px;
  font: inherit;
  color: inherit;
  background: var(--bg);
  border: 1px solid var(--field);
  border-radius: 8px;
}
textarea[aria-invalid='true'],
input[aria-invalid='true'] {
  border-color: var(--bad);
}
:focus-visible {
  outline: 3px solid var(--link);
  outline-offset: 2px;
}
pre {
  background: var(--bg);
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 10px;
  overflow-x: auto;
  font-size: 13px;
}
code {
  font-size: 13px;
  background: var(--bg);
  padding: 1px 5px;
  border-radius: 4px;
}
details {
  margin: 8px 0;
  color: var(--muted);
  font-size: 14px;
}
summary {
  cursor: pointer;
  min-height: 32px;
}
.rule {
  list-style: none;
  margin: 0 0 8px;
  padding: 12px 14px;
  background: var(--bg);
  border: 1px solid var(--line);
  border-radius: 8px;
}
.rule li {
  padding: 2px 0;
}
.stream {
  max-height: 160px;
  white-space: pre-wrap;
  word-break: break-all;
}
.row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
}
.row fieldset {
  flex: 1;
}
button {
  font: inherit;
  font-weight: 600;
  min-height: 44px;
  padding: 8px 18px;
  border: 1px solid var(--primary);
  border-radius: 8px;
  background: var(--primary);
  color: #fff;
  cursor: pointer;
  transition: opacity 0.15s, background-color 0.15s;
}
button:hover:not(:disabled) {
  opacity: 0.9;
}
button.secondary {
  background: transparent;
  color: var(--link);
  border-color: var(--link);
}
button.link {
  background: none;
  border: 0;
  color: var(--link);
  padding: 0 4px;
  text-decoration: underline;
}
button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.badge {
  font-size: 12px;
  font-weight: 700;
  padding: 2px 10px;
  border-radius: 999px;
  border: 1px solid var(--good);
  color: var(--good);
}
.badge.mock {
  border-color: var(--warn);
  color: var(--warn);
}
.steps {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 20px;
  list-style: none;
  margin: 20px 0 0;
  padding: 0;
  font-size: 14px;
  color: var(--muted);
}
.steps li {
  display: flex;
  align-items: center;
  gap: 8px;
}
.dot {
  display: inline-grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  border: 1px solid var(--field);
  font-size: 12px;
  font-weight: 700;
}
.steps .current {
  color: var(--text);
  font-weight: 700;
}
.steps .current .dot {
  background: var(--primary);
  border-color: var(--primary);
  color: #fff;
}
.steps .done .dot {
  border-color: var(--good);
  color: var(--good);
}
.working {
  margin: 16px 0 0;
  color: var(--muted);
}
.error {
  margin-top: 16px;
  padding: 12px 16px;
  border: 1px solid var(--bad);
  border-radius: 8px;
  color: var(--bad);
}
.error p {
  margin: 0 0 8px;
}
.error button {
  background: transparent;
  color: var(--bad);
  border-color: var(--bad);
}
.msg {
  margin-bottom: 20px;
}
small {
  display: block;
  color: var(--muted);
}
small.over {
  color: var(--bad);
  font-weight: 600;
}
.warn {
  color: var(--bad);
  margin: 4px 0 0;
}
section > .warn,
.row + .warn {
  color: var(--warn);
}
.ok {
  color: var(--good);
  margin: 4px 0 0;
}
.hint {
  color: var(--muted);
  font-size: 14px;
}
.chip {
  display: inline-block;
  margin: 0 8px 4px 0;
  padding: 2px 12px;
  border: 1px solid var(--field);
  border-radius: 999px;
  font-size: 14px;
}
.chip.sent {
  border-color: var(--good);
}
.chip.failed {
  border-color: var(--bad);
}
.chip.retrying {
  border-color: var(--warn);
}
.bar {
  height: 12px;
  background: var(--bg);
  border: 1px solid var(--line);
  border-radius: 999px;
  overflow: hidden;
}
.bar div {
  height: 100%;
  background: var(--primary);
  transition: width 0.3s;
}
@media (prefers-reduced-motion: reduce) {
  .bar div,
  button {
    transition: none;
  }
}
</style>
