export type ObservationType = 'bugfix' | 'feature' | 'refactor' | 'discovery' | 'decision' | 'change'
export type ObservationScope = 'project' | 'global'
export type ConceptType = 'gotcha' | 'pattern' | 'problem-solution' | 'trade-off' | 'how-it-works' | 'why-it-exists' | 'what-changed'

export interface Observation {
  id: number
  sdk_session_id: string
  project: string
  scope: ObservationScope
  type: ObservationType
  title: string
  subtitle: string
  narrative: string
  facts: string[]
  concepts: string[]
  files_read: string[]
  files_modified: string[]
  file_mtimes: Record<string, number>
  prompt_number: number
  discovery_tokens: number
  created_at: string
  created_at_epoch: number
  is_stale?: boolean
}

export const OBSERVATION_TYPES: ObservationType[] = ['bugfix', 'feature', 'refactor', 'discovery', 'decision', 'change']

export const CONCEPT_TYPES: ConceptType[] = [
  'gotcha',
  'pattern',
  'problem-solution',
  'trade-off',
  'how-it-works',
  'why-it-exists',
  'what-changed'
]

export const TYPE_CONFIG: Record<ObservationType, { icon: string; colorClass: string; bgClass: string; borderClass: string; gradient: string }> = {
  bugfix: { icon: 'fa-bug', colorClass: 'text-red-300', bgClass: 'bg-red-500/20', borderClass: 'border-red-500/30', gradient: 'from-red-500 to-red-700' },
  feature: { icon: 'fa-star', colorClass: 'text-purple-300', bgClass: 'bg-purple-500/20', borderClass: 'border-purple-500/30', gradient: 'from-purple-500 to-purple-700' },
  refactor: { icon: 'fa-rotate', colorClass: 'text-blue-300', bgClass: 'bg-blue-500/20', borderClass: 'border-blue-500/30', gradient: 'from-blue-500 to-blue-700' },
  change: { icon: 'fa-pen', colorClass: 'text-slate-300', bgClass: 'bg-slate-500/20', borderClass: 'border-slate-500/30', gradient: 'from-slate-500 to-slate-700' },
  discovery: { icon: 'fa-magnifying-glass', colorClass: 'text-cyan-300', bgClass: 'bg-cyan-500/20', borderClass: 'border-cyan-500/30', gradient: 'from-cyan-500 to-cyan-700' },
  decision: { icon: 'fa-scale-balanced', colorClass: 'text-yellow-300', bgClass: 'bg-yellow-500/20', borderClass: 'border-yellow-500/30', gradient: 'from-yellow-500 to-yellow-700' },
}

export const CONCEPT_CONFIG: Record<ConceptType, { icon: string; colorClass: string; bgClass: string; borderClass: string }> = {
  gotcha: { icon: 'fa-triangle-exclamation', colorClass: 'text-red-300', bgClass: 'bg-red-500/20', borderClass: 'border-red-500/40' },
  pattern: { icon: 'fa-puzzle-piece', colorClass: 'text-purple-300', bgClass: 'bg-purple-500/20', borderClass: 'border-purple-500/40' },
  'problem-solution': { icon: 'fa-lightbulb', colorClass: 'text-blue-300', bgClass: 'bg-blue-500/20', borderClass: 'border-blue-500/40' },
  'trade-off': { icon: 'fa-scale-balanced', colorClass: 'text-yellow-300', bgClass: 'bg-yellow-500/20', borderClass: 'border-yellow-500/40' },
  'how-it-works': { icon: 'fa-gear', colorClass: 'text-cyan-300', bgClass: 'bg-cyan-500/20', borderClass: 'border-cyan-500/40' },
  'why-it-exists': { icon: 'fa-circle-question', colorClass: 'text-green-300', bgClass: 'bg-green-500/20', borderClass: 'border-green-500/40' },
  'what-changed': { icon: 'fa-clipboard-list', colorClass: 'text-slate-300', bgClass: 'bg-slate-500/20', borderClass: 'border-slate-500/40' },
}
