<script setup lang="ts">
defineProps<{ remaining: number; max: number }>()
</script>

<template>
  <div class="flex items-center gap-2 text-sm text-gray-600">
    <span>Mistakes remaining:</span>
    <div class="flex gap-1.5">
      <span
        v-for="i in max"
        :key="i"
        :class="[
          'w-3.5 h-3.5 rounded-full inline-block transition-all duration-300',
          i <= remaining ? 'bg-[#5a594e]' : 'bg-[#d0d0c7]',
        ]"
      />
    </div>
  </div>
</template>
