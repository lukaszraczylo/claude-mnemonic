<script setup lang="ts">
import { ref, computed } from 'vue'
import type { Stats, SelfCheckResponse } from '@/types'
import ProjectFilter from './ProjectFilter.vue'
import SearchAnalytics from './SearchAnalytics.vue'
import SystemHealthDetails from './SystemHealthDetails.vue'
import TopObservations from './TopObservations.vue'
import { useGraphMetrics } from '@/composables'

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
const metricsExpanded = ref(localStorage.getItem('metrics-expanded') === 'true')

// Graph metrics composable
const { graphStats, vectorMetrics, loading: metricsLoading, refresh: refreshMetrics } = useGraphMetrics()

// Search Analytics modal state
const showSearchAnalytics = ref(false)

// System Health Details modal state
const showHealthDetails = ref(false)

// Top Observations modal state
const showTopObservations = ref(false)

function toggleCollapse() {
  isCollapsed.value = !isCollapsed.value
  localStorage.setItem('sidebar-collapsed', String(isCollapsed.value))
}

function toggleMetrics() {
  metricsExpanded.value = !metricsExpanded.value
  localStorage.setItem('metrics-expanded', String(metricsExpanded.value))
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
        <div class="flex items-center justify-between mb-3">
          <div class="flex items-center gap-2">
            <i :class="['fas', overallHealthIcon, overallHealthColor]" />
            <h3 class="text-sm font-semibold text-white">System Health</h3>
          </div>
          <button
            @click="showHealthDetails = true"
            class="text-xs text-emerald-400 hover:text-emerald-300 transition-colors"
            title="View detailed health status"
          >
            <i class="fas fa-expand" />
          </button>
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
        <div class="flex items-center justify-between mb-3">
          <div class="flex items-center gap-2">
            <i class="fas fa-brain text-purple-400" />
            <h3 class="text-sm font-semibold text-white">Memory Contents</h3>
          </div>
          <button
            @click="showTopObservations = true"
            class="text-xs text-amber-400 hover:text-amber-300 transition-colors"
            title="View top observations"
          >
            <i class="fas fa-trophy" />
          </button>
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
        <div class="flex items-center justify-between mb-3">
          <div class="flex items-center gap-2">
            <i class="fas fa-search text-cyan-400" />
            <h3 class="text-sm font-semibold text-white">Retrieval Stats</h3>
          </div>
          <button
            @click="showSearchAnalytics = true"
            class="text-xs text-cyan-400 hover:text-cyan-300 transition-colors"
            title="View detailed analytics"
          >
            <i class="fas fa-chart-line" />
          </button>
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

      <!-- Advanced Metrics -->
      <div class="bg-slate-800/50 rounded-lg border border-slate-700/50">
        <button
          @click="toggleMetrics"
          class="w-full flex items-center justify-between p-4 hover:bg-slate-700/30 transition-colors rounded-lg"
        >
          <div class="flex items-center gap-2">
            <i class="fas fa-chart-line text-violet-400" />
            <h3 class="text-sm font-semibold text-white">Advanced Metrics</h3>
          </div>
          <i
            :class="[
              'fas text-slate-400 transition-transform duration-200',
              metricsExpanded ? 'fa-chevron-up' : 'fa-chevron-down'
            ]"
          />
        </button>

        <Transition name="expand">
          <div v-show="metricsExpanded" class="px-4 pb-4 space-y-4">
            <!-- Loading State -->
            <div v-if="metricsLoading" class="text-center py-4">
              <i class="fas fa-spinner fa-spin text-slate-400" />
              <p class="text-slate-500 text-sm mt-2">Loading metrics...</p>
            </div>

            <!-- Graph Stats -->
            <div v-else-if="graphStats?.enabled">
              <div class="flex items-center justify-between mb-2">
                <span class="text-xs text-slate-400 uppercase tracking-wide">Graph</span>
                <button
                  @click="refreshMetrics"
                  class="text-xs text-violet-400 hover:text-violet-300 transition-colors"
                  title="Refresh metrics"
                >
                  <i class="fas fa-sync-alt" />
                </button>
              </div>
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <span class="text-slate-400 text-sm">Nodes</span>
                  <span class="text-white font-medium">{{ formatNumber(graphStats.nodeCount) }}</span>
                </div>
                <div class="flex items-center justify-between">
                  <span class="text-slate-400 text-sm">Edges</span>
                  <span class="text-white font-medium">{{ formatNumber(graphStats.edgeCount) }}</span>
                </div>
                <div class="flex items-center justify-between">
                  <span class="text-slate-400 text-sm">Avg Degree</span>
                  <span class="text-white font-medium">{{ graphStats.avgDegree.toFixed(1) }}</span>
                </div>
                <div class="flex items-center justify-between">
                  <span class="text-slate-400 text-sm">Max Degree</span>
                  <span class="text-white font-medium">{{ graphStats.maxDegree }}</span>
                </div>
              </div>

              <!-- Vector Metrics -->
              <div v-if="vectorMetrics?.enabled" class="mt-4 pt-4 border-t border-slate-700/50">
                <div class="text-xs text-slate-400 uppercase tracking-wide mb-2">Vector Storage</div>
                <div class="space-y-2">
                  <div class="flex items-center justify-between">
                    <span class="text-slate-400 text-sm">Savings</span>
                    <span class="text-green-400 font-medium">
                      {{ vectorMetrics.storage.savingsPercent.toFixed(1) }}%
                    </span>
                  </div>
                  <div class="flex items-center justify-between">
                    <span class="text-slate-400 text-sm">Queries</span>
                    <span class="text-white font-medium">{{ formatNumber(vectorMetrics.queries.total) }}</span>
                  </div>
                  <div class="flex items-center justify-between">
                    <span class="text-slate-400 text-sm">Cache Hit</span>
                    <span class="text-cyan-400 font-medium">
                      {{ (vectorMetrics.cache.hitRate * 100).toFixed(1) }}%
                    </span>
                  </div>
                  <div class="flex items-center justify-between">
                    <span class="text-slate-400 text-sm">Avg Latency</span>
                    <span class="text-white font-medium text-xs">{{ vectorMetrics.latency.avg }}</span>
                  </div>
                </div>
              </div>
            </div>

            <!-- Disabled State -->
            <div v-else class="text-slate-500 text-sm py-2">
              {{ graphStats?.message || 'Metrics not available' }}
            </div>
          </div>
        </Transition>
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

      <!-- Metrics indicator -->
      <div
        v-if="graphStats?.enabled"
        class="bg-slate-800/50 rounded-lg p-3 border border-slate-700/50 flex justify-center"
        :title="`${graphStats.nodeCount} nodes, ${graphStats.edgeCount} edges`"
      >
        <i class="fas fa-chart-line text-violet-400" />
      </div>
    </div>

    <!-- Search Analytics Modal -->
    <SearchAnalytics
      :show="showSearchAnalytics"
      @close="showSearchAnalytics = false"
    />

    <!-- System Health Details Modal -->
    <SystemHealthDetails
      :show="showHealthDetails"
      @close="showHealthDetails = false"
    />

    <!-- Top Observations Modal -->
    <TopObservations
      :show="showTopObservations"
      :current-project="currentProject"
      @close="showTopObservations = false"
      @navigate-to-observation="$emit('update:project', null)"
    />
  </aside>
</template>

<style scoped>
.expand-enter-active,
.expand-leave-active {
  transition: all 0.3s ease;
  overflow: hidden;
  max-height: 500px;
}

.expand-enter-from,
.expand-leave-to {
  max-height: 0;
  opacity: 0;
}
</style>
