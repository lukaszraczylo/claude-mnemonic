<template>
  <div class="min-h-screen flex flex-col">
    <NavBar :mobile-menu-open="mobileMenuOpen" @toggle-menu="mobileMenuOpen = !mobileMenuOpen" />

    <main class="flex-1">
      <HeroSection />

      <section class="py-10 px-4 sm:px-6">
        <div class="max-w-5xl mx-auto">
          <div class="rounded-xl overflow-hidden border border-neutral-800">
            <img src="/claude-mnemonic.jpg" alt="Claude Mnemonic Dashboard" class="w-full h-auto block" loading="lazy" />
          </div>
          <p class="text-center text-neutral-600 text-xs mt-3 tracking-wide uppercase">Dashboard — localhost:37777</p>
        </div>
      </section>

      <section id="features" class="py-20 px-4 sm:px-6">
        <div class="max-w-5xl mx-auto">
          <h2 class="text-3xl sm:text-4xl font-bold text-white mb-4">What it does</h2>
          <p class="text-neutral-500 mb-12 max-w-xl">Captures context from your Claude Code sessions and brings it back when you need it. No manual effort.</p>
          <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-6">
            <FeatureItem
              v-for="feature in features"
              :key="feature.title"
              :icon="feature.icon"
              :title="feature.title"
              :description="feature.description"
            />
          </div>
        </div>
      </section>

      <section class="py-10 px-4 sm:px-6">
        <div class="max-w-5xl mx-auto">
          <div class="rounded-xl overflow-hidden border border-neutral-800">
            <img src="/observation-relation-graph.jpg" alt="Knowledge Graph" class="w-full h-auto block" loading="lazy" />
          </div>
          <p class="text-center text-neutral-600 text-xs mt-3 tracking-wide uppercase">Observation relationship graph</p>
        </div>
      </section>

      <section id="install" class="py-20 px-4 sm:px-6">
        <div class="max-w-5xl mx-auto">
          <h2 class="text-3xl sm:text-4xl font-bold text-white mb-4">Install</h2>
          <p class="text-neutral-500 mb-8">One command. No configuration. No account.</p>

          <div class="flex gap-1 mb-6">
            <button
              v-for="tab in installTabs"
              :key="tab.id"
              @click="activeTab = tab.id"
              :class="[
                'px-4 py-2 text-sm rounded-lg transition-colors',
                activeTab === tab.id
                  ? 'bg-neutral-800 text-white'
                  : 'text-neutral-500 hover:text-neutral-300'
              ]"
            >
              {{ tab.label }}
            </button>
          </div>

          <div v-show="activeTab === 'macos'">
            <CodeBlock code="curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.sh | bash" />
          </div>

          <div v-show="activeTab === 'windows'">
            <CodeBlock code="irm https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.ps1 | iex" />
          </div>

          <div v-show="activeTab === 'source'">
            <CodeBlock code="git clone https://github.com/lukaszraczylo/claude-mnemonic.git&#10;cd claude-mnemonic&#10;make build && make install" />
            <p class="text-neutral-600 text-xs mt-2">Requires: Go 1.24+, Node.js 18+, CGO compiler</p>
          </div>

          <div class="mt-8 p-4 rounded-lg border border-neutral-800 bg-neutral-900/50 max-w-2xl">
            <p class="text-neutral-500 text-sm">
              <i class="fas fa-info-circle text-neutral-600 mr-1"></i>
              After install, open <a href="http://localhost:37777" class="text-amber-500 hover:text-amber-400">localhost:37777</a> for the dashboard. Start a new Claude Code session — memory is active.
            </p>
          </div>
        </div>
      </section>

      <section id="requirements" class="py-20 px-4 sm:px-6">
        <div class="max-w-5xl mx-auto">
          <h2 class="text-3xl sm:text-4xl font-bold text-white mb-4">Requirements</h2>
          <p class="text-neutral-500 mb-8">Minimal dependencies. Everything else is built in.</p>

          <div class="grid sm:grid-cols-2 gap-4 max-w-xl">
            <div v-for="req in requiredDeps" :key="req.name" class="p-4 rounded-lg border border-neutral-800 bg-neutral-900/50">
              <div class="flex items-center gap-2 mb-1">
                <i :class="[req.icon, 'text-amber-500 text-sm']"></i>
                <code class="text-white text-sm font-semibold">{{ req.name }}</code>
              </div>
              <p class="text-neutral-500 text-xs pl-6">{{ req.description }}</p>
            </div>
          </div>
          <p class="text-neutral-600 text-sm mt-4">No Python. No external services. No API keys — uses your existing Claude Pro/Max subscription.</p>
        </div>
      </section>

      <section id="config" class="py-20 px-4 sm:px-6">
        <div class="max-w-5xl mx-auto">
          <h2 class="text-3xl sm:text-4xl font-bold text-white mb-4">Configuration</h2>
          <p class="text-neutral-500 mb-8">Works out of the box. Adjust if you want to.</p>

          <div class="grid lg:grid-cols-2 gap-8">
            <div class="rounded-lg border border-neutral-800 bg-neutral-900/50 p-5">
              <p class="text-neutral-600 text-xs mb-3 font-mono">~/.claude-mnemonic/settings.json</p>
              <pre class="text-sm overflow-x-auto leading-relaxed"><code>{
  <span class="text-emerald-400">"CLAUDE_MNEMONIC_WORKER_PORT"</span>: <span class="text-amber-400">37777</span>,
  <span class="text-emerald-400">"CLAUDE_MNEMONIC_CONTEXT_OBSERVATIONS"</span>: <span class="text-amber-400">100</span>,
  <span class="text-emerald-400">"CLAUDE_MNEMONIC_CONTEXT_FULL_COUNT"</span>: <span class="text-amber-400">25</span>,
  <span class="text-emerald-400">"CLAUDE_MNEMONIC_RERANKING_ENABLED"</span>: <span class="text-amber-400">true</span>
}</code></pre>
            </div>

            <div class="space-y-2">
              <div v-for="config in configOptions" :key="config.name" class="flex items-start gap-3 p-3 rounded-lg border border-neutral-800/50">
                <div class="w-6 h-6 rounded bg-neutral-800 flex items-center justify-center flex-shrink-0 mt-0.5">
                  <i class="fas fa-sliders text-neutral-500 text-xs"></i>
                </div>
                <div>
                  <code class="text-amber-500 text-xs">{{ config.name }}</code>
                  <p class="text-neutral-500 text-xs mt-0.5">{{ config.description }}</p>
                </div>
              </div>
            </div>
          </div>

          <p class="text-neutral-600 text-xs mt-6">
            All settings also work as environment variables. <a href="https://github.com/lukaszraczylo/claude-mnemonic#configuration" target="_blank" class="text-neutral-500 hover:text-neutral-300">Full docs</a>.
          </p>
        </div>
      </section>

      <section id="architecture" class="py-20 px-4 sm:px-6">
        <div class="max-w-5xl mx-auto">
          <h2 class="text-3xl sm:text-4xl font-bold text-white mb-4">Under the hood</h2>
          <p class="text-neutral-500 mb-10">No magic. Here's what's running locally on your machine.</p>
          <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
            <TechCard v-for="tech in techStack" :key="tech.name" :name="tech.name" :description="tech.description" />
          </div>
        </div>
      </section>

      <section id="faq" class="py-20 px-4 sm:px-6">
        <div class="max-w-3xl mx-auto">
          <h2 class="text-3xl sm:text-4xl font-bold text-white mb-10">FAQ</h2>
          <FaqItem
            v-for="faq in faqs"
            :key="faq.question"
            :question="faq.question"
            :answer="faq.answer"
          />
        </div>
      </section>

      <section class="py-20 px-4 sm:px-6">
        <div class="max-w-3xl mx-auto text-center">
          <p class="text-neutral-600 text-sm mb-2">Open source, built by</p>
          <a href="https://github.com/lukaszraczylo" target="_blank" class="inline-flex items-center gap-3 text-neutral-400 hover:text-white transition-colors group">
            <img src="https://github.com/lukaszraczylo.png" alt="Lukasz Raczylo" class="w-10 h-10 rounded-full border border-neutral-700 group-hover:border-neutral-500 transition-colors" />
            <div class="text-left">
              <div class="text-sm font-medium">Lukasz Raczylo</div>
              <div class="text-xs text-neutral-600">MIT License</div>
            </div>
          </a>
        </div>
      </section>
    </main>

    <FooterSection />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import NavBar from './components/NavBar.vue'
