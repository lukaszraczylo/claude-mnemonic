<template>
  <footer class="border-t border-slate-800 py-10 sm:py-16 relative">
    <div class="max-w-6xl mx-auto px-4 sm:px-6 flex flex-col md:flex-row justify-between items-center gap-4 sm:gap-6">
      <div class="flex flex-wrap justify-center gap-4 sm:gap-6">
        <a v-for="link in links" :key="link.href" :href="link.href" target="_blank"
           class="text-slate-500 hover:text-white text-xs sm:text-sm transition-colors animated-underline">
          {{ link.label }}
        </a>
      </div>
      <p class="text-slate-600 text-xs sm:text-sm text-center">{{ copyright }}</p>
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
      { label: 'MIT License', href: 'https://github.com/lukaszraczylo/claude-mnemonic/blob/main/LICENSE' },
    ]
  },
  copyright: {
    type: String,
    default: 'Made with care for the Claude Code community'
  }
})
</script>
