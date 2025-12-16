import { ref, onMounted } from 'vue'

export interface TypesResponse {
  observation_types: string[]
  concept_types: string[]
}

// Default fallback types
const DEFAULT_OBSERVATION_TYPES = ['bugfix', 'feature', 'refactor', 'discovery', 'decision', 'change']
const DEFAULT_CONCEPT_TYPES = [
  'gotcha', 'pattern', 'problem-solution', 'trade-off',
  'how-it-works', 'why-it-exists', 'what-changed',
  'best-practice', 'anti-pattern', 'architecture',
  'security', 'performance', 'testing', 'debugging', 'workflow', 'tooling',
  'refactoring', 'api', 'database', 'configuration', 'error-handling',
  'caching', 'logging', 'auth', 'validation'
]

// Cached types data (shared across components)
const observationTypes = ref<string[]>(DEFAULT_OBSERVATION_TYPES)
const conceptTypes = ref<string[]>(DEFAULT_CONCEPT_TYPES)
const loaded = ref(false)
const loading = ref(false)

export function useTypes() {
  const fetchTypes = async () => {
    if (loaded.value || loading.value) return

    loading.value = true
    try {
      const response = await fetch('/api/types')
      if (!response.ok) throw new Error('Failed to fetch types')
      const data: TypesResponse = await response.json()
      observationTypes.value = data.observation_types
      conceptTypes.value = data.concept_types
      loaded.value = true
    } catch (error) {
      console.error('Failed to fetch types:', error)
      // Keep defaults
      loaded.value = true
    } finally {
      loading.value = false
    }
  }

  // Fetch on first use
  onMounted(fetchTypes)

  return {
    observationTypes,
    conceptTypes,
    loaded,
    loading,
    fetchTypes
  }
}
