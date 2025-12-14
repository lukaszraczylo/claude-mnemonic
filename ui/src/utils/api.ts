import type { Observation, UserPrompt, SessionSummary, Stats, FeedItem, ObservationFeedItem, PromptFeedItem, SummaryFeedItem } from '@/types'

const API_BASE = '/api'

async function fetchJson<T>(url: string): Promise<T> {
  const response = await fetch(url)
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchObservations(limit: number = 100, project?: string): Promise<Observation[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (project) params.append('project', project)
  return fetchJson<Observation[]>(`${API_BASE}/observations?${params}`)
}

export async function fetchPrompts(limit: number = 100, project?: string): Promise<UserPrompt[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (project) params.append('project', project)
  return fetchJson<UserPrompt[]>(`${API_BASE}/prompts?${params}`)
}

export async function fetchSummaries(limit: number = 50, project?: string): Promise<SessionSummary[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (project) params.append('project', project)
  return fetchJson<SessionSummary[]>(`${API_BASE}/summaries?${params}`)
}

export async function fetchStats(): Promise<Stats> {
  return fetchJson<Stats>(`${API_BASE}/stats`)
}

export async function fetchProjects(): Promise<string[]> {
  return fetchJson<string[]>(`${API_BASE}/projects`)
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
