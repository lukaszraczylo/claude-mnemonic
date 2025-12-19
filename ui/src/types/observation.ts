export type ObservationType = 'bugfix' | 'feature' | 'refactor' | 'discovery' | 'decision' | 'change'
export type ObservationScope = 'project' | 'global'
export type ConceptType =
  // Semantic concepts
  | 'gotcha' | 'pattern' | 'problem-solution' | 'trade-off'
  | 'how-it-works' | 'why-it-exists' | 'what-changed'
  // Globalizable concepts
  | 'best-practice' | 'anti-pattern' | 'architecture'
  | 'security' | 'performance' | 'testing' | 'debugging' | 'workflow' | 'tooling'
  // Additional useful concepts
  | 'refactoring' | 'api' | 'database' | 'configuration' | 'error-handling'
  | 'caching' | 'logging' | 'auth' | 'validation'

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
  // Importance scoring fields
  importance_score: number
  user_feedback: number  // -1 (thumbs down), 0 (neutral), 1 (thumbs up)
  retrieval_count: number
  last_retrieved_at_epoch?: number
  score_updated_at_epoch?: number
}

export const OBSERVATION_TYPES: ObservationType[] = ['bugfix', 'feature', 'refactor', 'discovery', 'decision', 'change']

export const CONCEPT_TYPES: ConceptType[] = [
  // Semantic concepts
  'gotcha',
  'pattern',
  'problem-solution',
  'trade-off',
  'how-it-works',
  'why-it-exists',
  'what-changed',
  // Globalizable concepts
  'best-practice',
  'anti-pattern',
  'architecture',
  'security',
  'performance',
  'testing',
  'debugging',
  'workflow',
  'tooling',
  // Additional useful concepts
  'refactoring',
  'api',
  'database',
  'configuration',
  'error-handling',
  'caching',
  'logging',
  'auth',
  'validation'
]

export const TYPE_CONFIG: Record<ObservationType, { icon: string; colorClass: string; bgClass: string; borderClass: string; gradient: string }> = {
  bugfix: { icon: 'fa-bug', colorClass: 'text-red-300', bgClass: 'bg-red-500/20', borderClass: 'border-red-500/30', gradient: 'from-red-500 to-red-700' },
  feature: { icon: 'fa-star', colorClass: 'text-purple-300', bgClass: 'bg-purple-500/20', borderClass: 'border-purple-500/30', gradient: 'from-purple-500 to-purple-700' },
  refactor: { icon: 'fa-rotate', colorClass: 'text-blue-300', bgClass: 'bg-blue-500/20', borderClass: 'border-blue-500/30', gradient: 'from-blue-500 to-blue-700' },
  change: { icon: 'fa-pen', colorClass: 'text-slate-300', bgClass: 'bg-slate-500/20', borderClass: 'border-slate-500/30', gradient: 'from-slate-500 to-slate-700' },
  discovery: { icon: 'fa-magnifying-glass', colorClass: 'text-cyan-300', bgClass: 'bg-cyan-500/20', borderClass: 'border-cyan-500/30', gradient: 'from-cyan-500 to-cyan-700' },
  decision: { icon: 'fa-scale-balanced', colorClass: 'text-yellow-300', bgClass: 'bg-yellow-500/20', borderClass: 'border-yellow-500/30', gradient: 'from-yellow-500 to-yellow-700' },
}

// Default config for unknown concepts
const DEFAULT_CONCEPT_CONFIG = { icon: 'fa-tag', colorClass: 'text-slate-300', bgClass: 'bg-slate-500/20', borderClass: 'border-slate-500/40' }

