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

  onMounted(() => {
    connect()
  })

  onUnmounted(() => {
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
