<template>
  <div class="rounded-lg border border-neutral-800 bg-neutral-900/30 p-4 relative overflow-hidden">
    <button @click="copyCode" class="absolute top-3 right-3 text-neutral-600 hover:text-neutral-300 text-xs transition-colors px-2 py-1 rounded border border-neutral-800 hover:border-neutral-600">
      {{ copied ? 'Copied' : 'Copy' }}
    </button>
    <pre class="text-sm whitespace-pre-wrap break-all font-mono text-neutral-300 pr-20 leading-relaxed"><code>{{ code }}</code></pre>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  code: { type: String, default: '' },
})

const copied = ref(false)

function copyCode() {
  navigator.clipboard.writeText(props.code)
  copied.value = true
  setTimeout(() => copied.value = false, 2000)
}
</script>
