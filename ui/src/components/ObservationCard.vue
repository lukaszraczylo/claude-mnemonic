<script setup lang="ts">
import type { ObservationFeedItem } from '@/types'
import { TYPE_CONFIG, CONCEPT_CONFIG } from '@/types/observation'
import { formatRelativeTime } from '@/utils/formatters'
import Card from './Card.vue'
import IconBox from './IconBox.vue'
import Badge from './Badge.vue'
import { computed } from 'vue'

const props = defineProps<{
  observation: ObservationFeedItem
  highlight?: boolean
}>()

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
      </div>
    </div>
  </Card>
</template>
