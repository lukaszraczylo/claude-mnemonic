<template>
  <div class="p-4 rounded-lg border border-neutral-800">
    <div class="text-white font-semibold text-sm mb-1">{{ name }}</div>
    <p class="text-neutral-500 text-xs leading-relaxed">{{ description }}</p>
  </div>
</template>

<script setup>
defineProps({
  name: { type: String, required: true },
  description: { type: String, required: true },
})
</script>
