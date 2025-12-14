<template>
  <div class="glass rounded-xl p-4 sm:p-6 relative max-w-4xl mx-auto glow-amber">
    <button @click="copyCode" class="absolute top-3 sm:top-4 right-3 sm:right-4 bg-slate-800/50 border border-slate-700/50 text-slate-500 hover:text-white hover:border-slate-600 px-2 sm:px-3 py-1 sm:py-1.5 rounded-md text-xs transition-all">
      {{ copied ? 'Copied!' : 'Copy' }}
    </button>
    <pre class="text-xs sm:text-sm whitespace-pre-wrap break-all font-mono pr-16 sm:pr-20"><slot></slot></pre>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  code: {
    type: String,
    default: ''
  }
})

const copied = ref(false)

function copyCode() {
  navigator.clipboard.writeText(props.code)
  copied.value = true
  setTimeout(() => copied.value = false, 2000)
}
</script>
