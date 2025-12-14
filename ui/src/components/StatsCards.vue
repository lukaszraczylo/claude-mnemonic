<script setup lang="ts">
import type { Stats } from '@/types'
import { formatUptime } from '@/utils/formatters'
import { computed } from 'vue'

const props = defineProps<{
  stats: Stats | null
  queueDepth: number
}>()

const uptime = computed(() => {
  if (!props.stats?.uptime) return '-'
  return formatUptime(props.stats.uptime)
})

const status = computed(() => {
  if (!props.stats) return 'Loading'
  if (props.queueDepth > 0) return 'Processing'
  if (props.stats.activeSessions > 0) return 'Active'
  return 'Idle'
})

const statusColor = computed(() => {
  if (status.value === 'Processing') return 'text-yellow-400'
  if (status.value === 'Active') return 'text-green-400'
  return 'text-slate-400'
})
</script>

<template>
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
    <!-- Uptime -->
    <div class="glass rounded-xl p-4 border border-white/10">
      <div class="flex items-center justify-between">
        <div>
          <p class="text-xs text-slate-400 uppercase tracking-wide">Uptime</p>
          <p class="text-2xl font-bold text-claude-400">{{ uptime }}</p>
        </div>
        <div class="w-10 h-10 rounded-full bg-claude-500/20 flex items-center justify-center">
          <i class="fas fa-clock text-claude-400" />
        </div>
      </div>
    </div>

    <!-- Active Sessions -->
    <div class="glass rounded-xl p-4 border border-white/10">
      <div class="flex items-center justify-between">
        <div>
          <p class="text-xs text-slate-400 uppercase tracking-wide">Active Sessions</p>
          <p class="text-2xl font-bold text-blue-400">{{ stats?.activeSessions ?? 0 }}</p>
        </div>
        <div class="w-10 h-10 rounded-full bg-blue-500/20 flex items-center justify-center">
          <i class="fas fa-terminal text-blue-400" />
        </div>
      </div>
    </div>

    <!-- Queue Depth -->
    <div class="glass rounded-xl p-4 border border-white/10">
      <div class="flex items-center justify-between">
        <div>
          <p class="text-xs text-slate-400 uppercase tracking-wide">Queue Depth</p>
          <p class="text-2xl font-bold text-purple-400">{{ queueDepth }}</p>
        </div>
        <div class="w-10 h-10 rounded-full bg-purple-500/20 flex items-center justify-center">
          <i class="fas fa-layer-group text-purple-400" />
        </div>
      </div>
    </div>

    <!-- Status -->
    <div class="glass rounded-xl p-4 border border-white/10">
      <div class="flex items-center justify-between">
        <div>
          <p class="text-xs text-slate-400 uppercase tracking-wide">Status</p>
          <p class="text-2xl font-bold" :class="statusColor">{{ status }}</p>
        </div>
        <div class="w-10 h-10 rounded-full bg-green-500/20 flex items-center justify-center">
          <i class="fas fa-signal text-green-400" />
        </div>
      </div>
    </div>
  </div>
</template>
