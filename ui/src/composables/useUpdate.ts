import { ref, onMounted, onUnmounted } from 'vue'

export interface UpdateInfo {
  available: boolean
  current_version: string
  latest_version: string
  release_notes?: string
  published_at?: string
}

export interface UpdateStatus {
  state: 'idle' | 'checking' | 'downloading' | 'verifying' | 'applying' | 'done' | 'error'
  progress: number
  message: string
  error?: string
}

const CHECK_INTERVAL = 30 * 60 * 1000 // 30 minutes in milliseconds

export function useUpdate() {
  const updateInfo = ref<UpdateInfo | null>(null)
  const updateStatus = ref<UpdateStatus>({ state: 'idle', progress: 0, message: '' })
  const isChecking = ref(false)
  const isUpdating = ref(false)
  let statusInterval: ReturnType<typeof setInterval> | null = null
  let checkInterval: ReturnType<typeof setInterval> | null = null

  const checkForUpdate = async () => {
    isChecking.value = true
    try {
      const response = await fetch('/api/update/check')
      if (response.ok) {
        updateInfo.value = await response.json()
      }
    } catch (error) {
      console.error('Failed to check for updates:', error)
    } finally {
      isChecking.value = false
    }
  }

  const applyUpdate = async () => {
    if (!updateInfo.value?.available) return

    isUpdating.value = true
    try {
      const response = await fetch('/api/update/apply', { method: 'POST' })
      if (response.ok) {
        // Start polling for status
        startStatusPolling()
      }
    } catch (error) {
      console.error('Failed to apply update:', error)
      isUpdating.value = false
    }
  }

  const fetchStatus = async () => {
    try {
      const response = await fetch('/api/update/status')
      if (response.ok) {
        updateStatus.value = await response.json()

        // Stop polling when done or error
        if (updateStatus.value.state === 'done' || updateStatus.value.state === 'error') {
          stopStatusPolling()
          isUpdating.value = false
        }
      }
    } catch (error) {
      console.error('Failed to fetch update status:', error)
    }
  }

  const startStatusPolling = () => {
    if (statusInterval) return
    statusInterval = setInterval(fetchStatus, 1000)
    fetchStatus()
  }

  const stopStatusPolling = () => {
    if (statusInterval) {
      clearInterval(statusInterval)
      statusInterval = null
    }
  }

  const startPeriodicCheck = () => {
    if (checkInterval) return
    // Check every hour
    checkInterval = setInterval(checkForUpdate, CHECK_INTERVAL)
  }

  const stopPeriodicCheck = () => {
    if (checkInterval) {
      clearInterval(checkInterval)
      checkInterval = null
    }
  }

  // Check for updates on mount and start periodic checking
  onMounted(() => {
    checkForUpdate()
    startPeriodicCheck()
  })

  onUnmounted(() => {
    stopStatusPolling()
    stopPeriodicCheck()
  })

  return {
    updateInfo,
    updateStatus,
    isChecking,
    isUpdating,
    checkForUpdate,
    applyUpdate
  }
}
