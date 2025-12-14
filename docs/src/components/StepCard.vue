<template>
  <div class="text-center relative">
    <div class="w-14 h-14 sm:w-20 sm:h-20 bg-slate-900 border-2 border-amber-500 rounded-full flex items-center justify-center mx-auto mb-4 sm:mb-6 text-amber-500 font-bold text-xl sm:text-3xl relative z-10">
      {{ number }}
    </div>
    <h3 class="text-white font-semibold text-base sm:text-lg mb-2 sm:mb-3">{{ title }}</h3>
    <p class="text-slate-400 text-xs sm:text-sm">{{ description }}</p>
  </div>
</template>

<script setup>
defineProps({
  number: {
    type: [String, Number],
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
