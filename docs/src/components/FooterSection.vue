<template>
  <footer class="border-t border-neutral-800/80 py-8 px-4 sm:px-6">
    <div class="max-w-5xl mx-auto flex flex-col sm:flex-row justify-between items-center gap-4">
      <p class="text-neutral-600 text-xs">MIT License</p>
      <div class="flex gap-5">
        <a v-for="link in links" :key="link.href" :href="link.href" target="_blank"
           class="text-neutral-600 hover:text-neutral-400 text-xs transition-colors">
          {{ link.label }}
        </a>
      </div>
    </div>
  </footer>
</template>

<script setup>
defineProps({
  links: {
    type: Array,
    default: () => [
      { label: 'GitHub', href: 'https://github.com/lukaszraczylo/claude-mnemonic' },
      { label: 'Releases', href: 'https://github.com/lukaszraczylo/claude-mnemonic/releases' },
      { label: 'Issues', href: 'https://github.com/lukaszraczylo/claude-mnemonic/issues' },
      { label: 'License', href: 'https://github.com/lukaszraczylo/claude-mnemonic/blob/main/LICENSE' },
    ]
  },
})
</script>
