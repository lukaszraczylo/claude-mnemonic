<script setup lang="ts">
import type { ObservationFeedItem, RelationWithDetails } from '@/types'
import { TYPE_CONFIG, CONCEPT_CONFIG } from '@/types/observation'
import { RELATION_TYPE_CONFIG, DETECTION_SOURCE_CONFIG } from '@/types/relation'
import { formatRelativeTime } from '@/utils/formatters'
import { fetchObservationRelations } from '@/utils/api'
import Card from './Card.vue'
import IconBox from './IconBox.vue'
import Badge from './Badge.vue'
import RelationGraph from './RelationGraph.vue'
import ScoreBreakdown from './ScoreBreakdown.vue'
import { computed, ref, onMounted } from 'vue'

const props = defineProps<{
  observation: ObservationFeedItem
  highlight?: boolean
  showFeedback?: boolean
}>()

const emit = defineEmits<{
  navigateToObservation: [id: number]
}>()

// Local feedback and score state (optimistic updates)
const localFeedback = ref<number | null>(null)
const localScore = ref<number | null>(null)
const isSubmitting = ref(false)

const currentFeedback = computed(() =>
  localFeedback.value !== null ? localFeedback.value : (props.observation.user_feedback || 0)
)

const currentScore = computed(() =>
  localScore.value !== null ? localScore.value : (props.observation.importance_score || 1)
)

const submitFeedback = async (value: number) => {
  if (isSubmitting.value) return

  // Toggle off if clicking same button
  const newValue = currentFeedback.value === value ? 0 : value

  localFeedback.value = newValue
  isSubmitting.value = true

  try {
    const response = await fetch(`/api/observations/${props.observation.id}/feedback`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ feedback: newValue })
    })
    if (response.ok) {
      const data = await response.json()
      if (data.score !== undefined) {
        localScore.value = data.score
      }
    }
  } catch (error) {
    console.error('Error submitting feedback:', error)
  } finally {
    isSubmitting.value = false
  }
}

const config = computed(() => TYPE_CONFIG[props.observation.type] || TYPE_CONFIG.change)

const concepts = computed(() => {
  const raw = props.observation.concepts
  if (Array.isArray(raw)) return raw
  return []
})

const facts = computed(() => {
  const raw = props.observation.facts
  if (Array.isArray(raw)) return raw
  return []
})

const filesRead = computed(() => {
  const raw = props.observation.files_read
  if (Array.isArray(raw)) return raw
  return []
})

const filesModified = computed(() => {
  const raw = props.observation.files_modified
  if (Array.isArray(raw)) return raw
  return []
})

const hasFiles = computed(() => filesRead.value.length > 0 || filesModified.value.length > 0)

// Relations state
const relations = ref<RelationWithDetails[]>([])
const relationsLoading = ref(false)
const relationsExpanded = ref(false)
const showGraph = ref(false)

// Score breakdown state
const showScoreBreakdown = ref(false)

const hasRelations = computed(() => relations.value.length > 0)
const relationCount = computed(() => relations.value.length)

// Load relations on mount
const loadRelations = async () => {
  relationsLoading.value = true
  try {
    relations.value = await fetchObservationRelations(props.observation.id)
  } catch (err) {
    console.error('Failed to load relations:', err)
  } finally {
    relationsLoading.value = false
  }
}

onMounted(() => {
  loadRelations()
})

// Toggle relations expansion
const toggleRelations = () => {
  relationsExpanded.value = !relationsExpanded.value
}

// Open graph modal
const openGraph = (e: Event) => {
  e.stopPropagation()
  showGraph.value = true
}

// Handle navigation from graph
const handleNavigateTo = (id: number) => {
  showGraph.value = false
  emit('navigateToObservation', id)
}

// Get relation display info (whether we're source or target)
const getRelationDisplay = (rel: RelationWithDetails) => {
  const isSource = rel.relation.source_id === props.observation.id
  return {
    type: rel.relation.relation_type,
    otherTitle: isSource ? rel.target_title : rel.source_title,
    otherId: isSource ? rel.relation.target_id : rel.relation.source_id,
    direction: isSource ? 'outgoing' : 'incoming',
    confidence: rel.relation.confidence
  }
}

// Split path into project root and relative path for styling
// e.g., /Users/foo/project/src/file.go → { root: 'project', path: 'src/file.go' }
const splitPath = (path: string, components = 3) => {
  const parts = path.split('/').filter(Boolean)
  if (parts.length <= components) {
    return { root: '', path: path }
  }
  const relevant = parts.slice(-components)
  return {
    root: relevant[0],
    path: relevant.slice(1).join('/')
  }
}
</script>

