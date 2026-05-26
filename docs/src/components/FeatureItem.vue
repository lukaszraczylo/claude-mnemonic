<template>
  <div class="p-5 rounded-lg border border-neutral-800 bg-neutral-900/30">
    <div class="flex items-center gap-3 mb-2">
      <i :class="[icon, 'text-amber-500 text-sm']"></i>
      <h3 class="text-white font-semibold text-sm">{{ title }}</h3>
    </div>
    <p class="text-neutral-500 text-xs leading-relaxed pl-7">{{ description }}</p>
  </div>
</template>

<script setup>
defineProps({
  icon: { type: String, required: true },
  title: { type: String, required: true },
  description: { type: String, required: true },
})
</script>
