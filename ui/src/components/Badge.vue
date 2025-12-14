<script setup lang="ts">
defineProps<{
  icon?: string
  colorClass?: string
  bgClass?: string
  borderClass?: string
}>()
</script>

<template>
  <span
    class="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-medium rounded-full border"
    :class="[bgClass || 'bg-slate-500/20', colorClass || 'text-slate-300', borderClass || 'border-slate-500/40']"
  >
    <i v-if="icon" class="fas" :class="icon" />
    <slot />
  </span>
</template>
