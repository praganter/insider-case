<script setup lang="ts">
import { computed } from 'vue'
import { progress, statusBars } from '../lib'
import { hasKey } from '../i18n'
import type { Key } from '../i18n'
import { lang, t } from '../composables/usePrefs'
import { useCampaign } from '../composables/useCampaign'

const { view, status, dash, sending, busy, approveAndSend, cancelSchedule } = useCampaign()

const prog = computed(() => progress(dash.value.counts))
const showVariants = computed(() => Object.keys(dash.value.variants ?? {}).length > 1)
const scheduledFor = computed(() =>
  view.value?.scheduled_at ? new Date(view.value.scheduled_at).toLocaleString(lang.value, { dateStyle: 'medium', timeStyle: 'short' }) : '',
)
// One group per variant when the campaign is an A/B test, otherwise everything together.
const deliveryGroups = computed(() => {
  const v = dash.value.variants ?? {}
  return showVariants.value
    ? Object.keys(v).sort().map((k) => ({ name: t('variant', { v: k }), counts: v[k] }))
    : [{ name: t('allMessages'), counts: dash.value.counts }]
})
const statusName = (s: string) => (hasKey('status_' + s) ? t(('status_' + s) as Key) : s)
</script>

<template>
  <section v-if="sending || status === 'scheduled' || status === 'approved'">
    <header class="sec-head"><span class="sec-no">04</span><h2>{{ t('delivery') }}</h2></header>
    <div v-if="status === 'approved'" class="row">
      <p role="status">{{ t('approvedNotSent') }}</p>
      <button class="primary" :disabled="busy" @click="approveAndSend">{{ t('sendNow') }}</button>
    </div>
    <div v-else-if="status === 'scheduled'" class="row">
      <p role="status">{{ t('willSendOn', { when: scheduledFor }) }}</p>
      <button class="outline" :disabled="busy" @click="cancelSchedule">{{ t('cancelSchedule') }}</button>
    </div>
    <template v-else>
      <div class="hero">
        <span class="big num">{{ prog.pct }}<small>%</small></span>
        <p class="caption">{{ t('statusLine', { status: statusName(dash.status || status), done: prog.done, total: prog.total }) }}</p>
      </div>
      <div class="bar" role="progressbar" :aria-label="t('delivery')" :aria-valuenow="prog.pct" aria-valuemin="0" aria-valuemax="100">
        <div :style="{ width: prog.pct + '%' }"></div>
      </div>
      <div class="delivery-groups">
        <div v-for="g in deliveryGroups" :key="g.name" class="delivery-group">
          <h3 class="group-name">{{ g.name }}</h3>
          <div v-for="b in statusBars(g.counts)" :key="b.status" class="status-row">
            <span class="status-name"><i class="swatch" :class="b.status"></i>{{ t(b.status) }}</span>
            <div class="track"><div class="fill" :class="b.status" :style="{ width: b.pct + '%' }"></div></div>
            <span class="status-n">{{ b.n }} <small>({{ b.pct }}%)</small></span>
          </div>
        </div>
      </div>
      <div v-if="status === 'sending' && prog.done === 0" class="row">
        <p class="hint">{{ t('notQueued') }}</p>
        <button class="outline" :disabled="busy" @click="approveAndSend">{{ t('sendNow') }}</button>
      </div>
      <p v-if="dash.counts.retrying" class="hint">{{ t('retryingHint') }}</p>
      <p v-if="(dash.status || status) === 'completed'" class="ok" role="status">{{ t('deliveryFinished') }}</p>
    </template>
  </section>
</template>
