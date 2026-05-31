<script setup lang="ts">
import { useGameStore } from '../stores/game'
import Tile from './Tile.vue'
import SolvedGroup from './SolvedGroup.vue'
import MistakeDots from './MistakeDots.vue'
import Controls from './Controls.vue'

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
      class="mt-4 text-center text-2xl font-bold text-green-600 pop-in"
    >
      Solved! 🎉
    </div>
    <div
      v-else-if="store.status === 'lost'"
      class="mt-4 text-center text-2xl font-bold text-red-500 pop-in"
    >
      Better luck next time!
    </div>

    <!-- Mistakes + controls -->
    <template v-if="store.status === 'playing' || store.status === 'lost'">
      <MistakeDots
        class="mt-2"
        :remaining="store.mistakesLeft"
        :max="store.maxMistakes"
      />
      <Controls />
    </template>
    <template v-else-if="store.status === 'won'">
      <Controls />
    </template>

    <!-- Toast -->
    <Transition name="fade">
      <div
        v-if="store.toast"
        class="fixed top-6 left-1/2 -translate-x-1/2 bg-black text-white px-4 py-2 rounded-full text-sm font-semibold shadow-lg"
      >
        {{ store.toast }}
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.3s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
