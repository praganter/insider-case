<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { t } from './composables/usePrefs'
import { useCampaign } from './composables/useCampaign'
import TopBar from './components/TopBar.vue'
import StepsBar from './components/StepsBar.vue'
import BriefCard from './components/BriefCard.vue'
import AudienceCard from './components/AudienceCard.vue'
import MessageCard from './components/MessageCard.vue'
import DeliveryCard from './components/DeliveryCard.vue'

const { busy, working, errors, canRetry, errorBox, retry, start, closeProgress } = useCampaign()

onMounted(start)
onUnmounted(closeProgress)
</script>

<template>
  <TopBar />

  <main>
    <StepsBar />

    <p v-if="busy && working" class="working" role="status">{{ t(working) }}</p>

    <div v-if="errors.length" ref="errorBox" class="error" role="alert" tabindex="-1">
      <p v-for="e in errors" :key="e">{{ e }}</p>
      <button v-if="canRetry" :disabled="busy" @click="retry">{{ t('tryAgain') }}</button>
    </div>

    <BriefCard />
    <AudienceCard />
    <MessageCard />
    <DeliveryCard />
  </main>
</template>
