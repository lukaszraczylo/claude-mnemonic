import { ref, onMounted, onUnmounted, watch, type Ref } from 'vue'
import type { Stats } from '@/types'
import { fetchStats } from '@/utils/api'
import { useSSE } from './useSSE'

// Fallback poll interval when SSE is disconnected
const FALLBACK_POLL_INTERVAL = 10000 // 10 seconds

export function useStats(projectRef?: Ref<string | null>) {
  const stats = ref<Stats | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  // SSE for real-time session updates
  const { lastEvent, isConnected } = useSSE()

  let fallbackIntervalId: number | null = null

  const refresh = async () => {
    loading.value = true
    error.value = null

    try {
      const project = projectRef?.value ?? null
      stats.value = await fetchStats(project)
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to fetch stats'
      console.error('[Stats] Error:', err)
    } finally {
      loading.value = false
    }
  }

  const startFallbackPolling = () => {
    if (fallbackIntervalId) return
    console.log('[Stats] SSE disconnected, starting fallback polling')
    fallbackIntervalId = window.setInterval(refresh, FALLBACK_POLL_INTERVAL)
  }

  const stopFallbackPolling = () => {
    if (fallbackIntervalId) {
      console.log('[Stats] SSE connected, stopping fallback polling')
      clearInterval(fallbackIntervalId)
      fallbackIntervalId = null
    }
  }

  // Watch for SSE events that affect stats
  watch(lastEvent, (event) => {
    if (event && (event.type === 'session' || event.type === 'processing_status')) {
      if (event.type === 'session') {
        console.log('[Stats] SSE session event triggered refresh:', event.action)
      }
      refresh()
    }
  })

  // Watch for project filter changes
  if (projectRef) {
    watch(projectRef, () => {
      console.log('[Stats] Project filter changed, refreshing stats')
      refresh()
    })
  }

  // Switch between SSE-driven and fallback polling based on connection status
  watch(isConnected, (connected) => {
    if (connected) {
      stopFallbackPolling()
      refresh() // Refresh immediately on reconnect
    } else {
      startFallbackPolling()
    }
  })

  onMounted(() => {
    refresh()
    // Start fallback polling only if SSE is not connected
    if (!isConnected.value) {
      startFallbackPolling()
    }
  })

  onUnmounted(() => {
    stopFallbackPolling()
  })

  return {
    stats,
    loading,
    error,
    refresh
  }
}
