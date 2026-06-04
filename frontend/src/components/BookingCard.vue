<template>
  <div class="p-4 bg-white rounded-lg shadow-sm border">
    <div class="flex justify-between items-start">
      <div>
        <p class="font-medium">{{ booking.date }} at {{ booking.start_time }}</p>
        <p class="text-sm text-gray-500">{{ booking.duration_minutes }} min</p>
        <p v-if="booking.result_room" class="text-sm text-green-600">Room: {{ booking.result_room }}</p>
        <p v-if="booking.status === 'moved'" class="text-sm text-orange-600">⚠️ {{ booking.failure_reason || 'Booking was modified on Asimut' }}</p>
        <p v-else-if="booking.failure_reason" class="text-sm text-red-500">{{ booking.failure_reason }}</p>
      </div>
      <div class="flex items-center gap-2">
        <StatusBadge :status="booking.status" />
        <button v-if="booking.status === 'pending' || booking.status === 'scheduled'" @click="$emit('delete', booking.id)" class="text-red-400 hover:text-red-600">&#x2715;</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import StatusBadge from './StatusBadge.vue'
import type { BookingWish } from '../stores/bookings'

defineProps<{ booking: BookingWish }>()
defineEmits<{ delete: [id: string] }>()
</script>
