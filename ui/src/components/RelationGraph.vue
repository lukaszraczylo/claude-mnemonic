<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, computed } from 'vue'
import { Network, type Data, type Options } from 'vis-network'
import type { RelationGraph, RelationWithDetails } from '@/types'
import { RELATION_TYPE_CONFIG, DETECTION_SOURCE_CONFIG } from '@/types/relation'
import { fetchObservationGraph } from '@/utils/api'

const props = defineProps<{
  observationId: number
  observationTitle: string
  show: boolean
}>()

const emit = defineEmits<{
  close: []
  navigateTo: [id: number]
}>()

const graphContainer = ref<HTMLElement | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const graphData = ref<RelationGraph | null>(null)
const selectedRelation = ref<RelationWithDetails | null>(null)
const depth = ref(2)

let network: Network | null = null

// Node colors based on observation type
const getNodeColor = (type: string) => {
  const colors: Record<string, { background: string; border: string; highlight: { background: string; border: string } }> = {
    bugfix: { background: '#ef4444', border: '#dc2626', highlight: { background: '#f87171', border: '#ef4444' } },
    feature: { background: '#a855f7', border: '#9333ea', highlight: { background: '#c084fc', border: '#a855f7' } },
    refactor: { background: '#3b82f6', border: '#2563eb', highlight: { background: '#60a5fa', border: '#3b82f6' } },
    discovery: { background: '#06b6d4', border: '#0891b2', highlight: { background: '#22d3ee', border: '#06b6d4' } },
    decision: { background: '#eab308', border: '#ca8a04', highlight: { background: '#facc15', border: '#eab308' } },
    change: { background: '#64748b', border: '#475569', highlight: { background: '#94a3b8', border: '#64748b' } },
  }
  return colors[type] || colors.change
}

// Edge colors based on relation type
const getEdgeColor = (type: string) => {
  const colors: Record<string, string> = {
    causes: '#f97316',
    fixes: '#22c55e',
    supersedes: '#a855f7',
    depends_on: '#3b82f6',
    relates_to: '#64748b',
    evolves_from: '#06b6d4',
  }
  return colors[type] || '#64748b'
}

const loadGraph = async () => {
  loading.value = true
  error.value = null

  try {
    graphData.value = await fetchObservationGraph(props.observationId, depth.value)
    renderGraph()
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load graph'
  } finally {
    loading.value = false
  }
}

const renderGraph = () => {
  if (!graphContainer.value || !graphData.value) return

  // Build nodes and edges from relations
  const nodeMap = new Map<number, { id: number; label: string; type: string }>()
  const edgesList: { from: number; to: number; label: string; color: string; arrows: string; relation: RelationWithDetails }[] = []

  // Add center node
  nodeMap.set(props.observationId, {
    id: props.observationId,
    label: truncateLabel(props.observationTitle),
    type: 'center'
  })

  // Process relations
  for (const rel of graphData.value.relations) {
    // Add source node
    if (!nodeMap.has(rel.relation.source_id)) {
      nodeMap.set(rel.relation.source_id, {
        id: rel.relation.source_id,
        label: truncateLabel(rel.source_title),
        type: rel.source_type
      })
    }

    // Add target node
    if (!nodeMap.has(rel.relation.target_id)) {
      nodeMap.set(rel.relation.target_id, {
        id: rel.relation.target_id,
        label: truncateLabel(rel.target_title),
        type: rel.target_type
      })
    }

    // Add edge
    edgesList.push({
      from: rel.relation.source_id,
      to: rel.relation.target_id,
      label: rel.relation.relation_type.replace('_', ' '),
      color: getEdgeColor(rel.relation.relation_type),
      arrows: 'to',
      relation: rel
    })
  }

  // Create vis-network data using plain arrays (simpler type compatibility)
  const nodes = Array.from(nodeMap.values()).map(node => ({
    id: node.id,
    label: node.label,
    color: node.id === props.observationId
      ? { background: '#f59e0b', border: '#d97706', highlight: { background: '#fbbf24', border: '#f59e0b' } }
      : getNodeColor(node.type),
    font: { color: '#fff', size: 12 },
    shape: 'box' as const,
    borderWidth: node.id === props.observationId ? 3 : 2,
    margin: { top: 10, right: 10, bottom: 10, left: 10 },
    shadow: true
  }))

  const edges = edgesList.map((edge, index) => ({
    id: index,
    from: edge.from,
    to: edge.to,
    label: edge.label,
    color: { color: edge.color, highlight: edge.color },
    font: { color: '#94a3b8', size: 10, strokeWidth: 0 },
    arrows: edge.arrows,
    width: 2,
    smooth: { enabled: true, type: 'curvedCW' as const, roundness: 0.2 }
  }))

  // Cleanup existing network
  if (network) {
    network.destroy()
  }

  // Create network data
  const data: Data = { nodes, edges }

  const options: Options = {
    physics: {
      enabled: true,
      solver: 'forceAtlas2Based',
      forceAtlas2Based: {
        gravitationalConstant: -50,
        centralGravity: 0.01,
        springLength: 150,
        springConstant: 0.08
      },
      stabilization: { iterations: 100 }
    },
    interaction: {
      hover: true,
      tooltipDelay: 200,
      zoomView: true,
      dragView: true
    },
    layout: {
      improvedLayout: true
    }
  }

  // Create network
  network = new Network(graphContainer.value, data, options)

  // Handle edge click to show details
  network.on('selectEdge', (params: { edges: (string | number)[] }) => {
    if (params.edges.length > 0) {
      const edgeId = params.edges[0] as number
      selectedRelation.value = edgesList[edgeId]?.relation || null
    }
  })

  // Handle node double-click to navigate
  network.on('doubleClick', (params: { nodes: (string | number)[] }) => {
    if (params.nodes.length > 0) {
      const nodeId = params.nodes[0] as number
      if (nodeId !== props.observationId) {
        emit('navigateTo', nodeId)
      }
    }
  })

  // Clear selection when clicking background
  network.on('click', (params: { nodes: (string | number)[]; edges: (string | number)[] }) => {
    if (params.nodes.length === 0 && params.edges.length === 0) {
      selectedRelation.value = null
    }
  })
}

