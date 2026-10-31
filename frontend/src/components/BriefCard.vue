<script setup lang="ts">
import { t } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const { brief, channels, abTest, view, busy, createAndSuggest } = useCampaign()
</script>

<template>
  <section>
    <header class="sec-head"><span class="sec-no">01</span><h2>{{ t('brief') }}</h2></header>
    <label for="brief" class="lead">{{ t('briefQuestion') }}</label>
    <textarea id="brief" v-model="brief" class="brief" rows="3" :disabled="!!view || busy" :placeholder="t('briefPlaceholder')"></textarea>
    <div class="options">
      <fieldset>
        <legend>{{ t('channels') }}</legend>
        <label v-for="ch in (['sms', 'email'] as const)" :key="ch" class="choice">
          <input type="checkbox" :value="ch" v-model="channels" :disabled="!!view || busy" />
          <span>{{ t(ch) }}</span>
        </label>
      </fieldset>
      <fieldset>
        <legend>A/B</legend>
        <label class="choice">
          <input type="checkbox" v-model="abTest" :disabled="!!view || busy" />
          <span>{{ t('abTest') }}</span>
        </label>
      </fieldset>
      <button v-if="!view" class="primary go" :disabled="busy || !brief.trim() || !channels.length" @click="createAndSuggest">
        {{ t('suggestAudience') }}<span aria-hidden="true"> →</span>
      </button>
    </div>
  </section>
</template>
