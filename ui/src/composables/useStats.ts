import { ref, onMounted, onUnmounted, watch } from 'vue'
import type { Stats } from '@/types'
import { fetchStats } from '@/utils/api'
import { useSSE } from './useSSE'

export function useStats(pollInterval: number = 5000) {
  const stats = ref<Stats | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  // SSE for real-time session updates
  const { lastEvent } = useSSE()

  let intervalId: number | null = null

  const refresh = async () => {
    loading.value = true
    error.value = null

    try {
      stats.value = await fetchStats()
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to fetch stats'
      console.error('[Stats] Error:', err)
    } finally {
      loading.value = false
    }
  }

  const startPolling = () => {
    if (intervalId) return
    intervalId = window.setInterval(refresh, pollInterval)
  }

  const stopPolling = () => {
    if (intervalId) {
      clearInterval(intervalId)
      intervalId = null
    }
  }

  // Watch for SSE session events and refresh immediately
  watch(lastEvent, (event) => {
    if (event && event.type === 'session') {
      console.log('[Stats] SSE session event triggered refresh:', event.action)
      refresh()
    }
  })

  onMounted(() => {
    refresh()
    startPolling()
  })

  onUnmounted(() => {
    stopPolling()
  })

  return {
    stats,
    loading,
    error,
    refresh
  }
}
