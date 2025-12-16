<script setup lang="ts">
import type { FilterType, ObservationType, ConceptType } from '@/types'
import { useTypes } from '@/composables/useTypes'

defineProps<{
  currentFilter: FilterType
  currentTypeFilter: ObservationType | null
  currentConceptFilter: ConceptType | null
  observationCount: number
  promptCount: number
}>()

const emit = defineEmits<{
  'update:filter': [filter: FilterType]
  'update:typeFilter': [type: ObservationType | null]
  'update:conceptFilter': [concept: ConceptType | null]
}>()

// Fetch types from API (cached)
const { observationTypes, conceptTypes } = useTypes()

const tabs: { key: FilterType; label: string; icon: string }[] = [
  { key: 'all', label: 'All', icon: 'fa-layer-group' },
  { key: 'observations', label: 'Observations', icon: 'fa-brain' },
  { key: 'summaries', label: 'Summaries', icon: 'fa-clipboard-list' },
  { key: 'prompts', label: 'Prompts', icon: 'fa-comment' }
]
</script>

<template>
  <div class="glass rounded-xl p-4 mb-4 border border-white/10">
    <!-- Main Filter Tabs -->
    <div class="flex items-center gap-2 flex-wrap">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="px-3 py-1.5 rounded-lg text-sm font-medium transition-colors"
        :class="[
          currentFilter === tab.key
            ? 'bg-claude-500 text-white'
            : 'bg-white/5 text-slate-400 hover:bg-white/10 hover:text-white'
        ]"
        @click="emit('update:filter', tab.key)"
      >
        <i class="fas mr-1.5" :class="tab.icon" />
        {{ tab.label }}
      </button>

      <!-- Stats -->
      <div class="ml-auto flex items-center gap-3 text-xs text-slate-500">
        <span>{{ observationCount }} obs</span>
        <span>·</span>
        <span>{{ promptCount }} prompts</span>
      </div>
    </div>

    <!-- Sub-filters (when observations selected) -->
    <div v-if="currentFilter === 'observations' || currentFilter === 'all'" class="mt-3 pt-3 border-t border-white/10">
      <div class="flex items-center gap-3 flex-wrap">
        <!-- Type Filter -->
        <div class="flex items-center gap-1">
          <span class="text-xs text-slate-500 mr-1">Type:</span>
          <select
            class="bg-white/5 border border-white/10 rounded px-2 py-1 text-xs text-slate-300 focus:outline-none focus:border-claude-500"
            :value="currentTypeFilter || ''"
            @change="emit('update:typeFilter', ($event.target as HTMLSelectElement).value as ObservationType || null)"
          >
            <option value="">All Types</option>
            <option v-for="type in observationTypes" :key="type" :value="type">
              {{ type }}
            </option>
          </select>
        </div>

        <!-- Concept Filter -->
        <div class="flex items-center gap-1">
          <span class="text-xs text-slate-500 mr-1">Concept:</span>
          <select
            class="bg-white/5 border border-white/10 rounded px-2 py-1 text-xs text-slate-300 focus:outline-none focus:border-claude-500"
            :value="currentConceptFilter || ''"
            @change="emit('update:conceptFilter', ($event.target as HTMLSelectElement).value as ConceptType || null)"
          >
            <option value="">All Concepts</option>
            <option v-for="concept in conceptTypes" :key="concept" :value="concept">
              {{ concept }}
            </option>
          </select>
        </div>
      </div>
    </div>
  </div>
</template>
