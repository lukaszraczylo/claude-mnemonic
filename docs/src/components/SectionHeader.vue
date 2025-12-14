<template>
  <div class="text-center mb-10 sm:mb-16">
    <h2 class="text-2xl sm:text-3xl md:text-4xl lg:text-5xl font-bold text-white mb-3 sm:mb-4">
      {{ title }}
    </h2>
    <p v-if="subtitle" class="text-sm sm:text-base md:text-lg text-slate-400 max-w-xl mx-auto px-2">
      {{ subtitle }}
    </p>
  </div>
</template>

<script setup>
defineProps({
  title: {
    type: String,
    required: true
  },
  subtitle: String
})
</script>
