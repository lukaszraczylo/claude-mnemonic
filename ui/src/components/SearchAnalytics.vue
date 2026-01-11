<script setup lang="ts">
import { ref, onMounted, watch, computed } from 'vue'
import { fetchSearchAnalytics, fetchRecentSearches, type SearchAnalytics, type RecentQuery } from '@/utils/api'
import Card from './Card.vue'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
}>()

const loading = ref(false)
const error = ref<string | null>(null)
const analytics = ref<SearchAnalytics | null>(null)
const recentSearches = ref<RecentQuery[]>([])

const loadData = async () => {
  if (!props.show) return

  loading.value = true
  error.value = null

  try {
    const [analyticsData, searchesData] = await Promise.all([
      fetchSearchAnalytics(),
      fetchRecentSearches(20)
    ])
    analytics.value = analyticsData
    recentSearches.value = searchesData
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load search analytics'
  } finally {
    loading.value = false
  }
}

// Load on mount and when show changes
onMounted(() => {
  if (props.show) loadData()
})

watch(() => props.show, (newVal) => {
  if (newVal) loadData()
})

// Computed stats
const cacheHitRate = computed(() => {
  if (!analytics.value || analytics.value.total_searches === 0) return 0
  return (analytics.value.cache_hits / analytics.value.total_searches) * 100
})

const coalescedRate = computed(() => {
  if (!analytics.value || analytics.value.total_searches === 0) return 0
  return (analytics.value.coalesced_requests / analytics.value.total_searches) * 100
})

const errorRate = computed(() => {
  if (!analytics.value || analytics.value.total_searches === 0) return 0
  return (analytics.value.search_errors / analytics.value.total_searches) * 100
})

// Helper for latency color
const getLatencyColor = (ms: number) => {
  if (ms < 10) return 'text-green-400'
  if (ms < 50) return 'text-amber-400'
  return 'text-red-400'
}

// Helper for formatting time ago
const formatTimeAgo = (isoDate: string) => {
  const date = new Date(isoDate)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  return `${diffDays}d ago`
}
</script>

