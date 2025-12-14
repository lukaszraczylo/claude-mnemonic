import { ref, computed, onMounted, watch } from 'vue'
import type { FeedItem, FilterType, ObservationType, ConceptType } from '@/types'
import { fetchObservations, fetchPrompts, fetchSummaries, combineTimeline } from '@/utils/api'
import { useSSE } from './useSSE'

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
    loading.value = true
    error.value = null

    try {
      // Use different limits based on project selection:
      // All projects: 50 of each type
      // Specific project: 100 of each type
      const project = currentProject.value || undefined
      const limit = project ? 100 : 50

      const [obs, prm, sum] = await Promise.all([
        fetchObservations(limit, project),
        fetchPrompts(limit, project),
        fetchSummaries(limit, project)
      ])

      // Combine into timeline
      allItems.value = combineTimeline(obs, prm, sum)

      // Update individual arrays for counting (data already filtered by project from API)
      observations.value = allItems.value.filter(i => i.itemType === 'observation')
      prompts.value = allItems.value.filter(i => i.itemType === 'prompt')
      summaries.value = allItems.value.filter(i => i.itemType === 'summary')
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to fetch timeline'
      console.error('[Timeline] Error:', err)
    } finally {
      loading.value = false
    }
  }

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

  // Watch for SSE events and refresh
  watch(lastEvent, (event) => {
    if (event && (event.type === 'observation' || event.type === 'prompt' || event.type === 'summary')) {
      refresh()
    }
  })

  onMounted(() => {
    refresh()
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
