<script setup lang="ts">
import { computed } from 'vue'
import { useGameStore } from '../stores/game'

const store = useGameStore()

const won = computed(() => store.status === 'won')
const s = computed(() => store.stats)

// Build a per-attempt result string for a quick visual recap (✓/~/✗)
const recap = computed(() =>
  store.attempts.map(a => (a.correct ? '✓' : a.one_away ? '~' : '✗')).join(' ')
)
</script>

<template>
  <Transition name="stats-fade">
    <div
      v-if="store.showStats"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4"
      data-testid="stats-panel"
      @click.self="store.showStats = false"
    >
      <div class="overlay-in bg-white rounded-2xl shadow-2xl w-full max-w-sm p-6 text-center">
        <div
          class="text-3xl font-black mb-1"
          :class="won ? 'text-green-600' : 'text-red-500'"
          data-testid="stats-result"
        >
          {{ won ? 'Solved! 🎉' : 'Out of mistakes' }}
        </div>
        <p class="text-sm text-gray-500 mb-5">{{ store.puzzleDate }}</p>

        <div v-if="s" class="grid grid-cols-2 gap-3 mb-5">
          <div class="rounded-lg bg-gray-50 py-3">
            <div class="text-2xl font-black" data-testid="stat-groups">{{ s.groups_solved }}/4</div>
            <div class="text-xs text-gray-500 uppercase tracking-wide">Groups</div>
          </div>
          <div class="rounded-lg bg-gray-50 py-3">
            <div class="text-2xl font-black" data-testid="stat-mistakes">{{ s.mistakes }}/{{ s.max_mistakes }}</div>
            <div class="text-xs text-gray-500 uppercase tracking-wide">Mistakes</div>
          </div>
          <div class="rounded-lg bg-gray-50 py-3">
            <div class="text-2xl font-black" data-testid="stat-guesses">{{ s.total_guesses }}</div>
            <div class="text-xs text-gray-500 uppercase tracking-wide">Guesses</div>
          </div>
          <div class="rounded-lg bg-gray-50 py-3">
            <div class="text-2xl font-black" data-testid="stat-accuracy">{{ s.accuracy }}%</div>
            <div class="text-xs text-gray-500 uppercase tracking-wide">Accuracy</div>
          </div>
        </div>

        <div v-if="recap" class="text-lg tracking-widest mb-5 text-gray-700">{{ recap }}</div>

        <div class="flex gap-2">
          <button
            class="flex-1 px-3 py-2.5 rounded-full border border-gray-400 text-sm font-semibold hover:bg-gray-100 transition-colors"
            data-testid="btn-prev"
            @click="store.prev()"
          >
            ← Prev
          </button>
          <button
            class="flex-1 px-3 py-2.5 rounded-full border border-gray-400 text-sm font-semibold hover:bg-gray-100 transition-colors"
            data-testid="btn-restart"
            @click="store.restart()"
          >
            Restart
          </button>
          <button
            class="flex-1 px-3 py-2.5 rounded-full bg-[#1a1a1a] text-white text-sm font-semibold hover:bg-[#333] transition-colors"
            data-testid="btn-next"
            @click="store.next()"
          >
            Next →
          </button>
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.stats-fade-enter-active, .stats-fade-leave-active { transition: opacity 0.25s; }
.stats-fade-enter-from, .stats-fade-leave-to { opacity: 0; }
</style>
