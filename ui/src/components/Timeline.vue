<script setup lang="ts">
import type { FeedItem } from '@/types'
import ObservationCard from './ObservationCard.vue'
import PromptCard from './PromptCard.vue'
import SummaryCard from './SummaryCard.vue'

defineProps<{
  items: FeedItem[]
  loading: boolean
}>()
</script>

<template>
  <div class="timeline">
    <!-- Loading State -->
    <div v-if="loading && items.length === 0" class="text-center py-12">
      <i class="fas fa-spinner fa-spin text-3xl text-claude-500 mb-4" />
      <p class="text-slate-400">Loading timeline...</p>
    </div>

    <!-- Empty State -->
    <div v-else-if="items.length === 0" class="text-center py-12">
      <i class="fas fa-inbox text-3xl text-slate-600 mb-4" />
      <p class="text-slate-400">No items to display</p>
    </div>

    <!-- Items -->
    <template v-else>
      <template v-for="(item, index) in items" :key="`${item.itemType}-${item.id}`">
        <ObservationCard
          v-if="item.itemType === 'observation'"
          :observation="item"
          :highlight="index === 0"
          :show-feedback="true"
        />
        <PromptCard
          v-else-if="item.itemType === 'prompt'"
          :prompt="item"
          :highlight="index === 0"
        />
        <SummaryCard
          v-else-if="item.itemType === 'summary'"
          :summary="item"
          :highlight="index === 0"
        />
      </template>
    </template>
  </div>
</template>
