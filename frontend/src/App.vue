<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watchEffect } from 'vue'
import {
  ApiError, LIMITS, api, describeRule, generateContent, groupMessages, initialLang, initialTheme, label, progress, runes,
  splitProblems, statusBars,
} from './lib'
import type { Campaign, Counts, Msg, Theme, VariantCounts } from './lib'
import { hasKey, translator } from './i18n'
import type { Key, Lang } from './i18n'

const lang = ref<Lang>(initialLang(localStorage.getItem('lang'), navigator.language))
const theme = ref<Theme>(initialTheme(localStorage.getItem('theme'), matchMedia('(prefers-color-scheme: dark)').matches))
// Reads lang.value, so anything rendered with it follows a language switch.
const t = (key: Key, params?: Record<string, string | number>) => translator(lang.value)(key, params)
watchEffect(() => {
  document.documentElement.lang = lang.value
  document.documentElement.dataset.theme = theme.value
  localStorage.setItem('lang', lang.value)
  localStorage.setItem('theme', theme.value)
})

const mode = ref('')
const brief = ref('')
const channels = ref<string[]>(['sms'])
const abTest = ref(false)
const scheduleAt = ref('')
const view = ref<Campaign | null>(null)
const suggestion = ref<{ rule: unknown; counts: Counts } | null>(null)
const audienceCounts = ref<Counts>({})
const streamText = ref('')
const msgs = ref<Msg[]>([])
const problems = ref<string[]>([])
const saved = ref(true)
const dash = ref<{ status: string; counts: Counts; variants?: VariantCounts | null }>({ status: '', counts: {} })

const busy = ref(false)
const working = ref<Key | ''>('')
const errors = ref<string[]>([])
const canRetry = ref(false)
const errorBox = ref<HTMLElement | null>(null)
let last: { fn: () => Promise<void>; label: Key | '' } | null = null

// Every action goes through here so a failure is shown the same way, with a
// retry when trying again can help (LLM or network trouble). Actions with a
// label tell the user what is happening and move focus to the error if one occurs.
async function run(fn: () => Promise<void>, label: Key | '' = '') {
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
      errors.value = [t('unreachable')]
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
const showVariants = computed(() => Object.keys(dash.value.variants ?? {}).length > 1)
const grouped = computed(() => groupMessages(msgs.value))
const manyLanguages = computed(() => new Set(msgs.value.map((m) => m.language)).size > 1)
const split = computed(() => splitProblems(problems.value, msgs.value))
const scheduledFor = computed(() =>
  view.value?.scheduled_at ? new Date(view.value.scheduled_at).toLocaleString(lang.value, { dateStyle: 'medium', timeStyle: 'short' }) : '',
)
// datetime-local has no zone; the minimum is "now" in the user's own time.
const nowLocal = () => new Date(Date.now() - new Date().getTimezoneOffset() * 60000).toISOString().slice(0, 16)
const audienceEmpty = computed(
  () => Object.keys(audienceCounts.value).length > 0 && Object.values(audienceCounts.value).some((n) => n === 0),
)
// The API returns counts as a JSON object, whose order is not stable; show SMS first.
const CHANNELS = ['sms', 'email'] as const
const inOrder = (counts: Counts) => CHANNELS.filter((ch) => ch in counts).map((ch) => [ch, counts[ch]] as const)
const suggestionCounts = computed(() => Object.values(suggestion.value?.counts ?? {}))
const nobodyMatches = computed(() => suggestionCounts.value.every((n) => n === 0))
const someChannelEmpty = computed(() => suggestionCounts.value.some((n) => n === 0))
// One group per variant when the campaign is an A/B test, otherwise everything together.
const deliveryGroups = computed(() => {
  const v = dash.value.variants ?? {}
  return showVariants.value
    ? Object.keys(v).sort().map((k) => ({ name: t('variant', { v: k }), counts: v[k] }))
    : [{ name: t('allMessages'), counts: dash.value.counts }]
})

const STEPS: Key[] = ['stepBrief', 'stepAudience', 'stepMessage', 'stepDelivery']
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
  abTest.value = v.ab_test
  if (v.contents?.length) {
    msgs.value = v.contents
    saved.value = true
  }
  if (['scheduled', 'sending', 'completed'].includes(v.status)) watchProgress(v.id)
}