import HeroSection from './components/HeroSection.vue'
import FeatureItem from './components/FeatureItem.vue'
import TechCard from './components/TechCard.vue'
import CodeBlock from './components/CodeBlock.vue'
import FaqItem from './components/FaqItem.vue'
import FooterSection from './components/FooterSection.vue'

const mobileMenuOpen = ref(false)
const activeTab = ref('macos')

const features = [
  { icon: 'fas fa-brain', title: 'Session memory', description: 'Captures bug fixes, decisions, and patterns automatically from your Claude Code sessions.' },
  { icon: 'fas fa-magnifying-glass-chart', title: 'Two-stage retrieval', description: 'Bi-encoder embeddings followed by cross-encoder reranking for accurate results.' },
  { icon: 'fas fa-diagram-project', title: 'Knowledge graph', description: 'Automatic relationship detection between observations via file overlap and semantic similarity.' },
  { icon: 'fas fa-folder-tree', title: 'Project isolation', description: 'Each project has its own memory. Your React knowledge doesn\'t leak into Go projects.' },
  { icon: 'fas fa-code', title: 'AST-aware chunking', description: 'Tree-sitter splits Go, Python, and TypeScript code at semantic boundaries.' },
  { icon: 'fas fa-database', title: 'Hybrid vector storage', description: 'Selective embedding caching. 60-80% storage reduction with minimal latency impact.' },
  { icon: 'fas fa-lock', title: 'Local embeddings', description: 'ONNX Runtime with all-MiniLM-L6-v2. No external API calls, everything stays on your machine.' },
  { icon: 'fas fa-gauge', title: 'Live statusline', description: 'Real-time metrics in Claude Code: [mnemonic] ● served:42 | project:28 memories' },
  { icon: 'fas fa-arrows-rotate', title: 'Auto-updates', description: 'Checks for new versions on startup, downloads in the background, applies on restart.' },
]

