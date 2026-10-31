<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Key } from '../i18n'
import { hasKey } from '../i18n'
import { lang, t } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const { campaigns, busy, reset } = useCampaign()

const FILTERS = ['all', 'scheduled', 'sending', 'completed'] as const
const filter = ref<(typeof FILTERS)[number]>('all')
const rows = computed(() => (filter.value === 'all' ? campaigns.value : campaigns.value.filter((c) => c.status === filter.value)))
const countOf = (f: (typeof FILTERS)[number]) => (f === 'all' ? campaigns.value.length : campaigns.value.filter((c) => c.status === f).length)

const filterName = (f: (typeof FILTERS)[number]) => (f === 'all' ? t('filterAll') : t(`status_${f}`))
const statusName = (s: string) => (hasKey('status_' + s) ? t(('status_' + s) as Key) : s)
// A scheduled campaign is listed by when it goes out, the others by when they were made.
const when = (c: { status: string; scheduled_at: string | null; created_at: string }) =>
  new Date(c.status === 'scheduled' && c.scheduled_at ? c.scheduled_at : c.created_at).toLocaleString(lang.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
</script>

<template>
  <section class="listing">
    <div class="list-head">
      <header class="sec-head"><h2>{{ t('campaigns') }}</h2></header>
      <button class="primary" @click="reset">{{ t('newCampaign') }}</button>
    </div>
    <div class="filters" role="group" :aria-label="t('colStatus')">
      <button v-for="f in FILTERS" :key="f" class="filter" :class="{ on: filter === f }" :aria-pressed="filter === f" @click="filter = f">
        {{ filterName(f) }}<span class="n num">{{ countOf(f) }}</span>
      </button>
    </div>

    <table v-if="busy && !campaigns.length" class="campaigns" aria-hidden="true">
      <tbody>
        <tr v-for="i in 4" :key="i" class="skeleton"><td><i></i></td><td><i></i></td><td><i></i></td><td><i></i></td><td><i></i></td></tr>
      </tbody>
    </table>
    <p v-else-if="!rows.length" class="hint empty">{{ t('noCampaigns') }}</p>
    <table v-else class="campaigns">
      <thead>
        <tr>
          <th class="id">#</th>
          <th>{{ t('brief') }}</th>
          <th>{{ t('colStatus') }}</th>
          <th>{{ t('channels') }}</th>
          <th class="when">{{ t('colDate') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="c in rows" :key="c.id">
          <td class="id num">{{ c.id }}</td>
          <td class="brief-cell"><a :href="'#' + c.id">{{ c.brief }}</a></td>
          <td><span class="state" :class="c.status">{{ statusName(c.status) }}</span></td>
          <td>{{ c.channels.map((ch) => t(ch as 'sms' | 'email')).join(', ') }}</td>
          <td class="when num">{{ when(c) }}</td>
        </tr>
      </tbody>
    </table>
  </section>
</template>
