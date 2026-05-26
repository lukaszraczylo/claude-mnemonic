<template>
  <section class="pt-24 pb-8 sm:pt-32 sm:pb-12 px-4 sm:px-6">
    <div class="max-w-5xl mx-auto">
      <p class="text-amber-500 text-sm font-medium mb-4 tracking-wide">Persistent memory for Claude Code</p>

      <h1 class="text-4xl sm:text-5xl lg:text-6xl font-extrabold text-white leading-[1.1] mb-6 max-w-3xl">
        Stop re-explaining your codebase every session.
      </h1>

      <p class="text-neutral-400 text-lg sm:text-xl max-w-2xl mb-10 leading-relaxed">
        Claude Code forgets everything when your session ends. Mnemonic captures what you've already explained — bug fixes, architecture decisions, patterns — and brings it back when relevant.
      </p>

      <div class="flex flex-col sm:flex-row gap-3 mb-12">
        <a href="#install" class="inline-flex items-center justify-center gap-2 bg-white text-neutral-900 px-6 py-3 rounded-lg font-semibold text-sm hover:bg-neutral-200 transition-colors">
          Install now
        </a>
        <a href="https://github.com/lukaszraczylo/claude-mnemonic" target="_blank" class="inline-flex items-center justify-center gap-2 border border-neutral-700 text-neutral-400 px-6 py-3 rounded-lg font-medium text-sm hover:border-neutral-500 hover:text-white transition-colors">
          <i class="fab fa-github"></i> View source
        </a>
      </div>

      <div class="max-w-2xl">
        <div class="flex items-stretch bg-neutral-900 border border-neutral-800 rounded-lg overflow-hidden">
          <div class="flex items-center px-3 gap-1.5 border-r border-neutral-800">
            <span class="w-3 h-3 rounded-full bg-neutral-700"></span>
            <span class="w-3 h-3 rounded-full bg-neutral-700"></span>
            <span class="w-3 h-3 rounded-full bg-neutral-700"></span>
          </div>
          <code class="flex-1 text-neutral-300 text-xs sm:text-sm px-4 py-3 font-mono overflow-x-auto">{{ installCommand }}</code>
          <button @click="copyCommand" class="px-4 text-neutral-600 hover:text-neutral-300 transition-colors border-l border-neutral-800 text-sm" title="Copy">
            <i :class="copied ? 'fas fa-check text-emerald-500' : 'fas fa-copy'"></i>
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup>
import { ref } from 'vue'

const installCommand = 'curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.sh | bash'
const copied = ref(false)

function copyCommand() {
  navigator.clipboard.writeText(installCommand)
  copied.value = true
  setTimeout(() => copied.value = false, 2000)
}
</script>
