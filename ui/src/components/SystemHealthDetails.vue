<script setup lang="ts">
import { ref, onMounted, watch, computed } from 'vue'
import { fetchSystemHealth, type SystemHealth } from '@/utils/api'
import Card from './Card.vue'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
}>()

const loading = ref(false)
const error = ref<string | null>(null)
const health = ref<SystemHealth | null>(null)

const loadData = async () => {
  if (!props.show) return

  loading.value = true
  error.value = null

  try {
    health.value = await fetchSystemHealth()
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load system health'
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

// Status helpers
const getStatusIcon = (status: string) => {
  switch (status) {
    case 'healthy': return 'fa-circle-check'
    case 'degraded': return 'fa-triangle-exclamation'
    case 'unhealthy': return 'fa-circle-xmark'
    default: return 'fa-circle-question'
  }
}

const getStatusColor = (status: string) => {
  switch (status) {
    case 'healthy': return 'text-green-400'
    case 'degraded': return 'text-amber-400'
    case 'unhealthy': return 'text-red-400'
    default: return 'text-slate-400'
  }
}

const getStatusBgColor = (status: string) => {
  switch (status) {
    case 'healthy': return 'bg-green-500/20 border-green-500/30'
    case 'degraded': return 'bg-amber-500/20 border-amber-500/30'
    case 'unhealthy': return 'bg-red-500/20 border-red-500/30'
    default: return 'bg-slate-500/20 border-slate-500/30'
  }
}

const getLatencyColor = (ms: number | undefined) => {
  if (!ms) return 'text-slate-400'
  if (ms < 10) return 'text-green-400'
  if (ms < 50) return 'text-amber-400'
  return 'text-red-400'
}

// Count healthy/degraded/unhealthy components
const componentCounts = computed(() => {
  if (!health.value) return { healthy: 0, degraded: 0, unhealthy: 0 }
  const counts = { healthy: 0, degraded: 0, unhealthy: 0 }
  for (const c of health.value.components) {
    if (c.status === 'healthy') counts.healthy++
    else if (c.status === 'degraded') counts.degraded++
    else if (c.status === 'unhealthy') counts.unhealthy++
  }
  return counts
})
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
      <div class="relative w-full max-w-xl mx-4 max-h-[90vh] overflow-y-auto">
        <Card
          gradient="bg-gradient-to-br from-emerald-500/10 to-green-500/5"
          border-class="border-emerald-500/30"
        >
          <!-- Header -->
          <div class="flex items-center justify-between mb-4">
            <div class="flex items-center gap-2">
              <i class="fas fa-heartbeat text-emerald-400" />
              <h3 class="text-lg font-semibold text-emerald-100">System Health</h3>
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
            <i class="fas fa-circle-notch fa-spin text-2xl text-emerald-400" />
          </div>

          <!-- Error State -->
          <div v-else-if="error" class="text-center py-8">
            <i class="fas fa-exclamation-triangle text-2xl text-red-400 mb-2" />
            <p class="text-red-300">{{ error }}</p>
          </div>

          <!-- Content -->
          <div v-else-if="health" class="space-y-5">
            <!-- Overall Status -->
            <div
              class="p-4 rounded-lg border"
              :class="getStatusBgColor(health.status)"
            >
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-3">
                  <i
                    class="fas text-3xl"
                    :class="[getStatusIcon(health.status), getStatusColor(health.status)]"
                  />
                  <div>
                    <div class="text-lg font-semibold capitalize" :class="getStatusColor(health.status)">
                      {{ health.status }}
                    </div>
                    <div class="text-xs text-slate-400">Overall System Status</div>
                  </div>
                </div>
                <div class="text-right">
                  <div class="text-sm text-slate-300 font-mono">{{ health.version }}</div>
                  <div class="text-xs text-slate-500">Version</div>
                </div>
              </div>
            </div>

            <!-- Component Status Summary -->
            <div class="grid grid-cols-3 gap-3 text-center">
              <div class="p-3 bg-slate-800/50 rounded-lg">
                <div class="text-xl font-bold text-green-400">{{ componentCounts.healthy }}</div>
                <div class="text-xs text-slate-500">Healthy</div>
              </div>
              <div class="p-3 bg-slate-800/50 rounded-lg">
                <div class="text-xl font-bold text-amber-400">{{ componentCounts.degraded }}</div>
                <div class="text-xs text-slate-500">Degraded</div>
              </div>
              <div class="p-3 bg-slate-800/50 rounded-lg">
                <div class="text-xl font-bold text-red-400">{{ componentCounts.unhealthy }}</div>
                <div class="text-xs text-slate-500">Unhealthy</div>
              </div>
            </div>

            <!-- Components List -->
            <div class="space-y-3">
              <div class="text-xs text-slate-500 uppercase tracking-wide">Components</div>

              <div class="space-y-2">
                <div
                  v-for="component in health.components"
                  :key="component.name"
                  class="flex items-center gap-3 p-3 bg-slate-800/30 rounded-lg"
                >
                  <!-- Status Icon -->
                  <i
                    class="fas w-5 text-center"
                    :class="[getStatusIcon(component.status), getStatusColor(component.status)]"
                  />

                  <!-- Name & Message -->
                  <div class="flex-1 min-w-0">
                    <div class="text-sm font-medium text-slate-200">{{ component.name }}</div>
                    <div v-if="component.message" class="text-xs text-slate-500 truncate" :title="component.message">
                      {{ component.message }}
                    </div>
                  </div>

                  <!-- Latency -->
                  <div v-if="component.latency_ms !== undefined" class="text-right">
                    <span class="font-mono text-sm" :class="getLatencyColor(component.latency_ms)">
                      {{ component.latency_ms.toFixed(1) }}ms
                    </span>
                  </div>

                  <!-- Status Badge -->
                  <span
                    class="text-xs font-medium capitalize px-2 py-0.5 rounded"
                    :class="[getStatusColor(component.status), getStatusBgColor(component.status)]"
                  >
                    {{ component.status }}
                  </span>
                </div>
              </div>
            </div>

            <!-- Warnings -->
            <div v-if="health.warnings && health.warnings.length > 0" class="space-y-3">
              <div class="text-xs text-slate-500 uppercase tracking-wide">Warnings</div>

              <div class="space-y-2">
                <div
                  v-for="(warning, index) in health.warnings"
                  :key="index"
                  class="flex items-start gap-2 p-3 bg-amber-500/10 border border-amber-500/30 rounded-lg text-sm"
                >
                  <i class="fas fa-exclamation-triangle text-amber-400 mt-0.5" />
                  <span class="text-amber-200">{{ warning }}</span>
                </div>
              </div>
            </div>

            <!-- Refresh Button -->
            <button
              @click="loadData"
              :disabled="loading"
              class="w-full py-2 text-sm text-emerald-400 hover:text-emerald-300 bg-emerald-500/10 hover:bg-emerald-500/20 rounded-lg transition-colors flex items-center justify-center gap-2"
            >
              <i class="fas fa-sync-alt" :class="{ 'fa-spin': loading }" />
              Refresh Health Status
            </button>
          </div>
        </Card>
      </div>
    </div>
  </Teleport>
</template>
