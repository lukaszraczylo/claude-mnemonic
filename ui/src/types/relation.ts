export type RelationType = 'causes' | 'fixes' | 'supersedes' | 'depends_on' | 'relates_to' | 'evolves_from'
export type DetectionSource = 'file_overlap' | 'embedding_similarity' | 'temporal_proximity' | 'narrative_mention' | 'concept_overlap' | 'type_progression'

export interface ObservationRelation {
  id: number
  source_id: number
  target_id: number
  relation_type: RelationType
  confidence: number
  detection_source: DetectionSource
  reason: string
  created_at: string
  created_at_epoch: number
}

export interface RelationWithDetails {
  relation: ObservationRelation
  source_title: string
  target_title: string
  source_type: string
  target_type: string
}

export interface RelationGraph {
  center_id: number
  relations: RelationWithDetails[]
}

export interface RelationStats {
  total_count: number
  high_confidence: number
  by_type: Record<RelationType, number>
  min_confidence_used: number
}

// Configuration for relation type display
export const RELATION_TYPE_CONFIG: Record<RelationType, { icon: string; label: string; colorClass: string; bgClass: string; description: string }> = {
  causes: {
    icon: 'fa-arrow-right',
    label: 'Causes',
    colorClass: 'text-orange-300',
    bgClass: 'bg-orange-500/20',
    description: 'This observation caused the related issue'
  },
  fixes: {
    icon: 'fa-wrench',
    label: 'Fixes',
    colorClass: 'text-green-300',
    bgClass: 'bg-green-500/20',
    description: 'This observation fixes the related issue'
  },
  supersedes: {
    icon: 'fa-layer-group',
    label: 'Supersedes',
    colorClass: 'text-purple-300',
    bgClass: 'bg-purple-500/20',
    description: 'This observation replaces the older one'
  },
  depends_on: {
    icon: 'fa-link',
    label: 'Depends On',
    colorClass: 'text-blue-300',
    bgClass: 'bg-blue-500/20',
    description: 'This observation depends on the related one'
  },
  relates_to: {
    icon: 'fa-arrows-left-right',
    label: 'Related',
    colorClass: 'text-slate-300',
    bgClass: 'bg-slate-500/20',
    description: 'These observations are related'
  },
  evolves_from: {
    icon: 'fa-code-branch',
    label: 'Evolves From',
    colorClass: 'text-cyan-300',
    bgClass: 'bg-cyan-500/20',
    description: 'This observation evolved from the related one'
  }
}

// Configuration for detection source display
export const DETECTION_SOURCE_CONFIG: Record<DetectionSource, { icon: string; label: string }> = {
  file_overlap: { icon: 'fa-file-code', label: 'Shared files' },
  embedding_similarity: { icon: 'fa-brain', label: 'Semantic similarity' },
  temporal_proximity: { icon: 'fa-clock', label: 'Close in time' },
  narrative_mention: { icon: 'fa-quote-left', label: 'Mentioned in text' },
  concept_overlap: { icon: 'fa-tags', label: 'Shared concepts' },
  type_progression: { icon: 'fa-diagram-next', label: 'Natural progression' }
}