const truncateLabel = (label: string, maxLen = 30) => {
  if (label.length <= maxLen) return label
  return label.substring(0, maxLen - 3) + '...'
}

const relationCount = computed(() => graphData.value?.relations.length || 0)

const closeModal = () => {
  emit('close')
}

// Watch for show prop changes
watch(() => props.show, (newVal) => {
  if (newVal) {
    loadGraph()
  } else {
    selectedRelation.value = null
    if (network) {
      network.destroy()
      network = null
    }
  }
})

// Watch for depth changes
watch(depth, () => {
  if (props.show) {
    loadGraph()
  }
})

onMounted(() => {
  if (props.show) {
    loadGraph()
  }
})

onUnmounted(() => {
  if (network) {
    network.destroy()
    network = null
  }
})
</script>

<template>
  <!-- Modal backdrop -->
  <Teleport to="body">
    <div
      v-if="show"
      class="fixed inset-0 z-50 flex items-center justify-center p-4"
      @click.self="closeModal"
    >
      <!-- Backdrop -->
      <div class="absolute inset-0 bg-black/70 backdrop-blur-sm" @click="closeModal" />

      <!-- Modal content -->
      <div class="relative bg-slate-900 border border-slate-700 rounded-xl shadow-2xl w-full max-w-5xl max-h-[90vh] flex flex-col overflow-hidden">
        <!-- Header -->
        <div class="flex items-center justify-between p-4 border-b border-slate-700">
          <div class="flex items-center gap-3">
            <div class="p-2 rounded-lg bg-amber-500/20">
              <i class="fas fa-diagram-project text-amber-400" />
            </div>
            <div>
              <h2 class="text-lg font-semibold text-amber-100">Knowledge Graph</h2>
              <p class="text-sm text-slate-400">
                {{ relationCount }} relation{{ relationCount !== 1 ? 's' : '' }} for "{{ truncateLabel(observationTitle, 50) }}"
              </p>
            </div>
          </div>

          <!-- Controls -->
          <div class="flex items-center gap-4">
            <!-- Depth selector -->
            <div class="flex items-center gap-2">
              <label class="text-xs text-slate-400">Depth:</label>
              <select
                v-model="depth"
                class="bg-slate-800 border border-slate-600 rounded px-2 py-1 text-sm text-slate-200 focus:outline-none focus:border-amber-500"
              >
                <option :value="1">1</option>
                <option :value="2">2</option>
                <option :value="3">3</option>
              </select>
            </div>

            <!-- Close button -->
            <button
              @click="closeModal"
              class="p-2 rounded-lg text-slate-400 hover:text-slate-200 hover:bg-slate-800 transition-colors"
            >
              <i class="fas fa-times" />
            </button>
          </div>
        </div>

        <!-- Graph container -->
        <div class="relative" style="height: 60vh; min-height: 500px;">
          <!-- Loading state -->
          <div v-if="loading" class="absolute inset-0 flex items-center justify-center bg-slate-900/50">
            <div class="flex items-center gap-3 text-amber-400">
              <i class="fas fa-circle-notch fa-spin text-xl" />
              <span>Loading graph...</span>
            </div>
          </div>

          <!-- Error state -->
          <div v-else-if="error" class="absolute inset-0 flex items-center justify-center">
            <div class="text-center">
              <i class="fas fa-exclamation-triangle text-3xl text-red-400 mb-2" />
              <p class="text-red-300">{{ error }}</p>
              <button
                @click="loadGraph"
                class="mt-3 px-4 py-2 bg-slate-800 hover:bg-slate-700 rounded-lg text-sm text-slate-200 transition-colors"
              >
                Retry
              </button>
            </div>
          </div>

          <!-- Empty state -->
          <div v-else-if="graphData && graphData.relations.length === 0" class="absolute inset-0 flex items-center justify-center">
            <div class="text-center">
              <i class="fas fa-diagram-project text-4xl text-slate-600 mb-3" />
              <p class="text-slate-400">No relations found for this observation</p>
              <p class="text-sm text-slate-500 mt-1">Relations are detected automatically when observations share files, concepts, or patterns</p>
            </div>
          </div>

          <!-- Graph -->
          <div ref="graphContainer" class="absolute inset-0" />
        </div>

        <!-- Relation details panel -->
        <div v-if="selectedRelation" class="border-t border-slate-700 p-4 bg-slate-800/50">
          <div class="flex items-start gap-4">
            <!-- Relation type icon -->
            <div
              class="p-3 rounded-lg"
              :class="RELATION_TYPE_CONFIG[selectedRelation.relation.relation_type]?.bgClass"
            >
              <i
                class="fas"
                :class="[
                  RELATION_TYPE_CONFIG[selectedRelation.relation.relation_type]?.icon,
                  RELATION_TYPE_CONFIG[selectedRelation.relation.relation_type]?.colorClass
                ]"
              />
            </div>

            <!-- Details -->
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2 mb-1">
                <span class="font-medium" :class="RELATION_TYPE_CONFIG[selectedRelation.relation.relation_type]?.colorClass">
                  {{ RELATION_TYPE_CONFIG[selectedRelation.relation.relation_type]?.label }}
                </span>
                <span class="text-xs text-slate-500">
                  ({{ Math.round(selectedRelation.relation.confidence * 100) }}% confidence)
                </span>
              </div>

              <div class="flex items-center gap-2 text-sm text-slate-300 mb-2">
                <span class="font-mono text-amber-400">{{ truncateLabel(selectedRelation.source_title, 40) }}</span>
                <i class="fas fa-arrow-right text-slate-500 text-xs" />
                <span class="font-mono text-amber-400">{{ truncateLabel(selectedRelation.target_title, 40) }}</span>
              </div>

              <div class="flex items-center gap-4 text-xs text-slate-500">
                <span class="flex items-center gap-1">
                  <i :class="['fas', DETECTION_SOURCE_CONFIG[selectedRelation.relation.detection_source]?.icon]" />
                  {{ DETECTION_SOURCE_CONFIG[selectedRelation.relation.detection_source]?.label }}
                </span>
                <span v-if="selectedRelation.relation.reason">
                  {{ selectedRelation.relation.reason }}
                </span>
              </div>
            </div>
          </div>
        </div>

        <!-- Legend -->
        <div class="border-t border-slate-700 p-3 bg-slate-800/30">
          <div class="flex flex-wrap items-center justify-center gap-4 text-xs text-slate-400">
            <span class="font-medium text-slate-300">Legend:</span>
            <span class="flex items-center gap-1">
              <span class="w-3 h-3 rounded bg-amber-500" /> Center
            </span>
            <span class="flex items-center gap-1">
              <span class="w-3 h-3 rounded bg-red-500" /> Bugfix
            </span>
            <span class="flex items-center gap-1">
              <span class="w-3 h-3 rounded bg-purple-500" /> Feature
            </span>
            <span class="flex items-center gap-1">
              <span class="w-3 h-3 rounded bg-blue-500" /> Refactor
            </span>
            <span class="flex items-center gap-1">
              <span class="w-3 h-3 rounded bg-cyan-500" /> Discovery
            </span>
            <span class="flex items-center gap-1">
              <span class="w-3 h-3 rounded bg-yellow-500" /> Decision
            </span>
            <span class="text-slate-500">|</span>
            <span>Double-click node to navigate</span>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
