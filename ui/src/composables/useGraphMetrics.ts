import { ref, onMounted } from 'vue'
import type { GraphStats, VectorMetrics } from '@/types'
import { fetchGraphStats, fetchVectorMetrics } from '@/utils/api'

export function useGraphMetrics() {
  const graphStats = ref<GraphStats | null>(null)
  const vectorMetrics = ref<VectorMetrics | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  const refresh = async () => {
    loading.value = true
    error.value = null

    try {
      // Fetch both in parallel
      const [graph, vector] = await Promise.all([
        fetchGraphStats(),
        fetchVectorMetrics()
      ])

      graphStats.value = graph
      vectorMetrics.value = vector
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to fetch metrics'
      console.error('[GraphMetrics] Error:', err)
    } finally {
      loading.value = false
    }
  }

  onMounted(() => {
    refresh()
  })

  return {
    graphStats,
    vectorMetrics,
    loading,
    error,
    refresh
  }
}
