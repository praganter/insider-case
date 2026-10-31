import { computed, nextTick, ref } from 'vue'
import { ApiError, api, generateContent } from '../lib'
import type { Campaign, Counts, Msg, VariantCounts } from '../lib'
import type { Key } from '../i18n'
import { t } from './usePrefs'

// Module-level state: every component that calls useCampaign() shares the one campaign on screen.
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
    closeProgress()
    dash.value = { status: '', counts: {} }
    await api.unschedule(view.value!.id)
    await load(view.value!.id)
  }, 'workCancel')
}

function approveAndSend() {
  return run(async () => {
    const id = view.value!.id
    try {
      // After a failed send the campaign is already approved (or sending, with messages not queued
      // yet); approving again would be a 409. Send itself is safe to repeat.
      if (status.value !== 'approved' && status.value !== 'sending') await api.approve(id)
      await api.send(id)
    } finally {
      // Even when send failed, show the real state (approved) so the Send button appears.
      await load(id).catch(() => {})
    }
  }, 'workSend')
}

let es: EventSource | null = null
function closeProgress() {
  es?.close()
  es = null
}
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
    closeProgress()
    load(id)
  })
}

function reset() {
  closeProgress()
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

// Called once by App.vue when it mounts.
function start() {
  api.mode().then((m) => (mode.value = m.mode)).catch(() => {})
  const id = Number(location.hash.slice(1))
  if (id) run(() => load(id))
}

export function useCampaign() {
  return {
    mode, brief, channels, abTest, scheduleAt, view, suggestion, audienceCounts, streamText, msgs, problems, saved, dash,
    busy, working, errors, canRetry, errorBox,
    status, isMock, sending,
    run, retry, createAndSuggest, suggestAgain, useAudience, generate, edited, approveAndSchedule, cancelSchedule,
    approveAndSend, reset, start, closeProgress,
  }
}
