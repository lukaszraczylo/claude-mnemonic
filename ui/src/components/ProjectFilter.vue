<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { fetchProjects } from '@/utils/api'

const props = defineProps<{
  currentProject: string | null
}>()

const emit = defineEmits<{
  'update:project': [project: string | null]
}>()

const projects = ref<string[]>([])
const searchQuery = ref('')
const isOpen = ref(false)
const loading = ref(false)

const filteredProjects = computed(() => {
  if (!searchQuery.value) return projects.value
  const query = searchQuery.value.toLowerCase()
  return projects.value.filter(p => p.toLowerCase().includes(query))
})

const selectedProjectName = computed(() => {
  if (!props.currentProject) return 'All Projects'
  // Extract just the directory name from the full path
  const parts = props.currentProject.split('/')
  return parts[parts.length - 1] || props.currentProject
})

async function loadProjects() {
  loading.value = true
  try {
    projects.value = await fetchProjects()
  } catch (err) {
    console.error('[ProjectFilter] Failed to load projects:', err)
  } finally {
    loading.value = false
  }
}

function selectProject(project: string | null) {
  emit('update:project', project)
  isOpen.value = false
  searchQuery.value = ''
}

function toggleDropdown() {
  isOpen.value = !isOpen.value
  if (isOpen.value) {
    loadProjects()
  }
}

// Close dropdown when clicking outside
function handleClickOutside(event: MouseEvent) {
  const target = event.target as HTMLElement
  if (!target.closest('.project-filter')) {
    isOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  loadProjects()
})
</script>

<template>
  <div class="project-filter relative">
    <!-- Trigger Button -->
    <button
      @click="toggleDropdown"
      class="flex items-center gap-2 px-4 py-2 bg-slate-800 hover:bg-slate-700 border border-slate-700 rounded-lg text-sm text-slate-200 transition-colors w-full"
    >
      <i class="fas fa-folder text-claude-400" />
      <span class="truncate flex-1 text-left">{{ selectedProjectName }}</span>
      <i
        class="fas fa-chevron-down text-slate-500 transition-transform"
        :class="{ 'rotate-180': isOpen }"
      />
    </button>

    <!-- Dropdown -->
    <div
      v-if="isOpen"
      class="absolute z-50 w-full mt-2 bg-slate-800 border border-slate-700 rounded-lg shadow-xl overflow-hidden"
    >
      <!-- Search Input -->
      <div class="p-2 border-b border-slate-700">
        <div class="relative">
          <i class="fas fa-search absolute left-3 top-1/2 -translate-y-1/2 text-slate-500 text-sm" />
          <input
            v-model="searchQuery"
            type="text"
            placeholder="Search projects..."
            class="w-full pl-9 pr-3 py-2 bg-slate-900 border border-slate-600 rounded text-sm text-slate-200 placeholder-slate-500 focus:outline-none focus:border-claude-500"
          />
        </div>
      </div>

      <!-- Project List -->
      <div class="max-h-64 overflow-y-auto">
        <!-- All Projects Option -->
        <button
          @click="selectProject(null)"
          class="w-full px-4 py-2 text-left text-sm hover:bg-slate-700 transition-colors flex items-center gap-2"
          :class="{ 'bg-slate-700 text-claude-400': !currentProject, 'text-slate-300': currentProject }"
        >
          <i class="fas fa-globe text-slate-500" />
          <span>All Projects</span>
          <i v-if="!currentProject" class="fas fa-check ml-auto text-claude-400" />
        </button>

        <!-- Loading State -->
        <div v-if="loading" class="px-4 py-3 text-slate-500 text-sm text-center">
          <i class="fas fa-spinner fa-spin mr-2" />
          Loading projects...
        </div>

        <!-- No Results -->
        <div v-else-if="filteredProjects.length === 0" class="px-4 py-3 text-slate-500 text-sm text-center">
          No projects found
        </div>

        <!-- Project Items -->
        <button
          v-else
          v-for="project in filteredProjects"
          :key="project"
          @click="selectProject(project)"
          class="w-full px-4 py-2 text-left text-sm hover:bg-slate-700 transition-colors flex items-center gap-2"
          :class="{ 'bg-slate-700 text-claude-400': currentProject === project, 'text-slate-300': currentProject !== project }"
        >
          <i class="fas fa-folder text-slate-500" />
          <span class="truncate">{{ project.split('/').pop() }}</span>
          <i v-if="currentProject === project" class="fas fa-check ml-auto text-claude-400" />
        </button>
      </div>
    </div>
  </div>
</template>
