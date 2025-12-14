<script setup lang="ts">
defineProps<{
  gradient?: string
  borderClass?: string
  highlight?: boolean
}>()
</script>

<template>
  <div
    class="p-4 rounded-xl border-2 transition-all"
    :class="[
      gradient || 'bg-gradient-to-br from-slate-800/50 to-slate-900/50',
      borderClass || 'border-slate-700/50',
      highlight ? 'ring-2 ring-claude-500/20' : '',
      'hover:border-opacity-70'
    ]"
  >
    <slot />
  </div>
</template>