export const CONCEPT_CONFIG: Record<ConceptType, { icon: string; colorClass: string; bgClass: string; borderClass: string }> = {
  // Semantic concepts
  gotcha: { icon: 'fa-triangle-exclamation', colorClass: 'text-red-300', bgClass: 'bg-red-500/20', borderClass: 'border-red-500/40' },
  pattern: { icon: 'fa-puzzle-piece', colorClass: 'text-purple-300', bgClass: 'bg-purple-500/20', borderClass: 'border-purple-500/40' },
  'problem-solution': { icon: 'fa-lightbulb', colorClass: 'text-blue-300', bgClass: 'bg-blue-500/20', borderClass: 'border-blue-500/40' },
  'trade-off': { icon: 'fa-scale-balanced', colorClass: 'text-yellow-300', bgClass: 'bg-yellow-500/20', borderClass: 'border-yellow-500/40' },
  'how-it-works': { icon: 'fa-gear', colorClass: 'text-cyan-300', bgClass: 'bg-cyan-500/20', borderClass: 'border-cyan-500/40' },
  'why-it-exists': { icon: 'fa-circle-question', colorClass: 'text-green-300', bgClass: 'bg-green-500/20', borderClass: 'border-green-500/40' },
  'what-changed': { icon: 'fa-clipboard-list', colorClass: 'text-slate-300', bgClass: 'bg-slate-500/20', borderClass: 'border-slate-500/40' },
  // Globalizable concepts
  'best-practice': { icon: 'fa-check-circle', colorClass: 'text-emerald-300', bgClass: 'bg-emerald-500/20', borderClass: 'border-emerald-500/40' },
  'anti-pattern': { icon: 'fa-ban', colorClass: 'text-red-300', bgClass: 'bg-red-500/20', borderClass: 'border-red-500/40' },
  architecture: { icon: 'fa-sitemap', colorClass: 'text-indigo-300', bgClass: 'bg-indigo-500/20', borderClass: 'border-indigo-500/40' },
  security: { icon: 'fa-shield-halved', colorClass: 'text-rose-300', bgClass: 'bg-rose-500/20', borderClass: 'border-rose-500/40' },
  performance: { icon: 'fa-gauge-high', colorClass: 'text-orange-300', bgClass: 'bg-orange-500/20', borderClass: 'border-orange-500/40' },
  testing: { icon: 'fa-vial', colorClass: 'text-teal-300', bgClass: 'bg-teal-500/20', borderClass: 'border-teal-500/40' },
  debugging: { icon: 'fa-bug', colorClass: 'text-amber-300', bgClass: 'bg-amber-500/20', borderClass: 'border-amber-500/40' },
  workflow: { icon: 'fa-diagram-project', colorClass: 'text-violet-300', bgClass: 'bg-violet-500/20', borderClass: 'border-violet-500/40' },
  tooling: { icon: 'fa-wrench', colorClass: 'text-zinc-300', bgClass: 'bg-zinc-500/20', borderClass: 'border-zinc-500/40' },
  // Additional useful concepts
  refactoring: { icon: 'fa-rotate', colorClass: 'text-blue-300', bgClass: 'bg-blue-500/20', borderClass: 'border-blue-500/40' },
  api: { icon: 'fa-plug', colorClass: 'text-lime-300', bgClass: 'bg-lime-500/20', borderClass: 'border-lime-500/40' },
  database: { icon: 'fa-database', colorClass: 'text-sky-300', bgClass: 'bg-sky-500/20', borderClass: 'border-sky-500/40' },
  configuration: { icon: 'fa-sliders', colorClass: 'text-fuchsia-300', bgClass: 'bg-fuchsia-500/20', borderClass: 'border-fuchsia-500/40' },
  'error-handling': { icon: 'fa-circle-exclamation', colorClass: 'text-red-300', bgClass: 'bg-red-500/20', borderClass: 'border-red-500/40' },
  caching: { icon: 'fa-bolt', colorClass: 'text-yellow-300', bgClass: 'bg-yellow-500/20', borderClass: 'border-yellow-500/40' },
  logging: { icon: 'fa-file-lines', colorClass: 'text-gray-300', bgClass: 'bg-gray-500/20', borderClass: 'border-gray-500/40' },
  auth: { icon: 'fa-key', colorClass: 'text-amber-300', bgClass: 'bg-amber-500/20', borderClass: 'border-amber-500/40' },
  validation: { icon: 'fa-check', colorClass: 'text-green-300', bgClass: 'bg-green-500/20', borderClass: 'border-green-500/40' },
}

// Helper to get config with fallback for unknown concepts
export function getConceptConfig(concept: string) {
  return CONCEPT_CONFIG[concept as ConceptType] || DEFAULT_CONCEPT_CONFIG
}
