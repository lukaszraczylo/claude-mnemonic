<script setup lang="ts">
import type { Stats } from '@/types'
import ProjectFilter from './ProjectFilter.vue'

defineProps<{
  stats: Stats | null
  observationCount: number
  promptCount: number
  summaryCount: number
  currentProject: string | null
}>()

defineEmits<{
  'update:project': [project: string | null]
}>()

function formatNumber(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toString()
}
</script>

<template>
  <aside class="w-72 flex-shrink-0 space-y-4">
    <!-- Project Filter -->
    <div class="bg-slate-800/50 rounded-lg p-4 border border-slate-700/50">
      <div class="flex items-center gap-2 mb-3">
        <i class="fas fa-filter text-claude-400" />
        <h3 class="text-sm font-semibold text-white">Filter by Project</h3>
      </div>
      <ProjectFilter
        :current-project="currentProject"
        @update:project="$emit('update:project', $event)"
      />
    </div>

    <!-- Memory Stats -->
    <div class="bg-slate-800/50 rounded-lg p-4 border border-slate-700/50">
      <div class="flex items-center gap-2 mb-3">
        <i class="fas fa-brain text-purple-400" />
        <h3 class="text-sm font-semibold text-white">Memory Contents</h3>
      </div>

      <div class="space-y-3">
        <!-- Observations -->
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <i class="fas fa-lightbulb text-amber-400 w-4" />
            <span class="text-slate-400 text-sm">Observations</span>
          </div>
          <span class="text-white font-medium">{{ formatNumber(observationCount) }}</span>
        </div>

        <!-- Prompts -->
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <i class="fas fa-comment text-blue-400 w-4" />
            <span class="text-slate-400 text-sm">Prompts</span>
          </div>
          <span class="text-white font-medium">{{ formatNumber(promptCount) }}</span>
        </div>

        <!-- Summaries -->
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <i class="fas fa-clipboard-list text-green-400 w-4" />
            <span class="text-slate-400 text-sm">Summaries</span>
          </div>
          <span class="text-white font-medium">{{ formatNumber(summaryCount) }}</span>
        </div>
      </div>
    </div>

    <!-- Retrieval Stats -->
    <div v-if="stats?.retrieval" class="bg-slate-800/50 rounded-lg p-4 border border-slate-700/50">
      <div class="flex items-center gap-2 mb-3">
        <i class="fas fa-search text-cyan-400" />
        <h3 class="text-sm font-semibold text-white">Retrieval Stats</h3>
      </div>

      <div class="space-y-3">
        <!-- Total Requests -->
        <div class="flex items-center justify-between">
          <span class="text-slate-400 text-sm">Total Requests</span>
          <span class="text-white font-medium">{{ formatNumber(stats.retrieval.TotalRequests) }}</span>
        </div>

        <!-- Observations Served -->
        <div class="flex items-center justify-between">
          <span class="text-slate-400 text-sm">Obs Served</span>
          <span class="text-white font-medium">{{ formatNumber(stats.retrieval.ObservationsServed) }}</span>
        </div>

        <!-- Search Requests -->
        <div class="flex items-center justify-between">
          <span class="text-slate-400 text-sm">Searches</span>
          <span class="text-white font-medium">{{ formatNumber(stats.retrieval.SearchRequests) }}</span>
        </div>

        <!-- Context Injections -->
        <div class="flex items-center justify-between">
          <span class="text-slate-400 text-sm">Injections</span>
          <span class="text-white font-medium">{{ formatNumber(stats.retrieval.ContextInjections) }}</span>
        </div>

        <!-- Verified Stale -->
        <div v-if="stats.retrieval.VerifiedStale > 0" class="flex items-center justify-between">
          <span class="text-slate-400 text-sm">Verified Stale</span>
          <span class="text-amber-400 font-medium">{{ formatNumber(stats.retrieval.VerifiedStale) }}</span>
        </div>

        <!-- Deleted Invalid -->
        <div v-if="stats.retrieval.DeletedInvalid > 0" class="flex items-center justify-between">
          <span class="text-slate-400 text-sm">Deleted Invalid</span>
          <span class="text-red-400 font-medium">{{ formatNumber(stats.retrieval.DeletedInvalid) }}</span>
        </div>
      </div>
    </div>

    <!-- Session Info -->
    <div v-if="stats" class="bg-slate-800/50 rounded-lg p-4 border border-slate-700/50">
      <div class="flex items-center gap-2 mb-3">
        <i class="fas fa-clock text-slate-400" />
        <h3 class="text-sm font-semibold text-white">Worker Info</h3>
      </div>

      <div class="space-y-3">
        <!-- Uptime -->
        <div class="flex items-center justify-between">
          <span class="text-slate-400 text-sm">Uptime</span>
          <span class="text-white font-medium text-xs">{{ stats.uptime }}</span>
        </div>

        <!-- Sessions Today -->
        <div class="flex items-center justify-between">
          <span class="text-slate-400 text-sm">Sessions Today</span>
          <span class="text-white font-medium">{{ stats.sessionsToday }}</span>
        </div>

        <!-- Connected Clients -->
        <div class="flex items-center justify-between">
          <span class="text-slate-400 text-sm">Connected Clients</span>
          <span class="text-white font-medium">{{ stats.connectedClients }}</span>
        </div>
      </div>
    </div>
  </aside>
</template>
