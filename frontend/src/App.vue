<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { t } from './composables/usePrefs'
import { useCampaign } from './composables/useCampaign'
import TopBar from './components/TopBar.vue'
import SideRail from './components/SideRail.vue'
import BriefCard from './components/BriefCard.vue'
import AudienceCard from './components/AudienceCard.vue'
import MessageCard from './components/MessageCard.vue'
import DeliveryCard from './components/DeliveryCard.vue'
import CampaignList from './components/CampaignList.vue'

const { busy, working, errors, canRetry, errorBox, listing, retry, start, closeProgress } = useCampaign()

onMounted(start)
onUnmounted(closeProgress)
</script>

<template>
  <TopBar />

  <div class="shell" :class="{ wide: listing }">
    <SideRail v-if="!listing" />

    <main>
      <p v-if="busy && working" class="working" role="status"><span class="pulse" aria-hidden="true"></span>{{ t(working) }}</p>

      <div v-if="errors.length" ref="errorBox" class="error" role="alert" tabindex="-1">
        <p v-for="e in errors" :key="e">{{ e }}</p>
        <button v-if="canRetry" class="outline" :disabled="busy" @click="retry">{{ t('tryAgain') }}</button>
      </div>

      <CampaignList v-if="listing" />
      <template v-else>
        <BriefCard />
        <AudienceCard />
        <MessageCard />
        <DeliveryCard />
      </template>

      <footer class="credit">{{ t('developedBy') }}</footer>
    </main>
  </div>
</template>
