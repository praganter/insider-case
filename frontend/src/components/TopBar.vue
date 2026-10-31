<script setup lang="ts">
import { lang, t, theme } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const { mode, isMock, listing, showList, reset } = useCampaign()
</script>

<template>
  <header class="topbar">
    <div class="brand">
      <h1>{{ t('appTitle') }}</h1>
      <span v-if="mode" class="badge" :class="{ mock: isMock }" :title="isMock ? t('mockTitle') : t('liveTitle')">
        {{ isMock ? 'MOCK' : 'LIVE' }}
      </span>
    </div>

    <nav class="tabs" :aria-label="t('appTitle')">
      <button class="tab" :class="{ on: !listing }" :aria-current="!listing ? 'page' : undefined" @click="listing && reset()">
        {{ t('compose') }}
      </button>
      <button class="tab" :class="{ on: listing }" :aria-current="listing ? 'page' : undefined" @click="!listing && showList()">
        {{ t('campaigns') }}
      </button>
    </nav>

    <div class="tools">
      <button class="seg" role="switch" :aria-checked="lang === 'en'" :aria-label="t('language')" @click="lang = lang === 'tr' ? 'en' : 'tr'">
        <span class="thumb" aria-hidden="true"></span>
        <span class="opt" :class="{ on: lang === 'tr' }">TR</span>
        <span class="opt" :class="{ on: lang === 'en' }">EN</span>
      </button>
      <button class="seg wide" role="switch" :aria-checked="theme === 'dark'" :aria-label="t('theme')" @click="theme = theme === 'dark' ? 'light' : 'dark'">
        <span class="thumb" aria-hidden="true"></span>
        <span class="opt" :class="{ on: theme === 'light' }">{{ t('light') }}</span>
        <span class="opt" :class="{ on: theme === 'dark' }">{{ t('dark') }}</span>
      </button>
    </div>
  </header>
</template>
