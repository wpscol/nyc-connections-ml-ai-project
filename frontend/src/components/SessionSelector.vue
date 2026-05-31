<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useGameStore } from '../stores/game'

const store = useGameStore()
const open = ref(false)
const root = ref<HTMLElement | null>(null)

const statusEmoji: Record<string, string> = {
  playing: '🟡',
  won: '🟢',
  lost: '🔴',
}

const current = computed(() => store.sessions.find(s => s.session_id === store.sessionId))

function label(s: { status: string; puzzle_date: string; session_id: string }) {
  return `${statusEmoji[s.status] ?? '⚪'} ${s.puzzle_date || '—'} · ${s.session_id.slice(0, 8)}`
}

function select(id: string) {
  open.value = false
  if (id !== store.sessionId) store.switchSession(id)
}

async function remove(e: MouseEvent, id: string) {
  e.stopPropagation()
  await store.deleteSession(id)
  if (store.sessions.length === 0) open.value = false
}

async function clearAll() {
  open.value = false
  await store.clearAllSessions()
}

function onOutsideClick(e: MouseEvent) {
  if (root.value && !root.value.contains(e.target as Node)) open.value = false
}

onMounted(() => document.addEventListener('mousedown', onOutsideClick))
onUnmounted(() => document.removeEventListener('mousedown', onOutsideClick))
</script>

<template>
  <div ref="root" class="relative flex items-center gap-2">
    <!-- Trigger button -->
    <button
      data-testid="session-selector-trigger"
      class="flex items-center gap-1 text-xs font-mono border border-gray-300 rounded px-2 py-1 bg-white text-gray-700 hover:bg-gray-50 focus:outline-none focus:ring-1 focus:ring-gray-400 max-w-[16rem] truncate"
      @click="open = !open"
    >
      <span class="truncate">{{ current ? label(current) : 'Select session' }}</span>
      <svg class="w-3 h-3 ml-1 shrink-0 transition-transform" :class="{ 'rotate-180': open }" viewBox="0 0 20 20" fill="currentColor">
        <path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd"/>
      </svg>
    </button>

    <!-- + New -->
    <button
      data-testid="btn-new-session"
      class="text-xs px-2 py-1 rounded border border-gray-300 text-gray-600 hover:bg-gray-100 transition-colors whitespace-nowrap"
      @click="store.newGame()"
    >
      + New
    </button>

    <!-- Dropdown -->
    <Transition name="drop">
      <div
        v-if="open"
        class="absolute right-0 top-full mt-1 z-50 bg-white border border-gray-200 rounded-lg shadow-lg min-w-[16rem] max-w-[22rem]"
      >
        <!-- Session rows -->
        <ul class="max-h-64 overflow-y-auto divide-y divide-gray-100">
          <li
            v-for="s in store.sessions"
            :key="s.session_id"
            class="flex items-center gap-2 px-3 py-2 hover:bg-gray-50 cursor-pointer group"
            :class="{ 'bg-gray-100 font-semibold': s.session_id === store.sessionId }"
            @click="select(s.session_id)"
          >
            <span class="flex-1 text-xs font-mono truncate">{{ label(s) }}</span>
            <button
              :data-testid="`btn-delete-session-${s.session_id.slice(0, 8)}`"
              class="opacity-0 group-hover:opacity-100 ml-1 shrink-0 text-gray-400 hover:text-red-500 transition-opacity focus:opacity-100"
              title="Remove session"
              @click="remove($event, s.session_id)"
            >
              ✕
            </button>
          </li>
          <li v-if="store.sessions.length === 0" class="px-3 py-2 text-xs text-gray-400">
            No sessions
          </li>
        </ul>

        <!-- Clear All footer -->
        <div class="border-t border-gray-100 px-3 py-2">
          <button
            data-testid="btn-clear-all-sessions"
            class="w-full text-xs text-red-500 hover:text-red-700 hover:bg-red-50 rounded px-2 py-1 transition-colors text-left"
            @click="clearAll"
          >
            🗑 Clear all &amp; new game
          </button>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.drop-enter-active, .drop-leave-active { transition: opacity 0.12s, transform 0.12s; }
.drop-enter-from, .drop-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
