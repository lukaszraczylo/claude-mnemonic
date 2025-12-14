<template>
  <div class="glass rounded-xl p-4 sm:p-6 hover:border-amber-500/20 transition-colors">
    <h3 class="text-white font-semibold text-sm sm:text-base mb-2 sm:mb-3">{{ question }}</h3>
    <p class="text-slate-400 text-xs sm:text-sm leading-relaxed">{{ answer }}</p>
  </div>
</template>

<script setup>
defineProps({
  question: {
    type: String,
    required: true
  },
  answer: {
    type: String,
    required: true
  }
})
</script>
