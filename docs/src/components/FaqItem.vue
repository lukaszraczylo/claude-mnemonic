<template>
  <div class="border-b border-neutral-800 last:border-0">
    <button @click="open = !open" class="w-full text-left flex items-center justify-between py-4">
      <span class="text-white text-sm font-medium pr-4">{{ question }}</span>
      <i :class="open ? 'fas fa-minus' : 'fas fa-plus'" class="text-neutral-600 text-xs flex-shrink-0"></i>
    </button>
    <div v-if="open" class="pb-4">
      <p class="text-neutral-500 text-sm leading-relaxed">{{ answer }}</p>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'

defineProps({
  question: { type: String, required: true },
  answer: { type: String, required: true },
})

const open = ref(false)
</script>
