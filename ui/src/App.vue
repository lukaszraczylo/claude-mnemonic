<script setup lang="ts">
import { useSSE, useStats, useTimeline, useUpdate, useHealth } from '@/composables'
import Header from '@/components/Header.vue'
import StatsCards from '@/components/StatsCards.vue'
import FilterTabs from '@/components/FilterTabs.vue'
import Timeline from '@/components/Timeline.vue'
import Sidebar from '@/components/Sidebar.vue'

// Composables
const { isConnected, isReconnecting, reconnectCountdown, isProcessing, queueDepth } = useSSE()
const { updateInfo, updateStatus, isUpdating, applyUpdate } = useUpdate()
const { health } = useHealth()
// Initialize useTimeline first to get currentProject ref
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
// Pass currentProject ref to useStats for project-specific retrieval stats
const { stats } = useStats(currentProject)

// Note: Feedback is handled directly in ObservationCard component
</script>

<template>
  <div class="min-h-screen">
    <!-- Reconnection Banner -->
    <Transition name="slide">
      <div
        v-if="isReconnecting"
        class="fixed top-0 left-0 right-0 z-50 bg-amber-500/90 backdrop-blur-sm text-black px-4 py-2 text-center text-sm font-medium shadow-lg"
      >
        <i class="fas fa-sync-alt fa-spin mr-2" />
        Connection lost. Reconnecting<span v-if="reconnectCountdown > 0"> in {{ reconnectCountdown }}s</span>...
      </div>
    </Transition>

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

<style scoped>
.slide-enter-active,
.slide-leave-active {
  transition: transform 0.3s ease, opacity 0.3s ease;
}

.slide-enter-from,
.slide-leave-to {
  transform: translateY(-100%);
  opacity: 0;
}
</style>
