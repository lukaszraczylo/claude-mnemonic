<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { fetchObservationScore, type ScoreBreakdown } from '@/utils/api'
import Card from './Card.vue'

const props = defineProps<{
  observationId: number
  show: boolean
}>()

const emit = defineEmits<{
  close: []
}>()

const loading = ref(false)
const error = ref<string | null>(null)
const data = ref<ScoreBreakdown | null>(null)

const loadScore = async () => {
  if (!props.observationId) return

  loading.value = true
  error.value = null

  try {
    data.value = await fetchObservationScore(props.observationId)
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load score breakdown'
  } finally {
    loading.value = false
  }
}

// Load on mount and when ID changes
onMounted(() => {
  if (props.show) loadScore()
})

watch(() => props.show, (newVal) => {
  if (newVal) loadScore()
})

watch(() => props.observationId, () => {
  if (props.show) loadScore()
})

// Score bar helper
const getScoreBarWidth = (value: number, max: number = 2) => {
  return `${Math.min(100, Math.max(0, (value / max) * 100))}%`
}

// Score color helper
const getScoreColor = (value: number) => {
  if (value >= 1.5) return 'bg-green-500'
  if (value >= 1) return 'bg-amber-500'
  if (value >= 0.5) return 'bg-orange-500'
  return 'bg-red-500'
}
</script>

<template>
  <!-- Modal Backdrop -->
  <Teleport to="body">
    <div
      v-if="show"
      class="fixed inset-0 z-50 flex items-center justify-center"
    >
      <!-- Backdrop -->
      <div
        class="absolute inset-0 bg-black/60 backdrop-blur-sm"
        @click="emit('close')"
      />

      <!-- Modal Content -->
      <div class="relative w-full max-w-lg mx-4 max-h-[90vh] overflow-y-auto">
        <Card
          gradient="bg-gradient-to-br from-purple-500/10 to-indigo-500/5"
          border-class="border-purple-500/30"
        >
          <!-- Header -->
          <div class="flex items-center justify-between mb-4">
            <div class="flex items-center gap-2">
              <i class="fas fa-chart-bar text-purple-400" />
              <h3 class="text-lg font-semibold text-purple-100">Score Breakdown</h3>
            </div>
            <button
              @click="emit('close')"
              class="p-1.5 text-slate-400 hover:text-slate-200 hover:bg-slate-700/50 rounded-lg transition-colors"
            >
              <i class="fas fa-times" />
            </button>
          </div>

          <!-- Loading State -->
          <div v-if="loading" class="flex items-center justify-center py-8">
            <i class="fas fa-circle-notch fa-spin text-2xl text-purple-400" />
          </div>

          <!-- Error State -->
          <div v-else-if="error" class="text-center py-8">
            <i class="fas fa-exclamation-triangle text-2xl text-red-400 mb-2" />
            <p class="text-red-300">{{ error }}</p>
          </div>

          <!-- Content -->
          <div v-else-if="data" class="space-y-4">
            <!-- Observation Info -->
            <div class="p-3 bg-slate-800/50 rounded-lg">
              <div class="text-xs text-slate-500 uppercase tracking-wide mb-1">Observation</div>
              <div class="text-amber-100 font-medium">{{ data.observation.title || 'Untitled' }}</div>
              <div class="flex items-center gap-2 mt-1 text-xs text-slate-400">
                <span class="px-1.5 py-0.5 bg-slate-700/50 rounded">{{ data.observation.type }}</span>
                <span>{{ data.scoring.age_days.toFixed(1) }} days old</span>
              </div>
            </div>

            <!-- Final Score -->
            <div class="p-4 bg-gradient-to-r from-purple-500/20 to-indigo-500/10 rounded-lg border border-purple-500/30">
              <div class="flex items-center justify-between">
                <span class="text-sm text-slate-300">Final Score</span>
                <span class="text-2xl font-bold text-purple-300">{{ data.scoring.final_score.toFixed(3) }}</span>
              </div>
              <div class="mt-2 h-2 bg-slate-700 rounded-full overflow-hidden">
                <div
                  class="h-full transition-all duration-500"
                  :class="getScoreColor(data.scoring.final_score)"
                  :style="{ width: getScoreBarWidth(data.scoring.final_score) }"
                />
              </div>
            </div>

            <!-- Score Components -->
            <div class="space-y-3">
              <div class="text-xs text-slate-500 uppercase tracking-wide">Score Components</div>

              <!-- Type Weight -->
              <div class="flex items-center justify-between text-sm">
                <div class="flex items-center gap-2">
                  <i class="fas fa-tag text-blue-400 w-4" />
                  <span class="text-slate-300">Type Weight</span>
                </div>
                <span class="font-mono text-blue-300">{{ data.scoring.type_weight.toFixed(2) }}</span>
              </div>
              <p class="text-xs text-slate-500 ml-6 -mt-2">{{ data.explanation.type_impact }}</p>

              <!-- Recency Decay -->
              <div class="flex items-center justify-between text-sm">
                <div class="flex items-center gap-2">
                  <i class="fas fa-clock text-cyan-400 w-4" />
                  <span class="text-slate-300">Recency Decay</span>
                </div>
                <span class="font-mono text-cyan-300">{{ data.scoring.recency_decay.toFixed(2) }}</span>
              </div>
              <p class="text-xs text-slate-500 ml-6 -mt-2">{{ data.explanation.recency_impact }}</p>

              <!-- Core Score -->
              <div class="flex items-center justify-between text-sm">
                <div class="flex items-center gap-2">
                  <i class="fas fa-star text-amber-400 w-4" />
                  <span class="text-slate-300">Core Score</span>
                </div>
                <span class="font-mono text-amber-300">{{ data.scoring.core_score.toFixed(3) }}</span>
              </div>

              <!-- Feedback Contribution -->
              <div class="flex items-center justify-between text-sm">
                <div class="flex items-center gap-2">
                  <i class="fas fa-thumbs-up text-green-400 w-4" />
                  <span class="text-slate-300">Feedback</span>
                </div>
                <span class="font-mono" :class="data.scoring.feedback_contrib >= 0 ? 'text-green-300' : 'text-red-300'">
                  {{ data.scoring.feedback_contrib >= 0 ? '+' : '' }}{{ data.scoring.feedback_contrib.toFixed(3) }}
                </span>
              </div>
              <p class="text-xs text-slate-500 ml-6 -mt-2">{{ data.explanation.feedback_impact }}</p>

              <!-- Concept Contribution -->
              <div class="flex items-center justify-between text-sm">
                <div class="flex items-center gap-2">
                  <i class="fas fa-tags text-purple-400 w-4" />
                  <span class="text-slate-300">Concepts</span>
                </div>
                <span class="font-mono text-purple-300">+{{ data.scoring.concept_contrib.toFixed(3) }}</span>
              </div>
              <p class="text-xs text-slate-500 ml-6 -mt-2">{{ data.explanation.concept_impact }}</p>

              <!-- Retrieval Contribution -->
              <div class="flex items-center justify-between text-sm">
                <div class="flex items-center gap-2">
                  <i class="fas fa-search text-indigo-400 w-4" />
                  <span class="text-slate-300">Retrieval</span>
                </div>
                <span class="font-mono text-indigo-300">+{{ data.scoring.retrieval_contrib.toFixed(3) }}</span>
              </div>
              <p class="text-xs text-slate-500 ml-6 -mt-2">{{ data.explanation.retrieval_impact }}</p>
            </div>
          </div>
        </Card>
      </div>
    </div>
  </Teleport>
</template>
