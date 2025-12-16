import { ref, onMounted, onUnmounted } from 'vue'
import type { SSEEvent } from '@/types'

export function useSSE() {
  const isConnected = ref(false)
  const isProcessing = ref(false)
  const queueDepth = ref(0)
  const lastEvent = ref<SSEEvent | null>(null)

  let eventSource: EventSource | null = null
  let reconnectTimeout: number | null = null

  const connect = () => {
    if (eventSource) {
      eventSource.close()
    }

    eventSource = new EventSource('/api/events')

    eventSource.onopen = () => {
      isConnected.value = true
      console.log('[SSE] Connected')
    }

    eventSource.onmessage = (event) => {
      try {
        const data: SSEEvent = JSON.parse(event.data)
        lastEvent.value = data

        if (data.type === 'processing_status') {
          isProcessing.value = data.isProcessing ?? false
          queueDepth.value = data.queueDepth ?? 0
        }
      } catch (err) {
        console.error('[SSE] Parse error:', err)
      }
    }

    eventSource.onerror = () => {
      isConnected.value = false
      eventSource?.close()
      eventSource = null

      // Reconnect after 5 seconds
      reconnectTimeout = window.setTimeout(() => {
        console.log('[SSE] Reconnecting...')
        connect()
      }, 5000)
    }
  }

  const disconnect = () => {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout)
      reconnectTimeout = null
    }
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
    isConnected.value = false
  }

  // Handle page unload/refresh to ensure SSE connection is closed immediately
  const handleBeforeUnload = () => {
    disconnect()
  }

  // Handle pagehide for mobile browsers and bfcache
  const handlePageHide = (event: PageTransitionEvent) => {
    if (event.persisted) {
      // Page is being cached (bfcache), disconnect but don't prevent reconnect
      disconnect()
    }
  }

  // Handle pageshow to reconnect if page was restored from bfcache
  const handlePageShow = (event: PageTransitionEvent) => {
    if (event.persisted && !eventSource) {
      connect()
    }
  }

  onMounted(() => {
    // Add listeners to close SSE on page refresh/navigation
    window.addEventListener('beforeunload', handleBeforeUnload)
    window.addEventListener('pagehide', handlePageHide)
    window.addEventListener('pageshow', handlePageShow)
    connect()
  })

  onUnmounted(() => {
    window.removeEventListener('beforeunload', handleBeforeUnload)
    window.removeEventListener('pagehide', handlePageHide)
    window.removeEventListener('pageshow', handlePageShow)
    disconnect()
  })

  return {
    isConnected,
    isProcessing,
    queueDepth,
    lastEvent,
    reconnect: connect
  }
}
