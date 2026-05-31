<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useGameStore } from './stores/game'
import { useWS } from './composables/useWS'
import GameBoard from './components/GameBoard.vue'
import SessionSelector from './components/SessionSelector.vue'

const store = useGameStore()
useWS()

const error = ref<string>('')
const loading = ref(true)

onMounted(async () => {
  try {
    await store.init()
  } catch (e) {
    error.value = 'Could not connect to server. Is the backend running?'
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="min-h-screen bg-white font-sans">
    <!-- Header -->
    <header class="border-b border-gray-200">
      <div class="max-w-xl mx-auto px-4 py-4 flex items-center gap-3 flex-wrap">
        <h1 class="text-3xl font-black tracking-tight">Connections</h1>
        <span data-testid="puzzle-date" class="text-gray-500 text-base">{{ store.puzzleDate }}</span>
        <div v-if="!loading && !error" class="ml-auto">
          <SessionSelector />
        </div>
      </div>
    </header>

    <main class="py-8">
      <div v-if="loading" class="flex justify-center items-center h-48 text-gray-400">
        Loading puzzle…
      </div>
      <div v-else-if="error" class="flex justify-center items-center h-48 text-red-500 text-sm px-4 text-center">
        {{ error }}
      </div>
      <GameBoard v-else />
    </main>
  </div>
</template>
