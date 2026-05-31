<script setup lang="ts">
defineProps<{
  word: string
  selected: boolean
  shaking: boolean
  disabled: boolean
  guessing: boolean
}>()

defineEmits<{ click: [] }>()
</script>

<template>
  <button
    :data-testid="'tile'"
    :class="[
      'flex items-center justify-center rounded-md font-black text-sm tracking-wider uppercase',
      'h-14 w-full cursor-pointer select-none transition-colors duration-100',
      guessing
        ? 'bg-amber-500 text-white ring-2 ring-amber-300'
        : selected
          ? 'bg-[#5a594e] text-white'
          : 'bg-[#efefe6] text-[#1a1a1a] hover:bg-[#deded5]',
      shaking && 'shake',
      disabled && 'opacity-50 cursor-not-allowed',
    ]"
    :disabled="disabled"
    @click="$emit('click')"
  >
    {{ word }}
  </button>
</template>
