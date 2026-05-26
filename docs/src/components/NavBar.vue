<template>
  <nav class="border-b border-neutral-800/80 sticky top-0 z-50 bg-[#09090b]/90 backdrop-blur-sm">
    <div class="max-w-5xl mx-auto px-4 sm:px-6 h-14 flex justify-between items-center">
      <a href="#" class="text-white font-bold text-base tracking-tight">Claude Mnemonic</a>

      <ul class="hidden md:flex items-center gap-6">
        <li v-for="link in links" :key="link.href">
          <a :href="link.href" class="text-neutral-500 hover:text-white text-sm transition-colors">{{ link.label }}</a>
        </li>
      </ul>

      <div class="flex items-center gap-4">
        <a :href="githubUrl" target="_blank" class="text-neutral-500 hover:text-white transition-colors text-lg">
          <i class="fab fa-github"></i>
        </a>
        <a href="#install" class="hidden sm:inline-flex items-center gap-2 text-sm font-medium text-amber-500 hover:text-amber-400 transition-colors">
          Install <i class="fas fa-arrow-right text-xs"></i>
        </a>
        <button @click="$emit('toggle-menu')" class="md:hidden text-neutral-500 hover:text-white p-1">
          <i :class="mobileMenuOpen ? 'fas fa-times' : 'fas fa-bars'"></i>
        </button>
      </div>
    </div>

    <div v-if="mobileMenuOpen" class="md:hidden border-t border-neutral-800/80 px-6 py-4 space-y-1">
      <a v-for="link in links" :key="link.href" :href="link.href"
         @click="$emit('toggle-menu')"
         class="block py-2 text-neutral-500 hover:text-white text-sm transition-colors">
        {{ link.label }}
      </a>
    </div>
  </nav>
</template>

<script setup>
defineProps({
  links: {
    type: Array,
    default: () => [
      { label: 'Features', href: '#features' },
      { label: 'Install', href: '#install' },
      { label: 'Config', href: '#config' },
      { label: 'FAQ', href: '#faq' },
    ]
  },
  githubUrl: {
    type: String,
    default: 'https://github.com/lukaszraczylo/claude-mnemonic'
  },
  mobileMenuOpen: Boolean
})

defineEmits(['toggle-menu'])
</script>
