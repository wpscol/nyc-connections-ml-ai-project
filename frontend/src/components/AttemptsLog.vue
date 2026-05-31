<script setup lang="ts">
import { computed } from 'vue'
import { useGameStore } from '../stores/game'
import { DIFFICULTY_HEX, SOURCE_LABEL } from '../types/game'

const store = useGameStore()

// newest first
const ordered = computed(() => [...store.attempts].slice().reverse())

function rowColor(correct: boolean, oneAway: boolean, difficulty: number): string {
  if (correct) return DIFFICULTY_HEX[difficulty] ?? '#22c55e'
  if (oneAway) return '#f59e0b'
  return '#9ca3af'
}
</script>

<template>
  <div v-if="store.attempts.length" class="w-full mt-4" data-testid="attempts-log">
    <div class="flex items-center justify-between mb-1.5">
      <h2 class="text-xs font-bold uppercase tracking-wide text-gray-500">Attempts</h2>
      <span class="text-xs text-gray-400">{{ store.attempts.length }}</span>
    </div>

    <TransitionGroup name="attempt" tag="ul" class="flex flex-col gap-1.5">
      <li
        v-for="(a, i) in ordered"
        :key="store.attempts.length - i"
        data-testid="attempt-row"
        class="flex items-center gap-2 text-xs"
      >
        <!-- source badge -->
        <span
          class="shrink-0 px-1.5 py-0.5 rounded font-semibold text-[10px] bg-gray-100 text-gray-600 w-20 text-center"
          :data-source="a.source"
        >
          {{ SOURCE_LABEL[a.source] ?? a.source }}
        </span>

        <!-- guessed words -->
        <div class="flex flex-wrap gap-1 flex-1">
          <span
            v-for="w in a.words"
            :key="w"
            class="px-1.5 py-0.5 rounded text-[10px] font-bold uppercase text-white"
            :style="{ backgroundColor: rowColor(a.correct, a.one_away, a.difficulty) }"
          >
            {{ w }}
          </span>
        </div>

        <!-- result icon -->
        <span class="shrink-0 w-5 text-center font-bold" :data-result="a.correct ? 'correct' : 'wrong'">
          <template v-if="a.correct">✓</template>
          <template v-else-if="a.one_away">~</template>
          <template v-else>✗</template>
        </span>
      </li>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.attempt-enter-active { transition: all 0.3s ease; }
.attempt-enter-from { opacity: 0; transform: translateX(-12px); }
</style>
