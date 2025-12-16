<template>
  <div class="bg-slate-950 text-white min-h-screen relative overflow-hidden">
    <!-- Animated Background -->
    <div class="fixed inset-0 pointer-events-none">
      <!-- Gradient orbs -->
      <div class="absolute top-0 left-1/4 w-96 h-96 bg-amber-500/10 rounded-full blur-3xl animate-float"></div>
      <div class="absolute bottom-1/4 right-1/4 w-80 h-80 bg-amber-400/5 rounded-full blur-3xl animate-float-delayed"></div>
      <div class="absolute top-1/2 right-0 w-64 h-64 bg-slate-700/20 rounded-full blur-3xl animate-glow"></div>

      <!-- Grid pattern -->
      <div class="absolute inset-0 bg-[linear-gradient(rgba(148,163,184,0.03)_1px,transparent_1px),linear-gradient(90deg,rgba(148,163,184,0.03)_1px,transparent_1px)] bg-[size:50px_50px]"></div>

      <!-- Floating particles -->
      <div v-for="i in 12" :key="i"
           class="particle animate-particle"
           :style="{
             left: `${(i * 8.3) % 100}%`,
             animationDelay: `${i * 1.5}s`,
             animationDuration: `${15 + (i % 5) * 3}s`
           }"></div>
    </div>

    <NavBar :mobile-menu-open="mobileMenuOpen" @toggle-menu="mobileMenuOpen = !mobileMenuOpen" />

    <HeroSection
      badge="The missing piece for Claude Code"
      title-before="Yesterday's context."
      title-highlight="Today's session."
      subtitle="Stop re-explaining your codebase. Claude Mnemonic captures bug fixes, architecture decisions, and coding patterns - then brings them back exactly when you need them."
    />

    <!-- Problem Section -->
    <section class="py-20 lg:py-28 px-4 sm:px-6 relative">
      <div class="max-w-6xl mx-auto grid lg:grid-cols-2 gap-8 lg:gap-16 items-center">
        <div>
          <h2 class="text-2xl sm:text-3xl md:text-4xl lg:text-5xl font-bold text-white mb-6">Sound familiar?</h2>
          <p class="text-slate-400 text-base sm:text-lg mb-8">Every Claude Code session starts fresh. That bug you fixed last Tuesday? Gone. The auth flow you explained in detail? Forgotten. Your team's naming conventions? You'll explain them again.</p>
          <ul class="space-y-4 sm:space-y-5">
            <li class="flex items-start gap-3 sm:gap-4 text-slate-400">
              <i class="fas fa-times-circle text-red-400 mt-1 flex-shrink-0"></i>
              <span class="text-sm sm:text-base">"We fixed this exact race condition last week..."</span>
            </li>
            <li class="flex items-start gap-3 sm:gap-4 text-slate-400">
              <i class="fas fa-times-circle text-red-400 mt-1 flex-shrink-0"></i>
              <span class="text-sm sm:text-base">"No, we use kebab-case for API routes, not camelCase..."</span>
            </li>
            <li class="flex items-start gap-3 sm:gap-4 text-slate-400">
              <i class="fas fa-times-circle text-red-400 mt-1 flex-shrink-0"></i>
              <span class="text-sm sm:text-base">"The database is Postgres, not MySQL. I told you yesterday..."</span>
            </li>
            <li class="flex items-start gap-3 sm:gap-4 text-white">
              <i class="fas fa-check-circle text-amber-500 mt-1 flex-shrink-0"></i>
              <span class="text-sm sm:text-base"><strong>Mnemonic remembers so you don't have to repeat yourself.</strong></span>
            </li>
          </ul>
        </div>

        <div class="glass rounded-2xl p-6 sm:p-8 relative overflow-hidden glow-amber">
          <div class="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-amber-500 to-transparent"></div>
          <p class="text-slate-500 text-xs sm:text-sm mb-4 font-mono">// What Mnemonic captures automatically:</p>
          <div class="space-y-3">
            <FlowItem icon="fas fa-bug" title="Bug fixes & solutions" description='"Fixed N+1 query in user loader by adding .includes(:posts)"' />
            <FlowItem icon="fas fa-sitemap" title="Architecture decisions" description='"Using event sourcing for audit trail - all mutations go through EventStore"' />
            <FlowItem icon="fas fa-code-branch" title="Project conventions" description='"API routes use /api/v1/ prefix, kebab-case for endpoints"' />
            <FlowItem icon="fas fa-shield-alt" title="Security patterns" description='"Always validate JWT on server side, never trust client claims"' />
          </div>
        </div>
      </div>
    </section>

    <!-- Features Section -->
    <section id="features" class="py-20 lg:py-28 bg-slate-900/30 relative">
      <div class="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-slate-700 to-transparent"></div>
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <SectionHeader title="Built for real workflows" subtitle="Not just storage - intelligent context that makes Claude more useful over time" />
        <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-4 sm:gap-6">
          <FeatureCard
            v-for="feature in features"
            :key="feature.title"
            :icon="feature.icon"
            :title="feature.title"
            :description="feature.description"
          />
        </div>
      </div>
    </section>

    <!-- Before/After Section -->
    <section class="py-20 lg:py-28 px-4 sm:px-6">
      <div class="max-w-6xl mx-auto">
        <SectionHeader title="The difference" subtitle="Same question. Different experience." />
        <div class="grid md:grid-cols-2 gap-6 sm:gap-8">
          <!-- Before -->
          <div class="glass rounded-2xl p-5 sm:p-6 relative border-red-500/20">
            <div class="absolute top-3 sm:top-4 right-3 sm:right-4 bg-red-500/20 text-red-400 px-2 sm:px-3 py-1 rounded-full text-xs font-medium">Without Mnemonic</div>
            <div class="space-y-3 sm:space-y-4 mt-8">
              <div class="bg-slate-800/50 rounded-lg p-3 sm:p-4">
                <p class="text-slate-400 text-xs sm:text-sm"><span class="text-blue-400">You:</span> Fix the authentication bug in the login flow</p>
              </div>
              <div class="bg-slate-800/50 rounded-lg p-3 sm:p-4">
                <p class="text-slate-400 text-xs sm:text-sm"><span class="text-amber-400">Claude:</span> I'd be happy to help! Can you tell me about your authentication setup? What framework are you using? How is the login flow structured?</p>
              </div>
              <div class="bg-slate-800/50 rounded-lg p-3 sm:p-4">
                <p class="text-slate-400 text-xs sm:text-sm"><span class="text-blue-400">You:</span> We use NextAuth with Postgres. The flow goes through /api/auth/... We talked about this last week...</p>
              </div>
              <p class="text-red-400 text-xs sm:text-sm text-center pt-2"><i class="fas fa-clock mr-2"></i>5+ minutes of re-explaining context</p>
            </div>
          </div>

          <!-- After -->
          <div class="glass rounded-2xl p-5 sm:p-6 relative border-green-500/20">
            <div class="absolute top-3 sm:top-4 right-3 sm:right-4 bg-green-500/20 text-green-400 px-2 sm:px-3 py-1 rounded-full text-xs font-medium">With Mnemonic</div>
            <div class="space-y-3 sm:space-y-4 mt-8">
              <div class="bg-slate-800/50 rounded-lg p-3 sm:p-4">
                <p class="text-slate-400 text-xs sm:text-sm"><span class="text-blue-400">You:</span> Fix the authentication bug in the login flow</p>
              </div>
              <div class="bg-slate-800/50 rounded-lg p-3 sm:p-4">
                <p class="text-slate-400 text-xs sm:text-sm"><span class="text-amber-400">Claude:</span> Looking at your NextAuth setup with Postgres, I see the issue is likely in the callback at <code class="text-amber-300 text-xs">/api/auth/[...nextauth].ts</code>. Based on the session validation pattern we established, the fix is...</p>
              </div>
              <p class="text-green-400 text-xs sm:text-sm text-center pt-2"><i class="fas fa-bolt mr-2"></i>Instant context. Immediate value.</p>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- How It Works Section -->
    <section id="how-it-works" class="py-20 lg:py-28 px-4 sm:px-6 bg-slate-900/30">
      <div class="max-w-6xl mx-auto">
        <SectionHeader title="Zero setup. Zero maintenance." subtitle="Install once, benefit forever. Mnemonic works silently in the background." />
        <div class="grid sm:grid-cols-2 lg:grid-cols-4 gap-6 sm:gap-8 relative">
          <div class="hidden lg:block absolute top-10 left-[60px] right-[60px] h-0.5 bg-gradient-to-r from-amber-500 via-amber-400 to-amber-500 opacity-30"></div>
          <StepCard
            v-for="step in steps"
            :key="step.number"
            :number="step.number"
            :title="step.title"
            :description="step.description"
          />
        </div>
      </div>
    </section>

    <!-- Installation Section -->
    <section id="installation" class="py-20 lg:py-28 px-4 sm:px-6">
      <div class="max-w-6xl mx-auto">
        <SectionHeader title="Quick install" subtitle="One command. No configuration. No account required." />

        <div class="flex flex-wrap justify-center gap-2 mb-6 sm:mb-8">
          <button
            v-for="tab in installTabs"
            :key="tab.id"
            @click="activeTab = tab.id"
            :class="[
              'px-4 sm:px-5 py-2 sm:py-2.5 rounded-lg text-xs sm:text-sm font-medium transition-all',
              activeTab === tab.id
                ? 'bg-amber-500/15 border border-amber-500 text-amber-500'
                : 'glass text-slate-400 hover:border-slate-600'
            ]"
          >
            {{ tab.label }}
          </button>
        </div>

        <div v-show="activeTab === 'macos'">
          <CodeBlock :code="installCommands.macos">
            <span class="text-slate-500"># That's it. Seriously.</span>
            <br>
            <span class="text-amber-400">curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.sh | bash</span>
          </CodeBlock>
        </div>

        <div v-show="activeTab === 'windows'">
          <CodeBlock :code="installCommands.windows">
            <span class="text-slate-500"># PowerShell (as Administrator)</span>
            <br>
            <span class="text-amber-400">irm https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.ps1 | iex</span>
          </CodeBlock>
        </div>

        <div v-show="activeTab === 'source'">
          <CodeBlock :code="installCommands.source">
            <span class="text-slate-500"># For contributors and tinkerers</span>
            <br>
            <span class="text-amber-400">git clone https://github.com/lukaszraczylo/claude-mnemonic.git</span>
            <br>
            <span class="text-amber-400">cd claude-mnemonic</span>
            <br>
            <span class="text-amber-400">make build && make install</span>
            <br><br>
            <span class="text-slate-500"># Requires: Go 1.24+, Node.js 18+, CGO compiler</span>
          </CodeBlock>
        </div>

        <p class="text-center text-slate-500 mt-6 sm:mt-8 text-xs sm:text-sm">
          After install, open <a href="http://localhost:37777" class="text-amber-400 hover:underline animated-underline">localhost:37777</a> to see the dashboard.
          Start a new Claude Code CLI session - memory is now active.
        </p>
      </div>
    </section>

    <!-- Requirements Section -->
    <section id="requirements" class="py-20 lg:py-28 px-4 sm:px-6 bg-slate-900/30">
      <div class="max-w-6xl mx-auto">
        <SectionHeader title="Requirements" subtitle="Minimal dependencies. Maximum functionality." />

        <div class="grid md:grid-cols-2 gap-6 sm:gap-8">
          <!-- Required -->
          <div class="glass rounded-2xl p-5 sm:p-6">
            <div class="flex items-center gap-2 mb-4">
              <i class="fas fa-check-circle text-green-500"></i>
              <span class="text-white font-semibold text-sm sm:text-base">Required</span>
            </div>
            <div class="space-y-3">
              <div v-for="req in requiredDeps" :key="req.name" class="flex items-start gap-3 p-3 bg-slate-800/50 rounded-lg">
                <i :class="[req.icon, 'text-amber-500 mt-0.5']"></i>
                <div>
                  <code class="text-white text-xs sm:text-sm font-semibold">{{ req.name }}</code>
                  <p class="text-slate-400 text-xs mt-1">{{ req.description }}</p>
                </div>
              </div>
            </div>
          </div>

          <!-- Optional -->
          <div class="glass rounded-2xl p-5 sm:p-6">
            <div class="flex items-center gap-2 mb-4">
              <i class="fas fa-plus-circle text-amber-500"></i>
              <span class="text-white font-semibold text-sm sm:text-base">Optional (for semantic search)</span>
            </div>
            <div class="space-y-3">
              <div v-for="req in optionalDeps" :key="req.name" class="flex items-start gap-3 p-3 bg-slate-800/50 rounded-lg">
                <i :class="[req.icon, 'text-slate-500 mt-0.5']"></i>
                <div>
                  <code class="text-white text-xs sm:text-sm font-semibold">{{ req.name }}</code>
                  <p class="text-slate-400 text-xs mt-1">{{ req.description }}</p>
                </div>
              </div>
            </div>
            <p class="text-slate-500 text-xs mt-4">
              <i class="fas fa-info-circle mr-1"></i>
              Without these, core functionality (SQLite storage, full-text search) still works.
            </p>
          </div>
        </div>

        <div class="mt-6 sm:mt-8 glass rounded-xl p-4 sm:p-5">
          <div class="flex items-center gap-2 mb-3">
            <i class="fas fa-terminal text-amber-500"></i>
            <span class="text-white font-semibold text-sm">Install optional dependencies</span>
          </div>
          <div class="grid sm:grid-cols-3 gap-3 text-xs">
            <div class="bg-slate-800/50 rounded-lg p-3">
              <p class="text-slate-400 mb-2">macOS</p>
              <code class="text-amber-400">brew install python3</code><br>
              <code class="text-amber-400">pip3 install uv</code>
            </div>
            <div class="bg-slate-800/50 rounded-lg p-3">
              <p class="text-slate-400 mb-2">Linux</p>
              <code class="text-amber-400">apt install python3</code><br>
              <code class="text-amber-400">pip3 install uv</code>
            </div>
            <div class="bg-slate-800/50 rounded-lg p-3">
              <p class="text-slate-400 mb-2">Windows</p>
              <code class="text-amber-400">winget install Python.Python.3</code><br>
              <code class="text-amber-400">pip install uv</code>
            </div>
          </div>
          <p class="text-slate-500 text-xs mt-3">
            <i class="fas fa-info-circle mr-1"></i>
            Requires Python 3.13+. Most package managers install the latest version.
          </p>
        </div>
      </div>
    </section>

    <!-- Configuration Section -->
    <section id="configuration" class="py-20 lg:py-28 px-4 sm:px-6 bg-slate-900/30">
      <div class="max-w-6xl mx-auto">
        <SectionHeader title="Fully configurable" subtitle="Works out of the box, but adapts to your preferences" />

        <div class="grid lg:grid-cols-2 gap-6 sm:gap-8">
          <!-- Config file example -->
          <div class="glass rounded-2xl p-5 sm:p-6">
            <div class="flex items-center gap-2 mb-4">
              <i class="fas fa-cog text-amber-500"></i>
              <span class="text-white font-semibold text-sm sm:text-base">~/.claude-mnemonic/settings.json</span>
            </div>
            <pre class="text-xs sm:text-sm font-mono overflow-x-auto"><code><span class="text-slate-500">{</span>
  <span class="text-emerald-400">"CLAUDE_MNEMONIC_WORKER_PORT"</span><span class="text-slate-500">:</span> <span class="text-amber-400">37777</span><span class="text-slate-500">,</span>
  <span class="text-emerald-400">"CLAUDE_MNEMONIC_CONTEXT_OBSERVATIONS"</span><span class="text-slate-500">:</span> <span class="text-amber-400">100</span><span class="text-slate-500">,</span>
  <span class="text-emerald-400">"CLAUDE_MNEMONIC_CONTEXT_FULL_COUNT"</span><span class="text-slate-500">:</span> <span class="text-amber-400">25</span><span class="text-slate-500">,</span>
  <span class="text-emerald-400">"CLAUDE_MNEMONIC_MODEL"</span><span class="text-slate-500">:</span> <span class="text-sky-400">"haiku"</span>
<span class="text-slate-500">}</span></code></pre>
          </div>

          <!-- Config options list -->
          <div class="space-y-3 sm:space-y-4">
            <div v-for="config in configOptions" :key="config.name" class="glass rounded-xl p-4 hover:border-amber-500/20 transition-colors">
              <div class="flex items-start gap-3">
                <div class="w-8 h-8 bg-amber-500/15 rounded-lg flex items-center justify-center text-amber-500 flex-shrink-0 text-sm">
                  <i :class="config.icon"></i>
                </div>
                <div class="min-w-0">
                  <code class="text-amber-400 text-xs sm:text-sm">{{ config.name }}</code>
                  <p class="text-slate-400 text-xs sm:text-sm mt-1">{{ config.description }}</p>
                </div>
              </div>
            </div>
          </div>
        </div>

        <p class="text-center text-slate-500 mt-6 sm:mt-8 text-xs sm:text-sm">
          All settings can also be set via environment variables. See <a href="https://github.com/lukaszraczylo/claude-mnemonic#configuration" target="_blank" class="text-amber-400 hover:underline">full documentation</a> for all options.
        </p>
      </div>
    </section>

    <!-- Technical Details -->
    <section class="py-20 lg:py-28 px-4 sm:px-6">
      <div class="max-w-6xl mx-auto">
        <SectionHeader title="Under the hood" subtitle="Built with simplicity and performance in mind" />
        <div class="grid sm:grid-cols-3 gap-4 sm:gap-8 text-center">
          <div class="glass rounded-2xl p-6 sm:p-8 hover:border-amber-500/30 transition-colors">
            <div class="text-3xl sm:text-4xl font-bold text-amber-500 mb-2">Go</div>
            <p class="text-slate-400 text-xs sm:text-sm">Worker service and hooks. Fast startup, low memory footprint, zero runtime dependencies.</p>
          </div>
          <div class="glass rounded-2xl p-6 sm:p-8 hover:border-amber-500/30 transition-colors">
            <div class="text-3xl sm:text-4xl font-bold text-amber-500 mb-2">SQLite</div>
            <p class="text-slate-400 text-xs sm:text-sm">FTS5 full-text search. Single file database. No server to manage. Survives restarts.</p>
          </div>
          <div class="glass rounded-2xl p-6 sm:p-8 hover:border-amber-500/30 transition-colors">
            <div class="text-3xl sm:text-4xl font-bold text-amber-500 mb-2">ChromaDB</div>
            <p class="text-slate-400 text-xs sm:text-sm">Vector embeddings for semantic search. "Fix auth" finds "JWT validation issue" automatically.</p>
          </div>
        </div>
      </div>
    </section>

    <!-- Trust Section -->
    <section class="py-20 lg:py-28 px-4 sm:px-6 bg-slate-900/30">
      <div class="max-w-4xl mx-auto">
        <SectionHeader title="Open source. Real person." subtitle="Not another anonymous package - built by a developer you can verify" />

        <div class="glass rounded-2xl p-6 sm:p-8 text-center">
          <div class="flex flex-col sm:flex-row items-center justify-center gap-6 sm:gap-8 mb-6">
            <a href="https://github.com/lukaszraczylo" target="_blank" class="group">
              <img
                src="https://github.com/lukaszraczylo.png"
                alt="Lukasz Raczylo"
                class="w-20 h-20 sm:w-24 sm:h-24 rounded-full border-2 border-slate-700 group-hover:border-amber-500 transition-colors"
              />
            </a>
            <div class="text-center sm:text-left">
              <h3 class="text-white font-semibold text-lg sm:text-xl mb-1">Lukasz Raczylo</h3>
              <p class="text-slate-400 text-sm mb-3">Principal Engineer & Open Source Maintainer</p>
              <div class="flex flex-wrap justify-center sm:justify-start gap-3">
                <a href="https://github.com/lukaszraczylo" target="_blank" class="text-slate-500 hover:text-white transition-colors text-sm">
                  <i class="fab fa-github mr-1"></i>GitHub
                </a>
                <a href="https://linkedin.com/in/lukaszraczylo" target="_blank" class="text-slate-500 hover:text-white transition-colors text-sm">
                  <i class="fab fa-linkedin mr-1"></i>LinkedIn
                </a>
                <a href="https://raczylo.com" target="_blank" class="text-slate-500 hover:text-white transition-colors text-sm">
                  <i class="fas fa-globe mr-1"></i>Website
                </a>
              </div>
            </div>
          </div>

          <div class="grid sm:grid-cols-3 gap-4 pt-6 border-t border-slate-700/50">
            <div class="text-center">
              <div class="text-amber-500 font-bold text-xl sm:text-2xl mb-1">100%</div>
              <p class="text-slate-400 text-xs sm:text-sm">Open Source</p>
            </div>
            <div class="text-center">
              <div class="text-amber-500 font-bold text-xl sm:text-2xl mb-1">MIT</div>
              <p class="text-slate-400 text-xs sm:text-sm">License</p>
            </div>
            <div class="text-center">
              <div class="text-amber-500 font-bold text-xl sm:text-2xl mb-1">0</div>
              <p class="text-slate-400 text-xs sm:text-sm">Data collection</p>
            </div>
          </div>
        </div>

        <p class="text-center text-slate-500 mt-6 sm:mt-8 text-xs sm:text-sm">
          Unlike anonymous packages, you can verify who built this, read every line of code, and reach out directly with questions.
          <br class="hidden sm:block">
          Your security matters - that's why transparency is non-negotiable.
        </p>
      </div>
    </section>

    <!-- FAQ Section -->
    <section id="faq" class="py-20 lg:py-28 px-4 sm:px-6">
      <div class="max-w-4xl mx-auto">
        <SectionHeader title="Questions?" />
        <div class="grid sm:grid-cols-2 gap-4 sm:gap-6">
          <FaqItem
            v-for="faq in faqs"
            :key="faq.question"
            :question="faq.question"
            :answer="faq.answer"
          />
        </div>
      </div>
    </section>

    <FooterSection />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import NavBar from './components/NavBar.vue'
