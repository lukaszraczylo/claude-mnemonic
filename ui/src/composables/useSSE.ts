import { ref, onMounted, onUnmounted } from 'vue'
import type { SSEEvent } from '@/types'

// Singleton state - shared across all useSSE() calls
const isConnected = ref(false)
const isReconnecting = ref(false)
const reconnectCountdown = ref(0)
const isProcessing = ref(false)
const queueDepth = ref(0)
const lastEvent = ref<SSEEvent | null>(null)

let eventSource: EventSource | null = null
let reconnectTimeout: number | null = null
let countdownInterval: number | null = null
let connectionCount = 0
let reconnectAttempt = 0

// Exponential backoff configuration
const MIN_BACKOFF = 1000      // 1 second
const MAX_BACKOFF = 30000     // 30 seconds
const BACKOFF_MULTIPLIER = 2
const JITTER_FACTOR = 0.2     // 20% jitter

function getBackoffDelay(): number {
  const baseDelay = Math.min(MIN_BACKOFF * Math.pow(BACKOFF_MULTIPLIER, reconnectAttempt), MAX_BACKOFF)
  const jitter = baseDelay * JITTER_FACTOR * Math.random()
  return Math.floor(baseDelay + jitter)
}

function startCountdown(delayMs: number) {
  reconnectCountdown.value = Math.ceil(delayMs / 1000)
  if (countdownInterval) {
    clearInterval(countdownInterval)
  }
  countdownInterval = window.setInterval(() => {
    reconnectCountdown.value = Math.max(0, reconnectCountdown.value - 1)
    if (reconnectCountdown.value <= 0 && countdownInterval) {
      clearInterval(countdownInterval)
      countdownInterval = null
    }
  }, 1000)
}

function stopCountdown() {
  if (countdownInterval) {
    clearInterval(countdownInterval)
    countdownInterval = null
  }
  reconnectCountdown.value = 0
}

export function useSSE() {

  const connect = () => {
    // Only create connection if not already connected
    if (eventSource) {
      return
    }

    eventSource = new EventSource('/api/events')

    eventSource.onopen = () => {
      isConnected.value = true
      isReconnecting.value = false
      stopCountdown()
      reconnectAttempt = 0 // Reset backoff on successful connection
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
      isReconnecting.value = true
      eventSource?.close()
      eventSource = null

      // Exponential backoff with jitter
      const delay = getBackoffDelay()
      reconnectAttempt++
      console.log(`[SSE] Reconnecting in ${Math.round(delay/1000)}s (attempt ${reconnectAttempt})`)

      startCountdown(delay)
      reconnectTimeout = window.setTimeout(() => {
        connect()
      }, delay)
    }
  }

  const disconnect = () => {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout)
      reconnectTimeout = null
    }
    stopCountdown()
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
    isConnected.value = false
    isReconnecting.value = false
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
    isReconnecting,
    reconnectCountdown,
    isProcessing,
    queueDepth,
    lastEvent,
    reconnect: connect
  }
}
