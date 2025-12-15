import { ref, onMounted, onUnmounted } from 'vue'
import type { SelfCheckResponse } from '@/types'

const CHECK_INTERVAL = 30 * 1000 // 30 seconds

export function useHealth() {
  const health = ref<SelfCheckResponse | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  let intervalId: ReturnType<typeof setInterval> | null = null

  async function fetchHealth() {
    loading.value = true
    error.value = null
    try {
      const response = await fetch('/api/selfcheck')
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`)
      }
      health.value = await response.json()
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Unknown error'
      health.value = null
    } finally {
      loading.value = false
    }
  }

  function startPolling() {
    fetchHealth()
    intervalId = setInterval(fetchHealth, CHECK_INTERVAL)
  }

  function stopPolling() {
    if (intervalId) {
      clearInterval(intervalId)
      intervalId = null
    }
  }

  onMounted(() => {
    startPolling()
  })

  onUnmounted(() => {
    stopPolling()
  })

  return {
    health,
    loading,
    error,
    refresh: fetchHealth
  }
}
