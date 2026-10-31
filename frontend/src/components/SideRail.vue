<script setup lang="ts">
import { computed } from 'vue'
import { hasKey } from '../i18n'
import type { Key } from '../i18n'
import { lang, t } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const { view, status, reset } = useCampaign()

const STEPS: Key[] = ['stepBrief', 'stepAudience', 'stepMessage', 'stepDelivery']
const stepIndex = computed(() => {
  if (!view.value) return 0
  if (status.value === 'draft') return 1
  if (status.value === 'audience_ready' || status.value === 'content_ready') return 2
  return status.value === 'completed' ? 4 : 3
})

const statusName = (s: string) => (hasKey('status_' + s) ? t(('status_' + s) as Key) : s)
const scheduledFor = computed(() =>
  view.value?.scheduled_at ? new Date(view.value.scheduled_at).toLocaleString(lang.value, { dateStyle: 'medium', timeStyle: 'short' }) : '',
)
</script>

<template>
  <aside class="rail">
    <ol class="steps" :aria-label="t('progress')">
      <li v-for="(s, i) in STEPS" :key="s" class="step" :class="{ done: i < stepIndex, current: i === stepIndex }" :aria-current="i === stepIndex ? 'step' : undefined">
        <span class="dot" aria-hidden="true">{{ i < stepIndex ? '✓' : i + 1 }}</span>
        <span class="step-name">{{ t(s) }}</span>
      </li>
    </ol>

    <dl v-if="view" class="facts">
      <div>
        <dt>#</dt>
        <dd class="num">{{ view.id }}</dd>
      </div>
      <div>
        <dt>{{ t('colStatus') }}</dt>
        <dd><span class="state" :class="status">{{ statusName(status) }}</span></dd>
      </div>
      <div>
        <dt>{{ t('channels') }}</dt>
        <dd>{{ view.channels.map((ch) => t(ch as 'sms' | 'email')).join(', ') }}</dd>
      </div>
      <div v-if="view.ab_test">
        <dt>A/B</dt>
        <dd>{{ t('variant', { v: 'A' }) }} · {{ t('variant', { v: 'B' }) }}</dd>
      </div>
      <div v-if="status === 'scheduled' && scheduledFor">
        <dt>{{ t('colDate') }}</dt>
        <dd>{{ scheduledFor }}</dd>
      </div>
    </dl>

    <button v-if="view" class="quiet" @click="reset">{{ t('newCampaign') }}</button>
  </aside>
</template>
