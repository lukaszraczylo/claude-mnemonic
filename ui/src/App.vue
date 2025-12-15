<script setup lang="ts">
import { watch } from 'vue'
import { useSSE, useStats, useTimeline, useUpdate, useHealth } from '@/composables'
import Header from '@/components/Header.vue'
import StatsCards from '@/components/StatsCards.vue'
import FilterTabs from '@/components/FilterTabs.vue'
import Timeline from '@/components/Timeline.vue'
import Sidebar from '@/components/Sidebar.vue'

// Composables
const { isConnected, isProcessing, queueDepth, lastEvent } = useSSE()
const { stats } = useStats()
const { updateInfo, updateStatus, isUpdating, applyUpdate } = useUpdate()
const { health } = useHealth()
const {
  filteredItems,
  loading,
  observationCount,
  promptCount,
  summaryCount,
  currentFilter,
  currentProject,
  currentTypeFilter,
  currentConceptFilter,
  refresh,
  setFilter,
  setProject,
  setTypeFilter,
  setConceptFilter
} = useTimeline()

// Refresh timeline when new events arrive
watch(lastEvent, (event) => {
  if (event && (event.type === 'observation' || event.type === 'prompt')) {
    refresh()
  }
})
</script>

<template>
  <div class="min-h-screen">
    <!-- Header -->
    <Header
      :is-connected="isConnected"
      :is-processing="isProcessing"
      :update-info="updateInfo"
      :update-status="updateStatus"
      :is-updating="isUpdating"
      @refresh="refresh"
      @apply-update="applyUpdate"
    />

    <!-- Main Content -->
    <main class="max-w-7xl mx-auto px-4 py-6">
      <!-- Stats Cards -->
      <StatsCards
        :stats="stats"
        :queue-depth="queueDepth"
      />

      <!-- Two Column Layout -->
      <div class="flex gap-6">
        <!-- Sidebar -->
        <Sidebar
          :stats="stats"
          :observation-count="observationCount"
          :prompt-count="promptCount"
          :summary-count="summaryCount"
          :current-project="currentProject"
          :health="health"
          @update:project="setProject"
        />

        <!-- Activity Timeline Section -->
        <section class="flex-1 min-w-0">
          <div class="flex items-center gap-3 mb-4">
            <i class="fas fa-list text-claude-400" />
            <h2 class="text-lg font-semibold text-white">Activity Timeline</h2>
          </div>

          <!-- Filter Tabs -->
          <FilterTabs
            :current-filter="currentFilter"
            :current-type-filter="currentTypeFilter"
            :current-concept-filter="currentConceptFilter"
            :observation-count="observationCount"
            :prompt-count="promptCount"
            @update:filter="setFilter"
            @update:type-filter="setTypeFilter"
            @update:concept-filter="setConceptFilter"
          />

          <!-- Timeline -->
          <Timeline
            :items="filteredItems"
            :loading="loading"
          />
        </section>
      </div>
    </main>
  </div>
</template>
