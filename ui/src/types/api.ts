import type { Observation } from './observation'
import type { UserPrompt } from './prompt'
import type { SessionSummary } from './summary'

export type FeedItemType = 'observation' | 'prompt' | 'summary'

export interface ObservationFeedItem extends Observation {
  itemType: 'observation'
  timestamp: Date
}

export interface PromptFeedItem extends UserPrompt {
  itemType: 'prompt'
  timestamp: Date
}

export interface SummaryFeedItem extends SessionSummary {
  itemType: 'summary'
  timestamp: Date
}

export type FeedItem = ObservationFeedItem | PromptFeedItem | SummaryFeedItem

export interface RetrievalStats {
  TotalRequests: number
  ObservationsServed: number
  VerifiedStale: number
  DeletedInvalid: number
  SearchRequests: number
  ContextInjections: number
}

export interface Stats {
  uptime: string
  activeSessions: number
  queueDepth: number
  isProcessing: boolean
  connectedClients: number
  sessionsToday: number
  retrieval: RetrievalStats
}

export interface SSEEvent {
  type: 'processing_status' | 'observation' | 'session' | 'prompt' | 'summary' | 'heartbeat' | 'connected'
  title?: string
  action?: string
  project?: string
  isProcessing?: boolean
  queueDepth?: number
}

export type FilterType = 'all' | 'observations' | 'summaries' | 'prompts'

export interface ComponentHealth {
  name: string
  status: 'healthy' | 'degraded' | 'unhealthy'
  message?: string
}

export interface SelfCheckResponse {
  overall: 'healthy' | 'degraded' | 'unhealthy'
  version: string
  uptime: string
  components: ComponentHealth[]
}