import HeroSection from './components/HeroSection.vue'
import SectionHeader from './components/SectionHeader.vue'
import FeatureCard from './components/FeatureCard.vue'
import StepCard from './components/StepCard.vue'
import FlowItem from './components/FlowItem.vue'
import CodeBlock from './components/CodeBlock.vue'
import FaqItem from './components/FaqItem.vue'
import FooterSection from './components/FooterSection.vue'

const mobileMenuOpen = ref(false)
const activeTab = ref('macos')

const features = [
  { icon: 'fas fa-brain', title: 'Learns as you work', description: 'Every bug fix, every architecture decision, every "aha moment" - captured automatically without breaking your flow.' },
  { icon: 'fas fa-folder-tree', title: 'Project-aware context', description: 'Your React knowledge stays in React projects. Your Go patterns stay in Go projects. No context pollution.' },
  { icon: 'fas fa-globe', title: 'Shared best practices', description: 'Security patterns, performance tips, and universal learnings automatically available across all your projects.' },
  { icon: 'fas fa-search', title: 'Finds what matters', description: 'Semantic search finds relevant memories even when you don\'t remember the exact words. "That auth thing" just works.' },
  { icon: 'fas fa-chart-line', title: 'Live statusline', description: 'Real-time metrics right in Claude Code: memories served, searches performed, and project memory count at a glance.' },
  { icon: 'fas fa-lock', title: '100% private', description: 'Your code context never leaves your machine. No telemetry. No cloud sync. Your memories are yours.' },
]

