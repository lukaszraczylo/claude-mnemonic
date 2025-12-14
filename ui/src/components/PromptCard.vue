<script setup lang="ts">
import type { PromptFeedItem } from '@/types'
import { formatRelativeTime, truncate } from '@/utils/formatters'
import Card from './Card.vue'
import Badge from './Badge.vue'
import { computed } from 'vue'

const props = defineProps<{
  prompt: PromptFeedItem
  highlight?: boolean
}>()

const displayText = computed(() => {
  const text = props.prompt.prompt_text || ''
  return truncate(text, 500)
})
</script>

<template>
  <Card
    gradient="bg-gradient-to-br from-emerald-500/10 to-green-500/5"
    border-class="border-emerald-500/30"
    :highlight="highlight"
    class="mb-4 hover:border-emerald-400/50"
  >
    <div class="flex items-start gap-3">
      <!-- User Icon -->
      <div class="w-10 h-10 rounded-full bg-emerald-500/20 flex items-center justify-center flex-shrink-0">
        <i class="fas fa-user text-emerald-400" />
      </div>

      <!-- Content -->
      <div class="flex-1 min-w-0">
        <!-- Header -->
        <div class="flex items-center gap-2 flex-wrap mb-2">
          <!-- Memory Match Badge -->
          <Badge
            v-if="prompt.matched_observations > 0"
            icon="fa-brain"
            color-class="text-amber-300"
            bg-class="bg-amber-500/20"
            border-class="border-amber-500/40"
          >
            {{ prompt.matched_observations }} memory match{{ prompt.matched_observations !== 1 ? 'es' : '' }}
          </Badge>

          <!-- Prompt Number & Time -->
          <span class="text-xs text-slate-500">
            #{{ prompt.prompt_number }} · {{ formatRelativeTime(prompt.created_at) }}
          </span>
        </div>

        <!-- Prompt Text -->
        <p class="text-sm text-slate-300 whitespace-pre-wrap">{{ displayText }}</p>
      </div>
    </div>
  </Card>
</template>