<template>
  <Card
    :gradient="`bg-gradient-to-br from-amber-500/10 to-orange-500/5`"
    :border-class="config.borderClass"
    :highlight="highlight"
    class="mb-4 hover:border-amber-400/50"
  >
    <div class="flex items-start gap-4">
      <!-- Icon -->
      <IconBox :icon="config.icon" :gradient="config.gradient" />

      <!-- Content -->
      <div class="flex-1 min-w-0">
        <!-- Header -->
        <div class="flex items-center gap-2 mb-2 flex-wrap">
          <Badge
            :icon="config.icon"
            :color-class="config.colorClass"
            :bg-class="config.bgClass"
            :border-class="config.borderClass"
          >
            {{ observation.type.toUpperCase() }}
          </Badge>
          <span class="text-xs text-slate-500">{{ formatRelativeTime(observation.created_at) }}</span>
          <span v-if="observation.project" class="text-xs text-slate-500 flex items-center gap-1">
            <span class="text-slate-600">·</span>
            <i class="fas fa-folder text-slate-600 text-[10px]" />
            <span class="text-amber-600/80 font-mono">{{ observation.project.split('/').pop() }}</span>
          </span>
        </div>

        <!-- Title & Subtitle -->
        <h3 class="text-lg font-semibold text-amber-100 mb-1">
          {{ observation.title || 'Untitled' }}
        </h3>
        <p v-if="observation.subtitle || observation.narrative" class="text-sm text-slate-300 mb-2">
          {{ observation.subtitle || observation.narrative }}
        </p>

        <!-- Concepts -->
        <div v-if="concepts.length > 0" class="flex flex-wrap gap-1.5 mt-2">
          <Badge
            v-for="concept in concepts"
            :key="concept"
            :icon="CONCEPT_CONFIG[concept as keyof typeof CONCEPT_CONFIG]?.icon || 'fa-tag'"
            :color-class="CONCEPT_CONFIG[concept as keyof typeof CONCEPT_CONFIG]?.colorClass"
            :bg-class="CONCEPT_CONFIG[concept as keyof typeof CONCEPT_CONFIG]?.bgClass"
            :border-class="CONCEPT_CONFIG[concept as keyof typeof CONCEPT_CONFIG]?.borderClass"
          >
            {{ concept }}
          </Badge>
        </div>

        <!-- Facts -->
        <div v-if="facts.length > 0" class="mt-3 space-y-1.5">
          <div class="text-xs text-slate-500 uppercase tracking-wide mb-1">Key Facts</div>
          <div v-for="(fact, index) in facts" :key="index" class="flex items-start gap-2 text-sm text-slate-300">
            <i class="fas fa-check text-amber-500/70 mt-0.5 flex-shrink-0 text-xs" />
            <span>{{ fact }}</span>
          </div>
        </div>

        <!-- Files -->
        <div v-if="hasFiles" class="mt-3 pt-3 border-t border-slate-700/50">
          <div class="space-y-1 text-xs">
            <div v-if="filesRead.length > 0" class="flex items-start gap-1.5">
              <i class="fas fa-eye text-slate-600 mt-0.5" />
              <span class="text-slate-600">Read:</span>
              <div class="flex flex-wrap gap-x-2 gap-y-0.5">
                <span v-for="(file, index) in filesRead" :key="file" :title="file" class="cursor-default font-mono">
                  <span class="text-amber-600">{{ splitPath(file).root }}</span><span v-if="splitPath(file).root && splitPath(file).path">/</span><span class="text-slate-400 hover:text-slate-300">{{ splitPath(file).path }}</span><span v-if="index < filesRead.length - 1" class="text-slate-600">,</span>
                </span>
              </div>
            </div>
            <div v-if="filesModified.length > 0" class="flex items-start gap-1.5">
              <i class="fas fa-pen text-slate-600 mt-0.5" />
              <span class="text-slate-600">Modified:</span>
              <div class="flex flex-wrap gap-x-2 gap-y-0.5">
                <span v-for="(file, index) in filesModified" :key="file" :title="file" class="cursor-default font-mono">
                  <span class="text-amber-600">{{ splitPath(file).root }}</span><span v-if="splitPath(file).root && splitPath(file).path">/</span><span class="text-slate-400 hover:text-slate-300">{{ splitPath(file).path }}</span><span v-if="index < filesModified.length - 1" class="text-slate-600">,</span>
                </span>
              </div>
            </div>
          </div>
        </div>

        <!-- Relations -->
        <div v-if="hasRelations || relationsLoading" class="mt-3 pt-3 border-t border-slate-700/50">
          <!-- Header with count and graph button -->
          <div class="flex items-center justify-between">
            <button
              @click="toggleRelations"
              class="flex items-center gap-2 text-xs text-slate-400 hover:text-slate-300 transition-colors"
              :disabled="relationsLoading"
            >
              <i class="fas fa-diagram-project text-cyan-500/70" />
              <span v-if="relationsLoading" class="text-slate-500">
                <i class="fas fa-circle-notch fa-spin mr-1" />
                Loading relations...
              </span>
              <span v-else>
                {{ relationCount }} related observation{{ relationCount !== 1 ? 's' : '' }}
              </span>
              <i
                v-if="!relationsLoading && hasRelations"
                class="fas text-[10px] transition-transform"
                :class="relationsExpanded ? 'fa-chevron-up' : 'fa-chevron-down'"
              />
            </button>

            <!-- View Graph button -->
            <button
              v-if="hasRelations"
              @click="openGraph"
              class="flex items-center gap-1.5 px-2 py-1 text-xs text-cyan-400 hover:text-cyan-300 bg-cyan-500/10 hover:bg-cyan-500/20 rounded transition-colors"
              title="View knowledge graph"
            >
              <i class="fas fa-project-diagram" />
              <span>View Graph</span>
            </button>
          </div>

          <!-- Expanded relations list -->
          <div
            v-if="relationsExpanded && hasRelations"
            class="mt-2 space-y-1.5"
          >
            <div
              v-for="rel in relations"
              :key="rel.relation.id"
              class="flex items-center gap-2 text-xs p-1.5 rounded bg-slate-800/30 hover:bg-slate-800/50 transition-colors group"
            >
              <!-- Relation type icon -->
              <i
                class="fas w-4 text-center"
                :class="[
                  RELATION_TYPE_CONFIG[getRelationDisplay(rel).type]?.icon || 'fa-link',
                  RELATION_TYPE_CONFIG[getRelationDisplay(rel).type]?.colorClass || 'text-slate-400'
                ]"
                :title="RELATION_TYPE_CONFIG[getRelationDisplay(rel).type]?.label"
              />

              <!-- Direction arrow -->
              <i
                class="fas text-[10px] text-slate-600"
                :class="getRelationDisplay(rel).direction === 'outgoing' ? 'fa-arrow-right' : 'fa-arrow-left'"
              />

              <!-- Related observation title -->
              <span
                class="flex-1 truncate text-slate-300 cursor-pointer hover:text-amber-300 transition-colors"
                :title="getRelationDisplay(rel).otherTitle"
                @click="emit('navigateToObservation', getRelationDisplay(rel).otherId)"
              >
                {{ getRelationDisplay(rel).otherTitle || 'Untitled' }}
              </span>

              <!-- Confidence -->
              <span
                class="text-[10px] text-slate-500 font-mono"
                :title="`${Math.round(getRelationDisplay(rel).confidence * 100)}% confidence`"
              >
                {{ Math.round(getRelationDisplay(rel).confidence * 100) }}%
              </span>

              <!-- Detection source icon -->
              <i
                class="fas text-[10px] text-slate-600"
                :class="DETECTION_SOURCE_CONFIG[rel.relation.detection_source]?.icon || 'fa-question'"
                :title="DETECTION_SOURCE_CONFIG[rel.relation.detection_source]?.label"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Feedback buttons (right side) -->
      <div v-if="showFeedback" class="flex flex-col items-center gap-1 ml-2 flex-shrink-0">
        <button
          @click="submitFeedback(1)"
          :disabled="isSubmitting"
          :class="[
            'p-1.5 rounded-lg transition-all duration-200',
            currentFeedback === 1
              ? 'bg-green-500/30 text-green-300 shadow-green-500/20 shadow-sm'
              : 'text-slate-500 hover:text-green-400 hover:bg-green-500/10'
          ]"
          title="Helpful"
        >
          <i class="fas fa-thumbs-up text-sm" />
        </button>

        <button
          @click="showScoreBreakdown = true"
          class="text-[10px] font-mono px-1.5 py-0.5 rounded bg-slate-800/50 text-slate-400 flex items-center gap-1 transition-all duration-300 hover:bg-purple-500/20 hover:text-purple-300 cursor-pointer"
          :class="{ 'text-green-400': localScore !== null && localScore > (observation.importance_score || 1), 'text-red-400': localScore !== null && localScore < (observation.importance_score || 1) }"
          :title="`Importance Score: ${currentScore.toFixed(3)}\nRetrieval Count: ${observation.retrieval_count || 0}\nClick for details`"
        >
          <i class="fas fa-chart-bar text-purple-500/60" />
          {{ currentScore.toFixed(2) }}
        </button>

        <button
          @click="submitFeedback(-1)"
          :disabled="isSubmitting"
          :class="[
            'p-1.5 rounded-lg transition-all duration-200',
            currentFeedback === -1
              ? 'bg-red-500/30 text-red-300 shadow-red-500/20 shadow-sm'
              : 'text-slate-500 hover:text-red-400 hover:bg-red-500/10'
          ]"
          title="Not helpful"
        >
          <i class="fas fa-thumbs-down text-sm" />
        </button>
      </div>
    </div>

    <!-- Relation Graph Modal -->
    <RelationGraph
      :observation-id="observation.id"
      :observation-title="observation.title || 'Untitled'"
      :show="showGraph"
      @close="showGraph = false"
      @navigate-to="handleNavigateTo"
    />

    <!-- Score Breakdown Modal -->
    <ScoreBreakdown
      :observation-id="observation.id"
      :show="showScoreBreakdown"
      @close="showScoreBreakdown = false"
    />
  </Card>
</template>
