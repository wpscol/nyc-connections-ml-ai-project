<script setup lang="ts">
import type { SolvedGroup } from '../types/game'
import { DIFFICULTY_COLORS } from '../types/game'

const props = defineProps<{ group: SolvedGroup }>()
const color = DIFFICULTY_COLORS[props.group.difficulty] ?? DIFFICULTY_COLORS[0]
</script>

<template>
  <div
    :class="[
      'w-full rounded-md flex flex-col items-center justify-center py-3 px-2 pop-in',
      color.bg, color.text,
    ]"
    style="min-height:3.5rem"
  >
    <p class="font-black text-sm tracking-wider uppercase">{{ group.title }}</p>
    <p class="text-sm mt-0.5">{{ group.words.join(', ') }}</p>
  </div>
</template>
