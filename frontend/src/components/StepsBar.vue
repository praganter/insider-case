<script setup lang="ts">
import { computed } from 'vue'
import type { Key } from '../i18n'
import { t } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const { view, status, reset } = useCampaign()

const STEPS: Key[] = ['stepBrief', 'stepAudience', 'stepMessage', 'stepDelivery']
const stepIndex = computed(() => {
  if (!view.value) return 0
  if (status.value === 'draft') return 1
  if (status.value === 'audience_ready' || status.value === 'content_ready') return 2
  return status.value === 'completed' ? 4 : 3
})
</script>

<template>
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
</template>
