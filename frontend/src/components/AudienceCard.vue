<script setup lang="ts">
import { computed } from 'vue'
import { describeRule } from '../lib'
import type { Counts } from '../lib'
import { t } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const { view, status, suggestion, audienceCounts, busy, useAudience, suggestAgain } = useCampaign()

const audienceEmpty = computed(
  () => Object.keys(audienceCounts.value).length > 0 && Object.values(audienceCounts.value).some((n) => n === 0),
)
// The API returns counts as a JSON object, whose order is not stable; show SMS first.
const CHANNELS = ['sms', 'email'] as const
const inOrder = (counts: Counts) => CHANNELS.filter((ch) => ch in counts).map((ch) => [ch, counts[ch]] as const)
const suggestionCounts = computed(() => Object.values(suggestion.value?.counts ?? {}))
const nobodyMatches = computed(() => suggestionCounts.value.every((n) => n === 0))
const someChannelEmpty = computed(() => suggestionCounts.value.some((n) => n === 0))
</script>

<template>
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
</template>