function createAndSuggest() {
  return run(async () => {
    const c = await api.create(brief.value, channels.value, abTest.value)
    view.value = c
    location.hash = String(c.id)
    await suggest()
  }, 'workCreate')
}

async function suggest() {
  suggestion.value = await api.suggest(brief.value, channels.value)
}
const suggestAgain = () => run(suggest, 'workSuggest')

function useAudience() {
  return run(async () => {
    const { counts } = await api.setAudience(view.value!.id, suggestion.value!.rule)
    audienceCounts.value = counts
    await load(view.value!.id)
  }, 'workSave')
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
    streamText.value = '' // otherwise the Message card stays open, empty, once sending starts
    await load(view.value!.id)
  }, 'workWrite')
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

function approveAndSchedule() {
  return run(async () => {
    await api.schedule(view.value!.id, new Date(scheduleAt.value))
    await load(view.value!.id)
  }, 'workSchedule')
}

function cancelSchedule() {
  return run(async () => {
    es?.close()
    es = null
    dash.value = { status: '', counts: {} }
    await api.unschedule(view.value!.id)
    await load(view.value!.id)
  }, 'workCancel')
}

function approveAndSend() {
  return run(async () => {
    await api.approve(view.value!.id)
    await api.send(view.value!.id)
    await load(view.value!.id)
  }, 'workSend')
}

let es: EventSource | null = null
function watchProgress(id: number) {
  if (es) return
  es = new EventSource(`/api/campaigns/${id}/events`)
  es.addEventListener('counts', (e) => {
    dash.value = JSON.parse((e as MessageEvent).data)
    // A scheduled campaign starts on its own; follow it into sending.
    if (dash.value.status !== view.value?.status) load(id)
  })
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
  abTest.value = false
  scheduleAt.value = ''
  location.hash = ''
}

const key = (m: Msg) => label(m).replace(/ /g, '-')
const statusName = (s: string) => (hasKey('status_' + s) ? t(('status_' + s) as Key) : s)
const languageName = (code: string) => (code === 'tr' || code === 'en' ? t(`lang_${code}`) : code)
const limitFor = (m: Msg) => (m.channel === 'sms' ? LIMITS.sms : LIMITS.subject)
const sizeFor = (m: Msg) => runes(m.channel === 'sms' ? m.body : m.subject)
const overBy = (m: Msg) => Math.max(0, sizeFor(m) - limitFor(m))
const describedBy = (m: Msg) => `count-${key(m)}` + (split.value.own(m).length ? ` problems-${key(m)}` : '')

onMounted(async () => {
  api.mode().then((m) => (mode.value = m.mode)).catch(() => {})
  const id = Number(location.hash.slice(1))
  if (id) run(() => load(id))
})
onUnmounted(() => es?.close())
</script>