const steps = [
  { number: 1, title: 'Install', description: 'One command. Hooks into Claude Code automatically.' },
  { number: 2, title: 'Work normally', description: 'Code with Claude as usual. Mnemonic listens silently in the background.' },
  { number: 3, title: 'Knowledge builds', description: 'Every session adds to your knowledge base. Bug fixes. Decisions. Patterns.' },
  { number: 4, title: 'Context appears', description: 'Start a new session - relevant memories are already there. No re-explaining.' },
]

const installTabs = [
  { id: 'macos', label: 'macOS / Linux' },
  { id: 'windows', label: 'Windows' },
  { id: 'source', label: 'From Source' },
]

const installCommands = {
  macos: `curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.sh | bash`,
  windows: `irm https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.ps1 | iex`,
  source: `git clone https://github.com/lukaszraczylo/claude-mnemonic.git\ncd claude-mnemonic\nmake build && make install`,
}

const configOptions = [
  { name: 'CLAUDE_MNEMONIC_WORKER_PORT', description: 'HTTP port for the worker service (default: 37777)', icon: 'fas fa-network-wired' },
  { name: 'CLAUDE_MNEMONIC_CONTEXT_OBSERVATIONS', description: 'Maximum observations injected per session (default: 100)', icon: 'fas fa-layer-group' },
  { name: 'CLAUDE_MNEMONIC_CONTEXT_FULL_COUNT', description: 'Observations with full narrative detail, rest are condensed (default: 25)', icon: 'fas fa-expand' },
  { name: 'CLAUDE_MNEMONIC_MODEL', description: 'Model for processing observations (default: haiku)', icon: 'fas fa-microchip' },
]

