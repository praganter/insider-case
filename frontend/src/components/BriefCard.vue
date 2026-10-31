<script setup lang="ts">
import { t } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const { brief, channels, abTest, view, busy, createAndSuggest } = useCampaign()
</script>

<template>
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
</template>