<template>
  <div class="topbar">
    <small class="credit">{{ t('developedBy') }}</small>
    <div class="title">
      <h1>{{ t('appTitle') }}</h1>
      <span v-if="mode" class="badge" :class="{ mock: isMock }" :title="isMock ? t('mockTitle') : t('liveTitle')">
        {{ isMock ? 'MOCK' : 'LIVE' }}
      </span>
    </div>
    <div class="tools">
      <button class="switch" role="switch" :aria-checked="lang === 'en'" :aria-label="t('language')" @click="lang = lang === 'tr' ? 'en' : 'tr'">
        <span class="thumb" aria-hidden="true"></span>
        <span class="opt" :class="{ on: lang === 'tr' }">TR</span>
        <span class="opt" :class="{ on: lang === 'en' }">EN</span>
      </button>
      <button class="switch" role="switch" :aria-checked="theme === 'dark'" :aria-label="t('theme')" :title="t(theme === 'dark' ? 'light' : 'dark')"
        @click="theme = theme === 'dark' ? 'light' : 'dark'">
        <span class="thumb" aria-hidden="true"></span>
        <span class="opt" :class="{ on: theme === 'light' }">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">
            <circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
          </svg>
        </span>
        <span class="opt" :class="{ on: theme === 'dark' }">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
          </svg>
        </span>
      </button>
    </div>
  </div>

  <main>
    <div class="progress-row">
      <ol class="steps" :aria-label="t('progress')">
        <template v-for="(s, i) in STEPS" :key="s">
          <li v-if="i > 0" class="sep" :class="{ done: i <= stepIndex }" aria-hidden="true"></li>
          <li class="step" :class="{ done: i < stepIndex, current: i === stepIndex }" :aria-current="i === stepIndex ? 'step' : undefined">
            <span class="dot" aria-hidden="true">{{ i < stepIndex ? '✓' : i + 1 }}</span>{{ t(s) }}
          </li>
        </template>
      </ol>
      <button v-if="view" class="link" @click="reset">{{ t('newCampaign') }}</button>
    </div>

    <p v-if="busy && working" class="working" role="status">{{ t(working) }}</p>

    <div v-if="errors.length" ref="errorBox" class="error" role="alert" tabindex="-1">
      <p v-for="e in errors" :key="e">{{ e }}</p>
      <button v-if="canRetry && last" :disabled="busy" @click="retry">{{ t('tryAgain') }}</button>
    </div>

    <section>
      <h2>{{ t('brief') }}</h2>
      <label for="brief">{{ t('briefQuestion') }}</label>
      <textarea id="brief" v-model="brief" rows="3" :disabled="!!view || busy" :placeholder="t('briefPlaceholder')"></textarea>
      <div class="row">
        <fieldset>
          <legend>{{ t('channels') }}</legend>
          <label v-for="ch in (['sms', 'email'] as const)" :key="ch" class="check">
            <input type="checkbox" :value="ch" v-model="channels" :disabled="!!view || busy" /> {{ t(ch) }}
          </label>
        </fieldset>
        <label class="check">
          <input type="checkbox" v-model="abTest" :disabled="!!view || busy" /> {{ t('abTest') }}
        </label>
        <button v-if="!view" :disabled="busy || !brief.trim() || !channels.length" @click="createAndSuggest">
          {{ t('suggestAudience') }}
        </button>
      </div>
    </section>

    <section v-if="view">
      <h2>{{ t('audience') }}</h2>
      <template v-if="status === 'draft'">
        <template v-if="suggestion">
          <div class="counts">
            <div v-for="[ch, n] in inOrder(suggestion.counts)" :key="ch" class="count">
              <span class="count-n">{{ n }}</span>
              <span>{{ t(ch) }}</span>
            </div>
          </div>
          <p class="hint">{{ t('audienceIntro') }}</p>
          <ul class="rule">
            <li v-for="(l, i) in describeRule(suggestion.rule as any, 0, t)" :key="i" :style="{ paddingLeft: l.depth * 20 + 'px' }">
              {{ l.text }}
            </li>
          </ul>
          <details>
            <summary>{{ t('showRaw') }}</summary>
            <pre>{{ JSON.stringify(suggestion.rule, null, 2) }}</pre>
          </details>
          <div class="row">
            <button :disabled="busy || nobodyMatches" @click="useAudience">{{ t('useAudience') }}</button>
            <button class="secondary" :disabled="busy" @click="suggestAgain">{{ t('suggestAgain') }}</button>
          </div>
          <p v-if="nobodyMatches" class="warn">{{ t('nobody') }}</p>
          <p v-else-if="someChannelEmpty" class="warn">{{ t('channelEmpty') }}</p>
        </template>
        <button v-else :disabled="busy" @click="suggestAgain">{{ t('suggestAudience') }}</button>
      </template>
      <template v-else>
        <ul class="rule">
          <li v-for="(l, i) in describeRule(view.segment_rule as any, 0, t)" :key="i" :style="{ paddingLeft: l.depth * 20 + 'px' }">
            {{ l.text }}
          </li>
        </ul>
        <p v-if="Object.keys(audienceCounts).length">
          <span v-for="[ch, n] in inOrder(audienceCounts)" :key="ch" class="chip">{{ t(ch) }}: {{ t('recipients', { n }) }}</span>
        </p>
        <p v-if="audienceEmpty" class="warn">{{ t('channelEmpty') }}</p>
      </template>
    </section>

    <section v-if="status === 'audience_ready' || status === 'content_ready' || streamText">
      <h2>{{ t('message') }}</h2>
      <button v-if="status === 'audience_ready'" :disabled="busy" @click="generate">{{ t('writeCopy') }}</button>
      <pre v-if="busy && streamText" class="stream" :aria-label="t('writing')">{{ streamText }}</pre>

      <template v-if="status === 'content_ready'">
        <p class="hint">
          <code>&#123;&#123;first_name&#125;&#125;</code>, <code>&#123;&#123;last_name&#125;&#125;</code>
          &amp; <code>&#123;&#123;city&#125;&#125;</code> {{ t('hint') }}
        </p>
        <p v-for="p in split.general" :key="p" class="warn">{{ p }}</p>
        <div v-for="g in grouped" :key="g.channel" class="channel-group">
          <h3 class="channel-title">{{ t(g.channel as 'sms' | 'email') }}</h3>
          <div v-for="l in g.languages" :key="l.language" class="lang-group">
            <h4 v-if="manyLanguages">{{ languageName(l.language) }}</h4>
            <div class="variants-grid">
              <div v-for="m in l.items" :key="key(m)" class="msg">
                <h5 v-if="view?.ab_test">{{ t('variant', { v: m.variant }) }}</h5>
                <template v-if="m.channel === 'email'">
                  <label :for="'subject-' + key(m)">{{ t('subject') }}</label>
                  <input :id="'subject-' + key(m)" v-model="m.subject" @input="edited"
                    :aria-invalid="split.own(m).length > 0" :aria-describedby="describedBy(m)" />
                </template>
                <label :for="'body-' + key(m)">{{ m.channel === 'email' ? t('emailBody') : t('smsText') }}</label>
                <textarea :id="'body-' + key(m)" v-model="m.body" rows="4" @input="edited"
                  :aria-invalid="split.own(m).length > 0" :aria-describedby="describedBy(m)"></textarea>
                <small :id="'count-' + key(m)" :class="{ over: overBy(m) > 0 }">
                  {{ m.channel === 'sms' ? t('chars') : t('subjectChars') }}: {{ sizeFor(m) }} / {{ limitFor(m) }}
                  <template v-if="overBy(m) > 0"> ({{ t('over', { n: overBy(m) }) }})</template>
                </small>
                <div :id="'problems-' + key(m)">
                  <p v-for="p in split.own(m)" :key="p" class="warn">{{ p }}</p>
                </div>
                <p v-if="saved && !split.own(m).length" class="ok">{{ t('fits') }}</p>
              </div>
            </div>
          </div>
        </div>
        <div class="row">
          <button class="secondary" :disabled="busy" @click="generate">{{ t('writeAgain') }}</button>
          <button :disabled="busy || !saved || problems.length > 0" @click="approveAndSend">{{ t('approveSend') }}</button>
        </div>
        <div class="row schedule">
          <div>
            <label for="schedule-at">{{ t('sendLater') }}</label>
            <input id="schedule-at" type="datetime-local" v-model="scheduleAt" :min="nowLocal()" />
          </div>
          <button class="secondary" :disabled="busy || !saved || problems.length > 0 || !scheduleAt" @click="approveAndSchedule">
            {{ t('approveSchedule') }}
          </button>
        </div>
        <p class="hint">{{ t('nothingSent') }}</p>
      </template>
    </section>

    <section v-if="sending || status === 'scheduled'">
      <h2>{{ t('delivery') }}</h2>
      <div v-if="status === 'scheduled'" class="row">
        <p role="status">{{ t('willSendOn', { when: scheduledFor }) }}</p>
        <button class="secondary" :disabled="busy" @click="cancelSchedule">{{ t('cancelSchedule') }}</button>
      </div>
      <template v-else>
        <p>{{ t('statusLine', { status: statusName(dash.status || status), done: prog.done, total: prog.total }) }}</p>
        <div class="bar" role="progressbar" :aria-label="t('delivery')" :aria-valuenow="prog.pct" aria-valuemin="0" aria-valuemax="100">
          <div :style="{ width: prog.pct + '%' }"></div>
        </div>
        <div class="delivery-groups">
          <div v-for="g in deliveryGroups" :key="g.name" class="delivery-group">
            <h3>{{ g.name }}</h3>
            <div v-for="b in statusBars(g.counts)" :key="b.status" class="status-row">
              <span class="status-name">{{ t(b.status) }}</span>
              <div class="track"><div class="fill" :class="b.status" :style="{ width: b.pct + '%' }"></div></div>
              <span class="status-n">{{ b.n }} <small>({{ b.pct }}%)</small></span>
            </div>
          </div>
        </div>
        <p v-if="dash.counts.retrying" class="hint">{{ t('retryingHint') }}</p>
        <p v-if="(dash.status || status) === 'completed'" class="ok" role="status">{{ t('deliveryFinished') }}</p>
      </template>
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
  color-scheme: light;
}
:root[data-theme='dark'] {
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
  color-scheme: dark;
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
  max-width: 980px;
  margin: 0 auto;
  padding: 24px 24px 64px;
}
.topbar {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 16px;
  padding: 10px 24px;
  background: var(--card);
  border-bottom: 1px solid var(--line);
}
.credit {
  font-size: 12px;
}
.title {
  display: flex;
  align-items: center;
  gap: 10px;
}
.tools {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 12px;
}
h1 {
  font-size: 20px;
  margin: 0;
  text-align: center;
}
/* A two-option switch: the thumb slides under the active option. */
.switch {
  position: relative;
  display: grid;
  grid-template-columns: 1fr 1fr;
  width: 76px;
  min-height: 32px;
  padding: 0;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--bg);
  color: var(--muted);
}
.switch .thumb {
  position: absolute;
  top: 2px;
  bottom: 2px;
  left: 2px;
  width: calc(50% - 2px);
  border-radius: 999px;
  background: var(--primary);
  transition: transform 0.2s;
}
.switch[aria-checked='true'] .thumb {
  transform: translateX(100%);
}
.switch .opt {
  position: relative;
  display: grid;
  place-items: center;
  font-size: 13px;
  font-weight: 600;
}
.switch .opt.on {
  color: #fff;
}
.progress-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
h2 {
  font-size: 18px;
  margin: 0 0 12px;
}
h3 {
  font-size: 14px;
  letter-spacing: 0.04em;
  margin: 0 0 10px;
  color: var(--text);
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
input:not([type]),
input[type='datetime-local'] {
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
  align-items: center;
  flex: 1;
  list-style: none;
  margin: 0;
  padding: 14px 24px;
  background: var(--card);
  border: 1px solid var(--line);
  border-radius: 12px;
  font-size: 14px;
  color: var(--muted);
}
.step {
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
  white-space: nowrap;
}
/* Only the dashed lines stretch, so the gaps between the steps are all equal. */
.sep {
  flex: 1;
  min-width: 24px;
  margin: 0 12px;
  border-top: 2px dashed var(--field);
}
.sep.done {
  border-color: var(--good);
}
.dot {
  flex: none;
  display: inline-grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  border: 1px solid var(--field);
  font-size: 12px;
  font-weight: 700;
}
.step.current {
  color: var(--text);
  font-weight: 700;
}
.step.current .dot {
  background: var(--primary);
  border-color: var(--primary);
  color: #fff;
}
.step.done .dot {
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
  padding: 14px 16px;
  border: 1px solid var(--line);
  border-radius: 10px;
}
.channel-group {
  margin-top: 20px;
}
.channel-title {
  font-size: 15px;
  margin: 0 0 10px;
  padding-bottom: 6px;
  border-bottom: 2px solid var(--primary);
}
.lang-group {
  margin-bottom: 14px;
}
h4 {
  font-size: 15px;
  font-weight: 700;
  margin: 0 0 8px;
}
h5 {
  font-size: 13px;
  margin: 0 0 8px;
}
.variants-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 12px;
}
.counts {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 8px;
}
.count {
  display: flex;
  flex-direction: column;
  min-width: 140px;
  padding: 10px 16px;
  border: 1px solid var(--line);
  border-radius: 10px;
}
.count-n {
  font-size: 28px;
  font-weight: 700;
  line-height: 1.2;
}
.delivery-groups {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 16px;
  margin-top: 16px;
}
.delivery-group {
  padding: 14px 16px;
  border: 1px solid var(--line);
  border-radius: 10px;
}
.status-row {
  display: grid;
  grid-template-columns: 140px 1fr 90px;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
  font-size: 14px;
}
.track {
  height: 12px;
  background: var(--bg);
  border: 1px solid var(--line);
  border-radius: 999px;
  overflow: hidden;
}
.fill {
  height: 100%;
  background: var(--field);
  transition: width 0.3s;
}
.fill.retrying {
  background: var(--warn);
}
.fill.sent {
  background: var(--good);
}
.fill.failed {
  background: var(--bad);
}
.status-n {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.status-n small {
  display: inline;
}
.row.schedule {
  align-items: flex-end;
}
.row.schedule input {
  width: auto;
  margin-bottom: 0;
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
  .fill,
  .switch .thumb,
  button {
    transition: none;
  }
}
</style>