<template>
  <!-- Modal Backdrop -->
  <Teleport to="body">
    <div
      v-if="show"
      class="fixed inset-0 z-50 flex items-center justify-center"
    >
      <!-- Backdrop -->
      <div
        class="absolute inset-0 bg-black/60 backdrop-blur-sm"
        @click="emit('close')"
      />

      <!-- Modal Content -->
      <div class="relative w-full max-w-2xl mx-4 max-h-[90vh] overflow-y-auto">
        <Card
          gradient="bg-gradient-to-br from-cyan-500/10 to-blue-500/5"
          border-class="border-cyan-500/30"
        >
          <!-- Header -->
          <div class="flex items-center justify-between mb-4">
            <div class="flex items-center gap-2">
              <i class="fas fa-chart-line text-cyan-400" />
              <h3 class="text-lg font-semibold text-cyan-100">Search Analytics</h3>
            </div>
            <button
              @click="emit('close')"
              class="p-1.5 text-slate-400 hover:text-slate-200 hover:bg-slate-700/50 rounded-lg transition-colors"
            >
              <i class="fas fa-times" />
            </button>
          </div>

          <!-- Loading State -->
          <div v-if="loading" class="flex items-center justify-center py-8">
            <i class="fas fa-circle-notch fa-spin text-2xl text-cyan-400" />
          </div>

          <!-- Error State -->
          <div v-else-if="error" class="text-center py-8">
            <i class="fas fa-exclamation-triangle text-2xl text-red-400 mb-2" />
            <p class="text-red-300">{{ error }}</p>
          </div>

          <!-- Content -->
          <div v-else-if="analytics" class="space-y-6">
            <!-- Overview Stats Grid -->
            <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
              <!-- Total Searches -->
              <div class="p-3 bg-slate-800/50 rounded-lg text-center">
                <div class="text-2xl font-bold text-cyan-300">{{ analytics.total_searches.toLocaleString() }}</div>
                <div class="text-xs text-slate-500 uppercase tracking-wide">Total Searches</div>
              </div>

              <!-- Vector Searches -->
              <div class="p-3 bg-slate-800/50 rounded-lg text-center">
                <div class="text-2xl font-bold text-purple-300">{{ analytics.vector_searches.toLocaleString() }}</div>
                <div class="text-xs text-slate-500 uppercase tracking-wide">Vector Searches</div>
              </div>

              <!-- Filter Searches -->
              <div class="p-3 bg-slate-800/50 rounded-lg text-center">
                <div class="text-2xl font-bold text-blue-300">{{ analytics.filter_searches.toLocaleString() }}</div>
                <div class="text-xs text-slate-500 uppercase tracking-wide">Filter Searches</div>
              </div>

              <!-- Cache Hits -->
              <div class="p-3 bg-slate-800/50 rounded-lg text-center">
                <div class="text-2xl font-bold text-green-300">{{ analytics.cache_hits.toLocaleString() }}</div>
                <div class="text-xs text-slate-500 uppercase tracking-wide">Cache Hits</div>
              </div>
            </div>

            <!-- Performance Metrics -->
            <div class="space-y-3">
              <div class="text-xs text-slate-500 uppercase tracking-wide">Performance Metrics</div>

              <!-- Cache Hit Rate -->
              <div class="flex items-center justify-between p-3 bg-slate-800/30 rounded-lg">
                <div class="flex items-center gap-2">
                  <i class="fas fa-database text-green-400 w-5" />
                  <span class="text-slate-300">Cache Hit Rate</span>
                </div>
                <div class="flex items-center gap-2">
                  <div class="w-24 h-2 bg-slate-700 rounded-full overflow-hidden">
                    <div
                      class="h-full bg-green-500 transition-all"
                      :style="{ width: `${cacheHitRate}%` }"
                    />
                  </div>
                  <span class="font-mono text-green-300 w-16 text-right">{{ cacheHitRate.toFixed(1) }}%</span>
                </div>
              </div>

              <!-- Coalesced Rate -->
              <div class="flex items-center justify-between p-3 bg-slate-800/30 rounded-lg">
                <div class="flex items-center gap-2">
                  <i class="fas fa-compress-arrows-alt text-amber-400 w-5" />
                  <span class="text-slate-300">Coalesced Requests</span>
                </div>
                <div class="flex items-center gap-2">
                  <div class="w-24 h-2 bg-slate-700 rounded-full overflow-hidden">
                    <div
                      class="h-full bg-amber-500 transition-all"
                      :style="{ width: `${coalescedRate}%` }"
                    />
                  </div>
                  <span class="font-mono text-amber-300 w-16 text-right">{{ coalescedRate.toFixed(1) }}%</span>
                </div>
              </div>

              <!-- Error Rate -->
              <div class="flex items-center justify-between p-3 bg-slate-800/30 rounded-lg">
                <div class="flex items-center gap-2">
                  <i class="fas fa-exclamation-circle text-red-400 w-5" />
                  <span class="text-slate-300">Error Rate</span>
                </div>
                <div class="flex items-center gap-2">
                  <div class="w-24 h-2 bg-slate-700 rounded-full overflow-hidden">
                    <div
                      class="h-full bg-red-500 transition-all"
                      :style="{ width: `${Math.min(100, errorRate)}%` }"
                    />
                  </div>
                  <span class="font-mono text-red-300 w-16 text-right">{{ errorRate.toFixed(2) }}%</span>
                </div>
              </div>
            </div>

            <!-- Latency Stats -->
            <div class="space-y-3">
              <div class="text-xs text-slate-500 uppercase tracking-wide">Latency</div>

              <div class="grid grid-cols-3 gap-3">
                <!-- Average Latency -->
                <div class="p-3 bg-slate-800/50 rounded-lg text-center">
                  <div class="text-xl font-bold font-mono" :class="getLatencyColor(analytics.avg_latency_ms)">
                    {{ analytics.avg_latency_ms.toFixed(1) }}ms
                  </div>
                  <div class="text-xs text-slate-500">Average</div>
                </div>

                <!-- Vector Latency -->
                <div class="p-3 bg-slate-800/50 rounded-lg text-center">
                  <div class="text-xl font-bold font-mono" :class="getLatencyColor(analytics.avg_vector_latency_ms)">
                    {{ analytics.avg_vector_latency_ms.toFixed(1) }}ms
                  </div>
                  <div class="text-xs text-slate-500">Vector</div>
                </div>

                <!-- Filter Latency -->
                <div class="p-3 bg-slate-800/50 rounded-lg text-center">
                  <div class="text-xl font-bold font-mono" :class="getLatencyColor(analytics.avg_filter_latency_ms)">
                    {{ analytics.avg_filter_latency_ms.toFixed(1) }}ms
                  </div>
                  <div class="text-xs text-slate-500">Filter</div>
                </div>
              </div>
            </div>

            <!-- Recent Searches -->
            <div v-if="recentSearches.length > 0" class="space-y-3">
              <div class="text-xs text-slate-500 uppercase tracking-wide">Recent Searches</div>

              <div class="space-y-2 max-h-48 overflow-y-auto">
                <div
                  v-for="(search, index) in recentSearches"
                  :key="index"
                  class="flex items-center gap-3 p-2 bg-slate-800/30 rounded-lg text-sm"
                >
                  <i class="fas fa-search text-slate-500 text-xs" />
                  <span class="flex-1 text-slate-300 truncate" :title="search.query">{{ search.query }}</span>
                  <span v-if="search.project" class="text-xs text-amber-600/80 font-mono">{{ search.project.split('/').pop() }}</span>
                  <span v-if="search.type" class="text-xs text-cyan-500 bg-cyan-500/10 px-1.5 py-0.5 rounded">{{ search.type }}</span>
                  <span class="text-xs text-slate-500 font-mono">×{{ search.count }}</span>
                  <span class="text-xs text-slate-600">{{ formatTimeAgo(search.last_used) }}</span>
                </div>
              </div>
            </div>
          </div>
        </Card>
      </div>
    </div>
  </Teleport>
</template>
