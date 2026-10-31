<script setup lang="ts">
import { lang, t, theme } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const { mode, isMock } = useCampaign()
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
</template>
