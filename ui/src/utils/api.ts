import type { Observation, UserPrompt, SessionSummary, Stats, FeedItem, ObservationFeedItem, PromptFeedItem, SummaryFeedItem, RelationWithDetails, RelationGraph, RelationStats } from '@/types'

const API_BASE = '/api'
const DEFAULT_TIMEOUT = 10000 // 10 seconds
const MAX_RETRIES = 3
const RETRY_DELAY = 1000 // 1 second base delay

interface FetchOptions {
  timeout?: number
  signal?: AbortSignal
  retries?: number
}

async function fetchJson<T>(url: string, options: FetchOptions = {}): Promise<T> {
  const { timeout = DEFAULT_TIMEOUT, signal } = options

  // Create timeout abort controller
  const timeoutController = new AbortController()
  const timeoutId = setTimeout(() => timeoutController.abort(), timeout)

  // Combine signals if both provided
  const combinedSignal = signal
    ? combineAbortSignals(signal, timeoutController.signal)
    : timeoutController.signal

  try {
    const response = await fetch(url, { signal: combinedSignal })
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`)
    }
    return response.json()
  } catch (err) {
    // Re-throw abort errors (user cancellation)
    if (err instanceof Error && err.name === 'AbortError') {
      // Check if it was a timeout vs user abort
      if (signal?.aborted) {
        throw err // User cancelled
      }
      throw new Error('Request timed out')
    }
    throw err
  } finally {
    clearTimeout(timeoutId)
  }
}

// Helper to combine multiple abort signals
function combineAbortSignals(...signals: AbortSignal[]): AbortSignal {
  const controller = new AbortController()
  for (const signal of signals) {
    if (signal.aborted) {
      controller.abort()
      break
    }
    signal.addEventListener('abort', () => controller.abort(), { once: true })
  }
  return controller.signal
}

// Fetch with retry logic
async function fetchWithRetry<T>(url: string, options: FetchOptions = {}): Promise<T> {
  const { retries = MAX_RETRIES, ...fetchOptions } = options
  let lastError: Error | null = null

  for (let attempt = 0; attempt < retries; attempt++) {
    try {
      return await fetchJson<T>(url, fetchOptions)
    } catch (err) {
      lastError = err instanceof Error ? err : new Error(String(err))

      // Don't retry on user abort
      if (lastError.name === 'AbortError') {
        throw lastError
      }

      // Don't retry on client errors (4xx)
      if (lastError.message.includes('HTTP 4')) {
        throw lastError
      }

      // Wait before retry (exponential backoff)
      if (attempt < retries - 1) {
        const delay = Math.min(RETRY_DELAY * Math.pow(2, attempt), 5000)
        await new Promise(r => setTimeout(r, delay))
      }
    }
  }

  throw lastError!
}

export async function fetchObservations(limit: number = 100, project?: string, signal?: AbortSignal): Promise<Observation[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (project) params.append('project', project)
  return fetchWithRetry<Observation[]>(`${API_BASE}/observations?${params}`, { signal })
}

export async function fetchPrompts(limit: number = 100, project?: string, signal?: AbortSignal): Promise<UserPrompt[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (project) params.append('project', project)
  return fetchWithRetry<UserPrompt[]>(`${API_BASE}/prompts?${params}`, { signal })
}

export async function fetchSummaries(limit: number = 50, project?: string, signal?: AbortSignal): Promise<SessionSummary[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (project) params.append('project', project)
  return fetchWithRetry<SessionSummary[]>(`${API_BASE}/summaries?${params}`, { signal })
}

export async function fetchStats(project?: string | null): Promise<Stats> {
  const params = new URLSearchParams()
  if (project) params.append('project', project)
  const query = params.toString()
  return fetchWithRetry<Stats>(`${API_BASE}/stats${query ? '?' + query : ''}`)
}

export async function fetchProjects(): Promise<string[]> {
  return fetchWithRetry<string[]>(`${API_BASE}/projects`)
}

/**
 * Combine and sort all feed items by timestamp
 */
export function combineTimeline(
  observations: Observation[],
  prompts: UserPrompt[],
  summaries: SessionSummary[]
): FeedItem[] {
  const obsItems: ObservationFeedItem[] = observations.map(o => ({
    ...o,
    itemType: 'observation' as const,
    timestamp: new Date(o.created_at)
  }))

  const promptItems: PromptFeedItem[] = prompts.map(p => ({
    ...p,
    itemType: 'prompt' as const,
    timestamp: new Date(p.created_at)
  }))

  const summaryItems: SummaryFeedItem[] = summaries.map(s => ({
    ...s,
    itemType: 'summary' as const,
    timestamp: new Date(s.created_at)
  }))

  return [...obsItems, ...promptItems, ...summaryItems]
    .sort((a, b) => b.timestamp.getTime() - a.timestamp.getTime())
}

// Relation API functions
export async function fetchObservationRelations(observationId: number, signal?: AbortSignal): Promise<RelationWithDetails[]> {
  return fetchWithRetry<RelationWithDetails[]>(`${API_BASE}/observations/${observationId}/relations`, { signal })
}

export async function fetchObservationGraph(observationId: number, depth: number = 2, signal?: AbortSignal): Promise<RelationGraph> {
  return fetchWithRetry<RelationGraph>(`${API_BASE}/observations/${observationId}/graph?depth=${depth}`, { signal })
}

export async function fetchRelatedObservations(observationId: number, minConfidence: number = 0.4, signal?: AbortSignal): Promise<Observation[]> {
  return fetchWithRetry<Observation[]>(`${API_BASE}/observations/${observationId}/related?min_confidence=${minConfidence}`, { signal })
}

export async function fetchRelationStats(signal?: AbortSignal): Promise<RelationStats> {
  return fetchWithRetry<RelationStats>(`${API_BASE}/relations/stats`, { signal })
}
