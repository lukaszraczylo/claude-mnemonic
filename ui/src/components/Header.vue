<script setup lang="ts">
defineProps<{
  isConnected: boolean
  isProcessing: boolean
}>()

const emit = defineEmits<{
  refresh: []
}>()
</script>

<template>
  <header class="glass border-b border-white/10 sticky top-0 z-50">
    <div class="max-w-7xl mx-auto px-4 py-4">
      <div class="flex items-center justify-between">
        <!-- Logo & Title -->
        <div class="flex items-center gap-3">
          <div class="w-10 h-10 rounded-xl bg-gradient-to-br from-claude-500 to-claude-700 flex items-center justify-center shadow-lg">
            <i class="fas fa-brain text-xl text-white" />
          </div>
          <div>
            <h1 class="text-xl font-bold text-white">Claude <span class="text-claude-400">Mnemonic</span></h1>
            <p class="text-xs text-slate-400">Persistent Memory System</p>
          </div>
        </div>

        <!-- Status & Actions -->
        <div class="flex items-center gap-4">
          <!-- Connection Status -->
          <div class="flex items-center gap-2">
            <span
              class="w-2 h-2 rounded-full"
              :class="[
                isConnected ? 'bg-green-500' : 'bg-red-500',
                isProcessing ? 'animate-pulse' : ''
              ]"
            />
            <span class="text-sm text-slate-400">
              {{ isConnected ? (isProcessing ? 'Processing' : 'Connected') : 'Disconnected' }}
            </span>
          </div>

          <!-- Refresh Button -->
          <button
            class="p-2 rounded-lg bg-white/5 hover:bg-white/10 transition-colors text-slate-400 hover:text-white"
            title="Refresh"
            @click="emit('refresh')"
          >
            <i class="fas fa-rotate" />
          </button>
        </div>
      </div>
    </div>
  </header>
</template>
