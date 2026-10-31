<script setup lang="ts">
import { computed } from 'vue'
import { LIMITS, groupMessages, label, runes, splitProblems } from '../lib'
import type { Msg } from '../lib'
import { t } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const {
  view, status, msgs, problems, saved, streamText, scheduleAt, busy, generate, edited, approveAndSend, approveAndSchedule,
} = useCampaign()

const grouped = computed(() => groupMessages(msgs.value))
const manyLanguages = computed(() => new Set(msgs.value.map((m) => m.language)).size > 1)
const split = computed(() => splitProblems(problems.value, msgs.value))
// datetime-local has no zone; the minimum is "now" in the user's own time.
const nowLocal = () => new Date(Date.now() - new Date().getTimezoneOffset() * 60000).toISOString().slice(0, 16)

const key = (m: Msg) => label(m).replace(/ /g, '-')
const languageName = (code: string) => (code === 'tr' || code === 'en' ? t(`lang_${code}`) : code)
const limitFor = (m: Msg) => (m.channel === 'sms' ? LIMITS.sms : LIMITS.subject)
const sizeFor = (m: Msg) => runes(m.channel === 'sms' ? m.body : m.subject)
const overBy = (m: Msg) => Math.max(0, sizeFor(m) - limitFor(m))
const describedBy = (m: Msg) => `count-${key(m)}` + (split.value.own(m).length ? ` problems-${key(m)}` : '')
</script>

<template>
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
</template>
