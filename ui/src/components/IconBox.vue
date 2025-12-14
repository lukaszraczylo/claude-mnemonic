<script setup lang="ts">
defineProps<{
  icon: string
  gradient?: string
  size?: 'sm' | 'md' | 'lg'
}>()
</script>

<template>
  <div
    class="flex items-center justify-center rounded-xl bg-gradient-to-br shadow-lg flex-shrink-0"
    :class="[
      gradient || 'from-claude-500 to-claude-700',
      {
        'w-8 h-8': size === 'sm',
        'w-12 h-12': size === 'md' || !size,
        'w-16 h-16': size === 'lg'
      }
    ]"
  >
    <i
      class="fas text-white"
      :class="[
        icon,
        {
          'text-base': size === 'sm',
          'text-2xl': size === 'md' || !size,
          'text-3xl': size === 'lg'
        }
      ]"
    />
  </div>
</template>
