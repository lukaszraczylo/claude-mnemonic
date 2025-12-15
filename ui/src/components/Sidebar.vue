<script setup lang="ts">
import { ref, computed } from 'vue'
import type { Stats, SelfCheckResponse } from '@/types'
import ProjectFilter from './ProjectFilter.vue'

const props = defineProps<{
  stats: Stats | null
  observationCount: number
  promptCount: number
  summaryCount: number
  currentProject: string | null
  health: SelfCheckResponse | null
}>()

defineEmits<{
  'update:project': [project: string | null]
}>()

// Collapse state - persisted in localStorage
const isCollapsed = ref(localStorage.getItem('sidebar-collapsed') === 'true')

function toggleCollapse() {
  isCollapsed.value = !isCollapsed.value
  localStorage.setItem('sidebar-collapsed', String(isCollapsed.value))
}

function formatNumber(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toString()
}

// Health status helpers
const overallHealthIcon = computed(() => {
  if (!props.health) return 'fa-circle-question'
  switch (props.health.overall) {
    case 'healthy': return 'fa-circle-check'
    case 'degraded': return 'fa-triangle-exclamation'
    case 'unhealthy': return 'fa-circle-xmark'
  }
})

const overallHealthColor = computed(() => {
  if (!props.health) return 'text-slate-400'
  switch (props.health.overall) {
    case 'healthy': return 'text-green-400'
    case 'degraded': return 'text-amber-400'
    case 'unhealthy': return 'text-red-400'
  }
})

function getStatusColor(status: string): string {
  switch (status) {
    case 'healthy': return 'text-green-400'
    case 'degraded': return 'text-amber-400'
    case 'unhealthy': return 'text-red-400'
    default: return 'text-slate-400'
  }
}
</script>

<template>
  <aside
    :class="[
      'flex-shrink-0 transition-all duration-300 ease-in-out',
      isCollapsed ? 'w-12' : 'w-72'
    ]"
  >
    <!-- Collapse Toggle Button -->
    <button
      @click="toggleCollapse"
      class="w-full flex items-center justify-center py-2 mb-4 bg-slate-800/50 rounded-lg border border-slate-700/50 hover:bg-slate-700/50 transition-colors"
      :title="isCollapsed ? 'Expand sidebar' : 'Collapse sidebar'"
    >
      <i
        :class="[
          'fas transition-transform duration-300',
          isCollapsed ? 'fa-chevron-right' : 'fa-chevron-left'
        ]"
        class="text-slate-400"
      />
    </button>

    <div v-show="!isCollapsed" class="space-y-4">
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

      <!-- Component Health -->
      <div class="bg-slate-800/50 rounded-lg p-4 border border-slate-700/50">
        <div class="flex items-center gap-2 mb-3">
          <i :class="['fas', overallHealthIcon, overallHealthColor]" />
          <h3 class="text-sm font-semibold text-white">System Health</h3>
        </div>

        <div v-if="health" class="space-y-2">
          <div
            v-for="component in health.components"
            :key="component.name"
            class="flex items-center justify-between"
          >
            <span class="text-slate-400 text-sm truncate" :title="component.message">
              {{ component.name }}
            </span>
            <span
              :class="['text-xs font-medium capitalize', getStatusColor(component.status)]"
            >
              {{ component.status }}
            </span>
          </div>
        </div>
        <div v-else class="text-slate-500 text-sm">
          Loading health status...
        </div>
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
    </div>

    <!-- Collapsed State - Show icons only -->
    <div v-show="isCollapsed" class="space-y-2">
      <!-- Health indicator -->
      <div
        class="bg-slate-800/50 rounded-lg p-3 border border-slate-700/50 flex justify-center"
        :title="`System: ${health?.overall || 'Unknown'}`"
      >
        <i :class="['fas', overallHealthIcon, overallHealthColor]" />
      </div>

      <!-- Memory indicator -->
      <div
        class="bg-slate-800/50 rounded-lg p-3 border border-slate-700/50 flex justify-center"
        :title="`${observationCount} observations, ${promptCount} prompts`"
      >
        <i class="fas fa-brain text-purple-400" />
      </div>

      <!-- Stats indicator -->
      <div
        v-if="stats?.retrieval"
        class="bg-slate-800/50 rounded-lg p-3 border border-slate-700/50 flex justify-center"
        :title="`${stats.retrieval.TotalRequests} total requests`"
      >
        <i class="fas fa-search text-cyan-400" />
      </div>
    </div>
  </aside>
</template>
