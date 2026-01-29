<template>
  <div>
    <header
      class="relative px-6 py-4 bg-linear-to-br from-slate-800/95 to-slate-950/98 backdrop-blur-lg border-b border-white/10 shadow-lg">
      <div class="absolute inset-x-0 top-0 h-0.5 bg-linear-to-r from-transparent via-blue-500/50 to-transparent"></div>
      <a class="group flex items-center gap-4 w-fit cursor-pointer transition-transform hover:-translate-y-0.5"
        @click="currentPage = 'hub'">
        <div
          class="flex items-center justify-center w-14 h-14 bg-linear-to-br from-blue-500/10 to-purple-600/10 rounded-xl border border-blue-500/20 transition-all group-hover:shadow-[0_0_25px_rgba(59,130,246,0.4)]">
          <Icon icon="logos:adonisjs-icon"
            class="w-8 h-8 transition-transform group-hover:scale-110 group-hover:rotate-6 drop-shadow-[0_0_8px_rgba(59,130,246,0.6)]" />
        </div>
        <div class="flex flex-col items-start">
          <h1
            class="text-2xl font-bold bg-linear-to-r from-white to-slate-300 bg-clip-text text-transparent tracking-tight">
            Paradox Modding Tool</h1>
          <span class="text-xs text-slate-400 font-medium tracking-wider uppercase">Tools and Utilities for Paradox Mod
            Developers</span>
        </div>
      </a>
    </header>

    <div v-show="currentPage === 'hub'">
      <Hub @select="currentPage = $event" />
    </div>
    <div v-show="currentPage === 'comparison'">
      <ComparisonTool />
    </div>
    <div v-show="currentPage === 'merge'">
      <MergeTool />
    </div>
  </div>
</template>

<script>
import Menubar from 'primevue/menubar'
import Button from 'primevue/button'
import Hub from './components/Hub.vue'
import ComparisonTool from './components/ComparisonTool.vue'
import MergeTool from './components/MergeTool.vue'
import { Icon } from "@iconify/vue";

export default {
  name: 'App',
  components: {
    Menubar,
    Button,
    Hub,
    ComparisonTool,
    MergeTool,
    Icon,
  },
  data() {
    return {
      currentPage: 'hub',
    }
  }
}
</script>