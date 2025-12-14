<template>
  <section class="min-h-screen flex items-center relative pt-24 sm:pt-28 pb-16 sm:pb-20">
    <!-- Background Effects -->
    <div class="absolute inset-0 overflow-hidden">
      <div class="absolute top-[-50%] left-1/2 -translate-x-1/2 w-[150%] h-full bg-[radial-gradient(ellipse_at_center,rgba(245,158,11,0.12)_0%,transparent_60%)] opacity-60"></div>
    </div>

    <div class="relative z-10 max-w-6xl mx-auto px-4 sm:px-6 text-center">
      <div class="inline-flex items-center gap-2 glass px-3 sm:px-4 py-1.5 sm:py-2 rounded-full text-xs sm:text-sm text-slate-400 mb-6 sm:mb-8 opacity-0 animate-fade-in-up">
        <span class="w-2 h-2 bg-amber-500 rounded-full animate-pulse-slow"></span>
        {{ badge }}
      </div>

      <h1 class="text-3xl sm:text-4xl md:text-5xl lg:text-6xl xl:text-7xl font-bold text-white mb-4 sm:mb-6 leading-tight opacity-0 animate-fade-in-up animation-delay-100">
        {{ titleBefore }}<br class="hidden sm:block">
        <span class="sm:hidden"> </span>
        <span class="text-gradient">{{ titleHighlight }}</span>
      </h1>

      <p class="text-base sm:text-lg md:text-xl text-slate-400 max-w-2xl mx-auto mb-8 sm:mb-10 opacity-0 animate-fade-in-up animation-delay-200 px-2">
        {{ subtitle }}
      </p>

      <div class="flex flex-col sm:flex-row gap-3 sm:gap-4 justify-center mb-12 sm:mb-16 opacity-0 animate-fade-in-up animation-delay-300 px-4 sm:px-0">
        <a :href="primaryCta.href" class="inline-flex items-center justify-center gap-2 bg-gradient-to-br from-amber-500 to-amber-400 text-slate-950 px-6 sm:px-8 py-3 sm:py-3.5 rounded-lg font-semibold hover:scale-105 hover:shadow-lg hover:shadow-amber-500/30 transition-all">
          <i :class="primaryCta.icon"></i>
          {{ primaryCta.label }}
        </a>
        <a :href="secondaryCta.href" target="_blank" class="inline-flex items-center justify-center gap-2 glass text-white px-6 sm:px-8 py-3 sm:py-3.5 rounded-lg font-medium hover:border-amber-500/30 hover:bg-amber-500/10 transition-all">
          <i :class="secondaryCta.icon"></i>
          {{ secondaryCta.label }}
        </a>
      </div>

      <!-- Install Command -->
      <div class="opacity-0 animate-fade-in-up animation-delay-400 px-2 sm:px-0">
        <div class="glass rounded-xl px-4 sm:px-6 py-4 sm:py-5 flex flex-col sm:flex-row items-center justify-between gap-3 sm:gap-4 max-w-4xl mx-auto glow-amber">
          <code class="text-amber-400 text-xs sm:text-sm md:text-base flex-1 text-center sm:text-left break-all font-mono">{{ installCommand }}</code>
          <button @click="copyCommand" class="text-slate-500 hover:text-white transition-colors p-2 flex-shrink-0 rounded-lg hover:bg-slate-800/50">
            <i :class="copied ? 'fas fa-check text-amber-500' : 'fas fa-copy'"></i>
            <span class="ml-2 text-sm sm:hidden">{{ copied ? 'Copied!' : 'Copy' }}</span>
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup>
import { ref } from 'vue'

defineProps({
  badge: {
    type: String,
    default: 'Persistent Memory for Claude Code'
  },
  titleBefore: {
    type: String,
    default: 'Claude forgets.'
  },
  titleHighlight: {
    type: String,
    default: 'Make it remember.'
  },
  subtitle: {
    type: String,
    default: 'Capture learnings, decisions, and patterns from your coding sessions. Bring that knowledge back in every future conversation.'
  },
  primaryCta: {
    type: Object,
    default: () => ({ label: 'Install Now', href: '#installation', icon: 'fas fa-download' })
  },
  secondaryCta: {
    type: Object,
    default: () => ({ label: 'View Source', href: 'https://github.com/lukaszraczylo/claude-mnemonic', icon: 'fab fa-github' })
  },
  installCommand: {
    type: String,
    default: 'curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.sh | bash'
  }
})

const copied = ref(false)

function copyCommand() {
  navigator.clipboard.writeText('curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.sh | bash')
  copied.value = true
  setTimeout(() => copied.value = false, 2000)
}
</script>
