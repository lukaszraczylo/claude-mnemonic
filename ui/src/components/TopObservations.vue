<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { fetchTopObservations, fetchMostRetrievedObservations } from '@/utils/api'
import type { Observation } from '@/types'
import Card from './Card.vue'

const props = defineProps<{
  show: boolean
  currentProject: string | null
}>()

const emit = defineEmits<{
  close: []
  navigateToObservation: [id: number]
}>()

const loading = ref(false)
const error = ref<string | null>(null)
const topObservations = ref<Observation[]>([])
const mostRetrieved = ref<Observation[]>([])
const activeTab = ref<'top' | 'retrieved'>('top')

const loadData = async () => {
  if (!props.show) return

  loading.value = true
  error.value = null

  try {
    const project = props.currentProject || undefined
    const [topData, retrievedData] = await Promise.all([
      fetchTopObservations(project, 15),
      fetchMostRetrievedObservations(project, 15)
    ])
    topObservations.value = topData
    mostRetrieved.value = retrievedData
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load observations'
  } finally {
    loading.value = false
  }
}

// Load on mount and when show changes
onMounted(() => {
  if (props.show) loadData()
})

watch(() => props.show, (newVal) => {
  if (newVal) loadData()
})

// Also reload when project changes
watch(() => props.currentProject, () => {
  if (props.show) loadData()
})

// Type config for styling
const typeConfig: Record<string, { icon: string; colorClass: string; bgClass: string }> = {
  discovery: { icon: 'fa-lightbulb', colorClass: 'text-amber-400', bgClass: 'bg-amber-500/20' },
  bugfix: { icon: 'fa-bug', colorClass: 'text-red-400', bgClass: 'bg-red-500/20' },
  change: { icon: 'fa-code-branch', colorClass: 'text-blue-400', bgClass: 'bg-blue-500/20' },
  refactor: { icon: 'fa-wrench', colorClass: 'text-purple-400', bgClass: 'bg-purple-500/20' },
  feature: { icon: 'fa-star', colorClass: 'text-green-400', bgClass: 'bg-green-500/20' },
  pattern: { icon: 'fa-puzzle-piece', colorClass: 'text-cyan-400', bgClass: 'bg-cyan-500/20' },
  architecture: { icon: 'fa-sitemap', colorClass: 'text-indigo-400', bgClass: 'bg-indigo-500/20' },
  preference: { icon: 'fa-heart', colorClass: 'text-pink-400', bgClass: 'bg-pink-500/20' }
}

const getTypeConfig = (type: string) => {
  return typeConfig[type] || { icon: 'fa-circle', colorClass: 'text-slate-400', bgClass: 'bg-slate-500/20' }
}

// Format score for display
const formatScore = (score: number) => {
  return score.toFixed(2)
}