const installTabs = [
  { id: 'macos', label: 'macOS / Linux' },
  { id: 'windows', label: 'Windows' },
  { id: 'source', label: 'From Source' },
]

const configOptions = [
  { name: 'WORKER_PORT', description: 'Dashboard and API port (default: 37777)' },
  { name: 'CONTEXT_OBSERVATIONS', description: 'Max memories injected per session (default: 100)' },
  { name: 'CONTEXT_FULL_COUNT', description: 'Full-detail memories, rest condensed (default: 25)' },
  { name: 'CONTEXT_RELEVANCE_THRESHOLD', description: 'Min similarity score 0.0-1.0 (default: 0.3)' },
  { name: 'RERANKING_ENABLED', description: 'Cross-encoder reranking (default: true)' },
  { name: 'VECTOR_STORAGE_STRATEGY', description: 'hub (default), always, or on_demand' },
  { name: 'GRAPH_ENABLED', description: 'Graph-based search with relationships (default: true)' },
  { name: 'EMBEDDING_MODEL', description: 'Embedding model for semantic search (default: bge-v1.5)' },
]

const requiredDeps = [
  { name: 'Claude Code CLI', description: 'Host application. Uses your existing subscription.', icon: 'fas fa-terminal' },
  { name: 'jq', description: 'JSON processing during install. Usually pre-installed.', icon: 'fas fa-code' },
]

const techStack = [
  { name: 'Go', description: 'Single binary, fast startup, low memory, zero runtime dependencies.' },
  { name: 'SQLite + FTS5', description: 'Full-text search in a single file. Survives restarts.' },
  { name: 'sqlite-vec', description: 'Vector search embedded in SQLite with LEANN-inspired selective embeddings.' },
  { name: 'BGE reranker', description: 'Cross-encoder for two-stage retrieval: bi-encoder + reranking.' },
  { name: 'Tree-sitter', description: 'AST-aware code chunking for Go, Python, and TypeScript.' },
  { name: 'CSR Graph', description: 'Memory-efficient observation relationship graph with edge detection.' },
]

const faqs = [
  { question: 'Will it confuse Claude with wrong context?', answer: 'Project isolation and semantic relevance scoring prevent that. Only memories from the current project (or global best practices) are injected, and only when relevant to your prompt.' },
  { question: 'What gets stored?', answer: 'Bug fixes with context, architecture decisions, project conventions, security patterns — things you\'d otherwise re-explain each session. No raw conversation dumps.' },
  { question: 'Can I delete or edit memories?', answer: 'Yes. The dashboard at localhost:37777 lets you browse, search, edit, and delete anything. You can also view graph relationships and storage metrics.' },
  { question: 'Performance impact?', answer: 'The Go worker uses ~30MB RAM. Context injection at session start takes milliseconds for most projects. Hybrid storage auto-tunes for your workload.' },
  { question: 'Does it work with my existing setup?', answer: 'Yes. Installs as a Claude Code plugin with hooks. Your workflows, settings, and shortcuts stay the same.' },
  { question: 'What if I switch between projects?', answer: 'Each project has isolated memories. Switch from your Python ML project to your TypeScript app — context switches automatically.' },
]
</script>
