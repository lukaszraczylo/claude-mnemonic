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

export interface GraphStats {
  enabled: boolean
  nodeCount: number
  edgeCount: number
  avgDegree: number
  maxDegree: number
  minDegree: number
  medianDegree: number
  edgeTypes: Record<string, number>
  config: {
    maxHops: number
    branchFactor: number
    edgeWeight: number
    rebuildIntervalMin: number
  }
  message?: string
}

export interface VectorMetrics {
  enabled: boolean
  queries: {
    total: number
    hubOnly: number
    hybrid: number
    onDemand: number
    graph: number
  }
  latency: {
    avg: string
    p50: string
    p95: string
    p99: string
    avgHub: string
    avgRecompute: string
  }
  storage: {
    totalDocuments: number
    hubDocuments: number
    storedEmbeddings: number
    savingsPercent: number
    recomputedTotal: number
  }
  cache: {
    hits: number
    misses: number
    hitRate: number
  }
  graph: {
    traversals: number
    avgDepth: number
  }
  uptime: string
  message?: string
}
