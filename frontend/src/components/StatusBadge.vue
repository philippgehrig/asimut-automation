<template>
  <span :class="classes" class="px-2 py-1 text-xs font-medium rounded-full">
    {{ label }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ status: string }>()

const label = computed(() => {
  const labels: Record<string, string> = {
    pending: 'Pending',
    scheduled: 'Scheduled',
    executing: 'Executing',
    booked: 'Booked',
    partially_booked: 'Partial',
    moved: 'Moved',
    failed: 'Failed',
  }
  return labels[props.status] || props.status
})

const classes = computed(() => {
  const map: Record<string, string> = {
    pending: 'bg-gray-100 text-gray-700',
    scheduled: 'bg-blue-100 text-blue-700',
    executing: 'bg-purple-100 text-purple-700',
    booked: 'bg-green-100 text-green-700',
    partially_booked: 'bg-yellow-100 text-yellow-700',
    moved: 'bg-orange-100 text-orange-700',
    failed: 'bg-red-100 text-red-700',
  }
  return map[props.status] || 'bg-gray-100 text-gray-700'
})
</script>
