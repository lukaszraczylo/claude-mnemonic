<script setup lang="ts">
import type { SummaryFeedItem } from '@/types'
import { formatRelativeTime } from '@/utils/formatters'
import Card from './Card.vue'
import IconBox from './IconBox.vue'

defineProps<{
  summary: SummaryFeedItem
  highlight?: boolean
}>()
</script>

<template>
  <Card
    gradient="bg-gradient-to-br from-blue-500/10 to-indigo-500/5"
    border-class="border-blue-500/30"
    :highlight="highlight"
    class="mb-4 hover:border-blue-400/50"
  >
    <div class="flex items-start gap-4">
      <!-- Icon -->
      <IconBox icon="fa-clipboard-list" gradient="from-blue-500 to-indigo-700" />

      <!-- Content -->
      <div class="flex-1 min-w-0">
        <!-- Header -->
        <div class="flex items-center gap-2 mb-3">
          <span class="text-xs px-2 py-1 rounded-full bg-blue-500/20 text-blue-300 font-semibold uppercase tracking-wide">
            <i class="fas fa-clipboard-list mr-1" /> Summary
          </span>
          <span class="text-xs text-slate-500">{{ formatRelativeTime(summary.created_at) }}</span>
        </div>

        <!-- Request -->
        <div v-if="summary.request" class="mb-3">
          <h4 class="text-xs text-slate-500 uppercase tracking-wide mb-1">Request</h4>
          <p class="text-sm text-blue-100">{{ summary.request }}</p>
        </div>

        <!-- Sections -->
        <div class="grid gap-3 text-sm">
          <!-- Completed -->
          <div v-if="summary.completed" class="bg-green-500/10 rounded-lg p-3 border border-green-500/20">
            <div class="flex items-center gap-2 mb-1">
              <i class="fas fa-check-circle text-green-400" />
              <span class="text-xs text-green-300 uppercase tracking-wide font-medium">Completed</span>
            </div>
            <p class="text-slate-300">{{ summary.completed }}</p>
          </div>

          <!-- Learned -->
          <div v-if="summary.learned" class="bg-purple-500/10 rounded-lg p-3 border border-purple-500/20">
            <div class="flex items-center gap-2 mb-1">
              <i class="fas fa-graduation-cap text-purple-400" />
              <span class="text-xs text-purple-300 uppercase tracking-wide font-medium">Learned</span>
            </div>
            <p class="text-slate-300">{{ summary.learned }}</p>
          </div>

          <!-- Next Steps -->
          <div v-if="summary.next_steps" class="bg-cyan-500/10 rounded-lg p-3 border border-cyan-500/20">
            <div class="flex items-center gap-2 mb-1">
              <i class="fas fa-arrow-right text-cyan-400" />
              <span class="text-xs text-cyan-300 uppercase tracking-wide font-medium">Next Steps</span>
            </div>
            <p class="text-slate-300">{{ summary.next_steps }}</p>
          </div>
        </div>
      </div>
    </div>
  </Card>
</template>
