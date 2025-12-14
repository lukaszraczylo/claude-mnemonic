<template>
  <nav class="fixed top-0 left-0 right-0 z-50 bg-gradient-to-b from-slate-950 via-slate-950/95 to-transparent backdrop-blur-md">
    <div class="max-w-6xl mx-auto px-4 sm:px-6 py-4 sm:py-5 flex justify-between items-center">
      <a href="#" class="flex items-center gap-2 sm:gap-3 text-white font-bold text-xl sm:text-2xl">
        <div class="w-8 h-8 sm:w-9 sm:h-9 bg-gradient-to-br from-amber-500 to-amber-400 rounded-lg flex items-center justify-center">
          <i class="fas fa-brain text-slate-950 text-sm sm:text-base"></i>
        </div>
        {{ title }}
      </a>

      <ul class="hidden md:flex gap-6 lg:gap-8">
        <li v-for="link in links" :key="link.href">
          <a :href="link.href" class="text-slate-400 hover:text-white text-sm font-medium transition-colors animated-underline">
            {{ link.label }}
          </a>
        </li>
      </ul>

      <div class="flex items-center gap-3 sm:gap-4">
        <a :href="githubUrl" target="_blank" class="text-slate-400 hover:text-white text-lg sm:text-xl transition-colors">
          <i class="fab fa-github"></i>
        </a>
        <a href="#installation" class="hidden sm:inline-flex bg-gradient-to-br from-amber-500 to-amber-400 text-slate-950 px-4 sm:px-5 py-2 sm:py-2.5 rounded-lg font-semibold text-sm hover:scale-105 hover:shadow-lg hover:shadow-amber-500/30 transition-all">
          Get Started
        </a>
        <button @click="$emit('toggle-menu')" class="md:hidden text-white text-xl p-2">
          <i :class="mobileMenuOpen ? 'fas fa-times' : 'fas fa-bars'"></i>
        </button>
      </div>
    </div>

    <!-- Mobile Menu -->
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0 -translate-y-2"
      enter-to-class="opacity-100 translate-y-0"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100 translate-y-0"
      leave-to-class="opacity-0 -translate-y-2"
    >
      <div v-if="mobileMenuOpen" class="md:hidden bg-slate-900/95 backdrop-blur-xl border-t border-slate-800 px-4 sm:px-6 py-4">
        <a v-for="link in links" :key="link.href" :href="link.href"
           @click="$emit('toggle-menu')"
           class="block py-3 text-slate-400 hover:text-white border-b border-slate-800 last:border-0 transition-colors">
          {{ link.label }}
        </a>
        <a href="#installation" @click="$emit('toggle-menu')"
           class="mt-4 block text-center bg-gradient-to-br from-amber-500 to-amber-400 text-slate-950 px-5 py-2.5 rounded-lg font-semibold text-sm">
          Get Started
        </a>
      </div>
    </Transition>
  </nav>
</template>

<script setup>
defineProps({
  title: {
    type: String,
    default: 'Mnemonic'
  },
  links: {
    type: Array,
    default: () => [
      { label: 'Features', href: '#features' },
      { label: 'How It Works', href: '#how-it-works' },
      { label: 'Install', href: '#installation' },
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
