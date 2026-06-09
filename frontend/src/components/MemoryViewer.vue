<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import type { MemNote, MemoryPage } from '../types/game'

const PER_PAGE = 8

const open = ref(false)
const page = ref(1)
const data = ref<MemoryPage>({ notes: [], total: 0, page: 1, per_page: PER_PAGE, pages: 1 })
const loading = ref(false)
const root = ref<HTMLElement | null>(null)

let pollTimer: ReturnType<typeof setInterval> | null = null

async function fetchPage(p: number) {
  loading.value = true
  try {
    const res = await fetch(`/api/memories?page=${p}&per_page=${PER_PAGE}`)
    if (res.ok) data.value = await res.json()
  } finally {
    loading.value = false
  }
}

function toggle() {
  open.value = !open.value
  if (open.value) fetchPage(page.value)
}

function prev() { if (page.value > 1) { page.value--; fetchPage(page.value) } }
function next() { if (page.value < data.value.pages) { page.value++; fetchPage(page.value) } }

watch(open, (val) => {
  if (val) {
    pollTimer = setInterval(() => fetchPage(page.value), 4000)
  } else {
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  }
})

function onOutsideClick(e: MouseEvent) {
  if (root.value && !root.value.contains(e.target as Node)) open.value = false
}

function formatTime(ms: number) {
  return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

onMounted(() => {
  document.addEventListener('mousedown', onOutsideClick)
  // fetch total count for badge even while closed
  fetch(`/api/memories?page=1&per_page=1`).then(r => r.ok && r.json()).then(d => {
    if (d) data.value = { ...data.value, total: d.total }
  })
})
onUnmounted(() => {
  document.removeEventListener('mousedown', onOutsideClick)
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<template>
  <div ref="root" class="relative">
    <!-- Trigger -->
    <button
      class="flex items-center gap-1 text-xs font-mono border border-gray-300 rounded px-2 py-1 bg-white text-gray-700 hover:bg-gray-50 focus:outline-none focus:ring-1 focus:ring-gray-400 whitespace-nowrap"
      :class="{ 'border-indigo-400 text-indigo-700': open }"
      @click="toggle"
      title="AI model memory"
    >
      🧠
      <span class="tabular-nums">{{ data.total }}</span>
      <svg class="w-3 h-3 ml-0.5 shrink-0 transition-transform" :class="{ 'rotate-180': open }" viewBox="0 0 20 20" fill="currentColor">
        <path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd"/>
      </svg>
    </button>

    <!-- Dropdown -->
    <Transition name="drop">
      <div
        v-if="open"
        class="absolute right-0 top-full mt-1 z-50 bg-white border border-gray-200 rounded-lg shadow-lg w-80"
      >
        <!-- Header -->
        <div class="flex items-center justify-between px-3 py-2 border-b border-gray-100">
          <span class="text-xs font-semibold text-gray-600 uppercase tracking-wide">AI Memory</span>
          <span class="text-xs text-gray-400 tabular-nums">
            {{ data.total === 0 ? 'empty' : `${data.total} note${data.total === 1 ? '' : 's'}` }}
            <span v-if="loading" class="ml-1 animate-pulse">↻</span>
          </span>
        </div>

        <!-- Notes list -->
        <ul class="divide-y divide-gray-50 max-h-72 overflow-y-auto">
          <li
            v-for="note in data.notes"
            :key="note.key"
            class="px-3 py-2 hover:bg-gray-50"
          >
            <div class="flex items-baseline justify-between gap-2 mb-0.5">
              <span class="text-xs font-mono font-semibold text-indigo-700 truncate max-w-[10rem]" :title="note.key">{{ note.key }}</span>
              <span class="text-[10px] text-gray-400 shrink-0 tabular-nums">{{ formatTime(note.updated_at) }}</span>
            </div>
            <p class="text-xs text-gray-700 break-words leading-snug">{{ note.content }}</p>
          </li>
          <li v-if="data.notes.length === 0 && !loading" class="px-3 py-4 text-xs text-gray-400 text-center">
            No notes yet — the AI writes here while playing.
          </li>
        </ul>

        <!-- Pagination -->
        <div v-if="data.pages > 1" class="flex items-center justify-between px-3 py-2 border-t border-gray-100">
          <button
            class="text-xs px-2 py-0.5 rounded border border-gray-200 text-gray-500 hover:bg-gray-100 disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
            :disabled="page <= 1"
            @click="prev"
          >← Prev</button>
          <span class="text-xs text-gray-400 tabular-nums">{{ page }} / {{ data.pages }}</span>
          <button
            class="text-xs px-2 py-0.5 rounded border border-gray-200 text-gray-500 hover:bg-gray-100 disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
            :disabled="page >= data.pages"
            @click="next"
          >Next →</button>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.drop-enter-active, .drop-leave-active { transition: opacity 0.12s, transform 0.12s; }
.drop-enter-from, .drop-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
