<script setup lang="ts">
import { useGameStore } from '../stores/game'
import Tile from './Tile.vue'
import SolvedGroup from './SolvedGroup.vue'
import MistakeDots from './MistakeDots.vue'
import Controls from './Controls.vue'
import AttemptsLog from './AttemptsLog.vue'
import StatsPanel from './StatsPanel.vue'

const store = useGameStore()
</script>

<template>
  <div class="flex flex-col items-center gap-2 w-full max-w-xl mx-auto px-2">
    <p class="text-sm text-gray-600 mb-1">Create four groups of four!</p>

    <!-- Solved groups -->
    <div class="w-full flex flex-col gap-2">
      <SolvedGroup
        v-for="group in store.solved"
        :key="group.title"
        :group="group"
      />
    </div>

    <!-- Tile grid -->
    <div
      v-if="store.remaining.length > 0"
      class="w-full grid grid-cols-4 gap-2"
    >
      <Tile
        v-for="word in store.remaining"
        :key="word"
        :word="word"
        :selected="store.selected.includes(word)"
        :guessing="store.guessingTiles.includes(word)"
        :shaking="store.shakingTiles.includes(word)"
        :disabled="
          store.status !== 'playing' ||
          (!store.selected.includes(word) && store.selected.length >= 4)
        "
        @click="store.toggleTile(word)"
      />
    </div>

    <!-- End state message -->
    <div
      v-if="store.status === 'won'"
      data-testid="game-status-won"
      class="mt-4 text-center text-2xl font-bold text-green-600 pop-in"
    >
      Solved! 🎉
    </div>
    <div
      v-else-if="store.status === 'lost'"
      data-testid="game-status-lost"
      class="mt-4 text-center text-2xl font-bold text-red-500 pop-in"
    >
      Better luck next time!
    </div>

    <!-- Mistakes + play controls (only while playing) -->
    <template v-if="store.status === 'playing'">
      <MistakeDots
        class="mt-2"
        :remaining="store.mistakesLeft"
        :max="store.maxMistakes"
      />
      <Controls />
    </template>

    <!-- Session controls: prev / restart / next (always available) -->
    <div class="flex items-center justify-center gap-3 mt-3">
      <button
        class="px-4 py-1.5 rounded-full border border-gray-300 text-xs font-semibold text-gray-700 hover:bg-gray-100 transition-colors"
        data-testid="btn-prev-inline"
        @click="store.prev()"
      >
        ← Previous
      </button>
      <button
        class="px-4 py-1.5 rounded-full border border-gray-300 text-xs font-semibold text-gray-700 hover:bg-gray-100 transition-colors"
        data-testid="btn-restart-inline"
        @click="store.restart()"
      >
        ↺ Restart
      </button>
      <button
        class="px-4 py-1.5 rounded-full border border-gray-300 text-xs font-semibold text-gray-700 hover:bg-gray-100 transition-colors"
        data-testid="btn-next-inline"
        @click="store.next()"
      >
        Next Puzzle →
      </button>
      <button
        v-if="store.finished && store.stats"
        class="px-4 py-1.5 rounded-full border border-gray-300 text-xs font-semibold text-gray-700 hover:bg-gray-100 transition-colors"
        data-testid="btn-view-stats"
        @click="store.showStats = true"
      >
        📊 Stats
      </button>
    </div>

    <!-- Live attempts log (player / AI / API) -->
    <AttemptsLog />

    <!-- Toast -->
    <Transition name="fade">
      <div
        v-if="store.toast"
        data-testid="toast"
        class="fixed top-6 left-1/2 -translate-x-1/2 bg-black text-white px-4 py-2 rounded-full text-sm font-semibold shadow-lg"
      >
        {{ store.toast }}
      </div>
    </Transition>

    <!-- End-of-game stats overlay -->
    <StatsPanel />
  </div>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.3s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
