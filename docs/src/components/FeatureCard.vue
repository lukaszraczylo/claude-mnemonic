<template>
  <div class="glass rounded-2xl p-5 sm:p-8 relative overflow-hidden group hover:border-amber-500/30 hover:-translate-y-1 transition-all duration-300">
    <div class="absolute top-0 left-0 right-0 h-0.5 bg-gradient-to-r from-amber-500 to-transparent opacity-0 group-hover:opacity-100 transition-opacity"></div>
    <div class="w-10 h-10 sm:w-12 sm:h-12 bg-amber-500/15 rounded-xl flex items-center justify-center mb-4 sm:mb-5 text-amber-500 text-lg sm:text-xl">
      <i :class="icon"></i>
    </div>
    <h3 class="text-white font-semibold text-base sm:text-lg mb-2 sm:mb-3">{{ title }}</h3>
    <p class="text-slate-400 text-xs sm:text-sm leading-relaxed">{{ description }}</p>
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
