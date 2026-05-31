<script setup lang="ts">
import { useGameStore } from '../stores/game'
const store = useGameStore()
</script>

<template>
  <div class="flex items-center justify-center gap-3 mt-4">
    <button
      class="px-5 py-2 rounded-full border border-gray-400 text-sm font-semibold hover:bg-gray-100 transition-colors"
      :disabled="store.status !== 'playing'"
      @click="store.shuffle()"
    >
      Shuffle
    </button>
    <button
      class="px-5 py-2 rounded-full border border-gray-400 text-sm font-semibold hover:bg-gray-100 transition-colors"
      :disabled="store.selected.length === 0 || store.status !== 'playing'"
      @click="store.deselectAll()"
    >
      Deselect All
    </button>
    <button
      :class="[
        'px-5 py-2 rounded-full text-sm font-semibold transition-colors',
        store.canSubmit
          ? 'bg-[#1a1a1a] text-white hover:bg-[#333]'
          : 'bg-gray-200 text-gray-400 cursor-not-allowed',
      ]"
      :disabled="!store.canSubmit"
      @click="store.submitGuess()"
    >
      Submit
    </button>
  </div>
</template>