const requiredDeps = [
  { name: 'Claude Code CLI', description: 'Host application - this is a plugin for Claude Code. Uses your existing subscription (Pro/Max) instead of API keys.', icon: 'fas fa-terminal' },
  { name: 'jq', description: 'JSON processor used during installation. Usually pre-installed on most systems.', icon: 'fas fa-code' },
]

const optionalDeps = [
  { name: 'Python 3.13+', description: 'Required for ChromaDB semantic search. Natural language queries like "that auth bug" work.', icon: 'fab fa-python' },
  { name: 'uv', description: 'Python package manager for ChromaDB MCP server. Provides the uvx command.', icon: 'fas fa-box' },
]

const faqs = [
  { question: 'Will it confuse Claude with wrong context?', answer: 'No. Mnemonic uses project isolation and semantic relevance scoring. Only memories from the current project (or global best practices) are injected, and only when they\'re actually relevant to your prompt.' },
  { question: 'What exactly gets saved?', answer: 'Bug fixes with context ("Fixed race condition by adding mutex"), architecture decisions ("Using repository pattern for data access"), conventions ("All API routes prefixed with /api/v1"), and learnings you want to preserve.' },
  { question: 'Can I delete or edit memories?', answer: 'Yes. The web dashboard at localhost:37777 lets you browse, search, edit, and delete any memory. You\'re always in control.' },
  { question: 'Does it work with my existing Claude Code setup?', answer: 'Yes. Mnemonic installs as a Claude Code plugin with hooks. Your existing workflows, settings, and shortcuts remain unchanged.' },
  { question: 'What if I switch between projects frequently?', answer: 'That\'s the point. Each project has isolated memories. Switch from your Python ML project to your TypeScript app - context switches automatically.' },
  { question: 'Is there a performance impact?', answer: 'Minimal. The Go worker is lightweight (typically under 30MB RAM). Context injection at session start takes milliseconds for most projects.' },
]
</script>