// Current observations based on active tab
const currentObservations = () => {
  return activeTab.value === 'top' ? topObservations.value : mostRetrieved.value
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
      <div class="relative w-full max-w-2xl mx-4 max-h-[90vh] overflow-y-auto">
        <Card
          gradient="bg-gradient-to-br from-amber-500/10 to-orange-500/5"
          border-class="border-amber-500/30"
        >
          <!-- Header -->
          <div class="flex items-center justify-between mb-4">
            <div class="flex items-center gap-2">
              <i class="fas fa-trophy text-amber-400" />
              <h3 class="text-lg font-semibold text-amber-100">Top Observations</h3>
            </div>
            <button
              @click="emit('close')"
              class="p-1.5 text-slate-400 hover:text-slate-200 hover:bg-slate-700/50 rounded-lg transition-colors"
            >
              <i class="fas fa-times" />
            </button>
          </div>

          <!-- Tabs -->
          <div class="flex gap-2 mb-4">
            <button
              @click="activeTab = 'top'"
              :class="[
                'flex-1 py-2 px-4 text-sm font-medium rounded-lg transition-colors',
                activeTab === 'top'
                  ? 'bg-amber-500/20 text-amber-300 border border-amber-500/30'
                  : 'text-slate-400 hover:text-slate-300 hover:bg-slate-700/50'
              ]"
            >
              <i class="fas fa-star mr-2" />
              Highest Scored
            </button>
            <button
              @click="activeTab = 'retrieved'"
              :class="[
                'flex-1 py-2 px-4 text-sm font-medium rounded-lg transition-colors',
                activeTab === 'retrieved'
                  ? 'bg-cyan-500/20 text-cyan-300 border border-cyan-500/30'
                  : 'text-slate-400 hover:text-slate-300 hover:bg-slate-700/50'
              ]"
            >
              <i class="fas fa-search mr-2" />
              Most Retrieved
            </button>
          </div>

          <!-- Project Filter Indicator -->
          <div v-if="currentProject" class="flex items-center gap-2 mb-4 text-xs text-slate-500">
            <i class="fas fa-filter" />
            <span>Filtered by:</span>
            <span class="text-amber-600/80 font-mono">{{ currentProject.split('/').pop() }}</span>
          </div>

          <!-- Loading State -->
          <div v-if="loading" class="flex items-center justify-center py-8">
            <i class="fas fa-circle-notch fa-spin text-2xl text-amber-400" />
          </div>

          <!-- Error State -->
          <div v-else-if="error" class="text-center py-8">
            <i class="fas fa-exclamation-triangle text-2xl text-red-400 mb-2" />
            <p class="text-red-300">{{ error }}</p>
          </div>

          <!-- Empty State -->
          <div v-else-if="currentObservations().length === 0" class="text-center py-8">
            <i class="fas fa-inbox text-2xl text-slate-500 mb-2" />
            <p class="text-slate-400">No observations found</p>
          </div>

          <!-- Content -->
          <div v-else class="space-y-2">
            <div
              v-for="(obs, index) in currentObservations()"
              :key="obs.id"
              @click="emit('navigateToObservation', obs.id); emit('close')"
              class="flex items-center gap-3 p-3 bg-slate-800/30 hover:bg-slate-800/50 rounded-lg cursor-pointer transition-colors group"
            >
              <!-- Rank -->
              <div
                class="w-7 h-7 rounded-full flex items-center justify-center text-xs font-bold"
                :class="[
                  index < 3 ? 'bg-amber-500/30 text-amber-300' : 'bg-slate-700/50 text-slate-400'
                ]"
              >
                {{ index + 1 }}
              </div>

              <!-- Type Icon -->
              <div
                class="w-8 h-8 rounded-lg flex items-center justify-center"
                :class="getTypeConfig(obs.type).bgClass"
              >
                <i
                  class="fas text-sm"
                  :class="[getTypeConfig(obs.type).icon, getTypeConfig(obs.type).colorClass]"
                />
              </div>

              <!-- Title & Meta -->
              <div class="flex-1 min-w-0">
                <div class="text-sm font-medium text-slate-200 truncate group-hover:text-amber-200 transition-colors">
                  {{ obs.title || 'Untitled' }}
                </div>
                <div class="flex items-center gap-2 text-xs text-slate-500">
                  <span class="capitalize">{{ obs.type }}</span>
                  <span v-if="obs.project" class="text-amber-600/70 font-mono">{{ obs.project.split('/').pop() }}</span>
                </div>
              </div>

              <!-- Score / Retrieval Count -->
              <div class="text-right flex-shrink-0">
                <div
                  v-if="activeTab === 'top'"
                  class="text-sm font-mono font-bold"
                  :class="obs.importance_score && obs.importance_score >= 1.5 ? 'text-green-400' : obs.importance_score && obs.importance_score >= 1 ? 'text-amber-400' : 'text-slate-400'"
                >
                  {{ formatScore(obs.importance_score || 1) }}
                </div>
                <div
                  v-else
                  class="text-sm font-mono font-bold text-cyan-400"
                >
                  {{ obs.retrieval_count || 0 }}×
                </div>
                <div class="text-xs text-slate-500">
                  {{ activeTab === 'top' ? 'score' : 'retrieved' }}
                </div>
              </div>

              <!-- Arrow -->
              <i class="fas fa-chevron-right text-slate-600 group-hover:text-slate-400 transition-colors" />
            </div>
          </div>

          <!-- Refresh Button -->
          <button
            @click="loadData"
            :disabled="loading"
            class="w-full mt-4 py-2 text-sm text-amber-400 hover:text-amber-300 bg-amber-500/10 hover:bg-amber-500/20 rounded-lg transition-colors flex items-center justify-center gap-2"
          >
            <i class="fas fa-sync-alt" :class="{ 'fa-spin': loading }" />
            Refresh
          </button>
        </Card>
      </div>
    </div>
  </Teleport>
</template>
