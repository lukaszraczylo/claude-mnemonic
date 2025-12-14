<template>
  <div class="flex items-start sm:items-center gap-3 sm:gap-4 p-3 sm:p-4 bg-slate-800/50 rounded-lg border-l-4 border-amber-500">
    <div class="w-8 h-8 sm:w-10 sm:h-10 bg-amber-500/15 rounded-lg flex items-center justify-center text-amber-500 flex-shrink-0 text-sm sm:text-base">
      <i :class="icon"></i>
    </div>
    <div class="min-w-0">
      <strong class="block text-white text-sm sm:text-base mb-0.5 sm:mb-1">{{ title }}</strong>
      <span class="text-slate-500 text-xs sm:text-sm block break-words">{{ description }}</span>
    </div>
  </div>
</template>

<script setup>
defineProps({
  icon: {
    type: String,
    required: true
  },
  title: {
    type: String,
    required: true
  },
  description: {
    type: String,
    required: true
  }
})
</script>
