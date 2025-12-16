import { ref, onMounted, onUnmounted } from 'vue'
import type { SSEEvent } from '@/types'

// Singleton state - shared across all useSSE() calls
const isConnected = ref(false)
const isProcessing = ref(false)
const queueDepth = ref(0)
const lastEvent = ref<SSEEvent | null>(null)

let eventSource: EventSource | null = null
let reconnectTimeout: number | null = null
let connectionCount = 0

export function useSSE() {

  const connect = () => {
    // Only create connection if not already connected
    if (eventSource) {
      return
    }

    eventSource = new EventSource('/api/events')

    eventSource.onopen = () => {
      isConnected.value = true
      console.log('[SSE] Connected')
    }

    eventSource.onmessage = (event) => {
      try {
        const data: SSEEvent = JSON.parse(event.data)

        // Debug: log all SSE events
        if (data.type !== 'processing_status') {
          console.log('[SSE] Event received:', data.type, data)
        }

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
    connectionCount++
    if (connectionCount === 1) {
      // First consumer - add listeners and connect
      window.addEventListener('beforeunload', handleBeforeUnload)
      window.addEventListener('pagehide', handlePageHide)
      window.addEventListener('pageshow', handlePageShow)
      connect()
    }
  })

  onUnmounted(() => {
    connectionCount--
    if (connectionCount === 0) {
      // Last consumer - remove listeners and disconnect
      window.removeEventListener('beforeunload', handleBeforeUnload)
      window.removeEventListener('pagehide', handlePageHide)
      window.removeEventListener('pageshow', handlePageShow)
      disconnect()
    }
  })

  return {
    isConnected,
    isProcessing,
    queueDepth,
    lastEvent,
    reconnect: connect
  }
}
