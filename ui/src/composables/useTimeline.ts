import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import type { FeedItem, FilterType, ObservationType, ConceptType } from '@/types'
import { fetchObservations, fetchPrompts, fetchSummaries, combineTimeline } from '@/utils/api'
import { useSSE } from './useSSE'

// Debounce utility
function debounce<T extends (...args: unknown[]) => void>(fn: T, ms: number): T & { cancel: () => void } {
  let timeoutId: number | null = null
  const debounced = ((...args: unknown[]) => {
    if (timeoutId) clearTimeout(timeoutId)
    timeoutId = window.setTimeout(() => fn(...args), ms)
  }) as T & { cancel: () => void }
  debounced.cancel = () => {
    if (timeoutId) clearTimeout(timeoutId)
  }
  return debounced
}

export function useTimeline() {
  const observations = ref<FeedItem[]>([])
  const prompts = ref<FeedItem[]>([])
  const summaries = ref<FeedItem[]>([])
  const allItems = ref<FeedItem[]>([])

  const loading = ref(false)
  const error = ref<string | null>(null)

  // Filters
  const currentFilter = ref<FilterType>('all')
  const currentProject = ref<string | null>(null)
  const currentTypeFilter = ref<ObservationType | null>(null)
  const currentConceptFilter = ref<ConceptType | null>(null)

  // Request cancellation
  let abortController: AbortController | null = null

  // SSE for real-time updates
  const { lastEvent } = useSSE()

  // Counts (reflect data fetched for current project/all)
  const observationCount = computed(() => observations.value.length)
  const promptCount = computed(() => prompts.value.length)
  const summaryCount = computed(() => summaries.value.length)

  // Filtered items (further filter by type/concept within fetched data)
  const filteredItems = computed(() => {
    let items = [...allItems.value]

    // Filter by main type
    if (currentFilter.value === 'observations') {
      items = items.filter(item => item.itemType === 'observation')
    } else if (currentFilter.value === 'summaries') {
      items = items.filter(item => item.itemType === 'summary')
    } else if (currentFilter.value === 'prompts') {
      items = items.filter(item => item.itemType === 'prompt')
    }

    // Filter by observation type
    if (currentTypeFilter.value) {
      items = items.filter(item => {
        if (item.itemType !== 'observation') return false
        return item.type === currentTypeFilter.value
      })
    }

    // Filter by concept
    if (currentConceptFilter.value) {
      items = items.filter(item => {
        if (item.itemType !== 'observation') return false
        const concepts = item.concepts || []
        return concepts.includes(currentConceptFilter.value!)
      })
    }

    return items
  })

  const refresh = async () => {
    // Cancel any in-flight request
    if (abortController) {
      abortController.abort()
    }
    abortController = new AbortController()
    const signal = abortController.signal

    loading.value = true
    error.value = null

    try {
      // Use different limits based on project selection:
      // All projects: 50 of each type
      // Specific project: 100 of each type
      const project = currentProject.value || undefined
      const limit = project ? 100 : 50

      const [obs, prm, sum] = await Promise.all([
        fetchObservations(limit, project, signal),
        fetchPrompts(limit, project, signal),
        fetchSummaries(limit, project, signal)
      ])

      // Combine into timeline
      allItems.value = combineTimeline(obs, prm, sum)

      // Update individual arrays for counting (data already filtered by project from API)
      observations.value = allItems.value.filter(i => i.itemType === 'observation')
      prompts.value = allItems.value.filter(i => i.itemType === 'prompt')
      summaries.value = allItems.value.filter(i => i.itemType === 'summary')
    } catch (err) {
      // Ignore aborted requests
      if (err instanceof Error && err.name === 'AbortError') {
        return
      }
      error.value = err instanceof Error ? err.message : 'Failed to fetch timeline'
      console.error('[Timeline] Error:', err)
    } finally {
      loading.value = false
    }
  }

  // Debounced refresh for SSE events (300ms delay)
  const debouncedRefresh = debounce(() => {
    console.log('[Timeline] Debounced refresh triggered')
    refresh()
  }, 300)

  const setFilter = (filter: FilterType) => {
    currentFilter.value = filter
  }

  const setProject = (project: string | null) => {
    currentProject.value = project
    // Refresh when project changes to fetch correct data
    refresh()
  }

  const setTypeFilter = (type: ObservationType | null) => {
    currentTypeFilter.value = type
  }

  const setConceptFilter = (concept: ConceptType | null) => {
    currentConceptFilter.value = concept
  }

  // Watch for SSE events and debounced refresh
  watch(lastEvent, (event) => {
    if (event && (event.type === 'observation' || event.type === 'prompt' || event.type === 'summary')) {
      console.log('[Timeline] SSE event queued refresh:', event.type)
      debouncedRefresh()
    }
  })

  onMounted(() => {
    refresh()
  })

  onUnmounted(() => {
    // Cancel pending requests and debounced calls
    debouncedRefresh.cancel()
    if (abortController) {
      abortController.abort()
    }
  })

  return {
    allItems,
    filteredItems,
    loading,
    error,
    observationCount,
    promptCount,
    summaryCount,
    currentFilter,
    currentProject,
    currentTypeFilter,
    currentConceptFilter,
    refresh,
    setFilter,
    setProject,
    setTypeFilter,
    setConceptFilter
  }
}
