<template>
  <div class="p-4 bg-white rounded-lg shadow-sm border">
    <div class="flex justify-between items-start">
      <div>
        <p class="font-medium">{{ booking.date }} at {{ booking.start_time }}</p>
        <p class="text-sm text-gray-500">{{ booking.duration_minutes }} min</p>
        <p v-if="booking.result_room" class="text-sm text-green-600">Room: {{ booking.result_room }}</p>
        <p v-if="booking.status === 'moved'" class="text-sm text-orange-600">⚠️ {{ booking.failure_reason || 'Booking was modified on Asimut' }}</p>
        <p v-else-if="booking.failure_reason" class="text-sm text-red-500">{{ booking.failure_reason }}</p>
        <button v-if="hasLog" @click="toggleLog" class="mt-1 text-xs text-blue-600 hover:underline">
          {{ showLog ? 'Hide details' : 'Show details' }}
        </button>
      </div>
      <div class="flex items-center gap-2">
        <StatusBadge :status="booking.status" />
        <button v-if="booking.status === 'pending' || booking.status === 'scheduled'" @click="$emit('delete', booking.id)" class="text-red-400 hover:text-red-600">&#x2715;</button>
      </div>
    </div>
    <div v-if="showLog" class="mt-3 border-t pt-3 text-xs">
      <p v-if="logLoading" class="text-gray-500">Loading…</p>
      <p v-else-if="logError" class="text-red-500">{{ logError }}</p>
      <p v-else-if="!log.length" class="text-gray-500">No execution log recorded for this booking.</p>
      <ol v-else class="space-y-2">
        <li v-for="e in log" :key="e.id" class="flex gap-2">
          <span class="text-gray-400 tabular-nums shrink-0">{{ time(e.timestamp) }}</span>
          <div class="min-w-0">
            <p :class="stepClass(e.step)">{{ e.message }}</p>
            <pre v-if="e.detail" class="mt-0.5 whitespace-pre-wrap break-words font-sans text-gray-500">{{ e.detail }}</pre>
          </div>
        </li>
      </ol>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import StatusBadge from './StatusBadge.vue'
import { api } from '../api'
import type { BookingWish, ExecLogEntry } from '../stores/bookings'

const props = defineProps<{ booking: BookingWish }>()
defineEmits<{ delete: [id: string] }>()

const showLog = ref(false)
const log = ref<ExecLogEntry[]>([])
const logLoading = ref(false)
const logError = ref('')

// Only bookings that have run (or are running) have an execution log.
const hasLog = computed(() => !['pending', 'scheduled'].includes(props.booking.status))

async function toggleLog() {
  showLog.value = !showLog.value
  if (!showLog.value) return
  logLoading.value = true
  logError.value = ''
  try {
    log.value = await api.getBookingLog(props.booking.id)
  } catch (e) {
    logError.value = e instanceof Error ? e.message : 'Failed to load log'
  } finally {
    logLoading.value = false
  }
}

// SQLite timestamps are UTC ("YYYY-MM-DD HH:MM:SS") without a zone marker.
function time(ts: string): string {
  const d = new Date(ts.includes('T') ? ts : ts.replace(' ', 'T') + 'Z')
  return isNaN(d.getTime()) ? ts : d.toLocaleTimeString('de-DE', { timeZone: 'Europe/Berlin' })
}

function stepClass(step: string): string {
  if (['failed', 'room_failed', 'extend_failed', 'error'].includes(step)) return 'text-red-600'
  if (['room_diagnosis', 'blocker_followup', 'blocker_followup_scheduled', 'timing_retry'].includes(step)) return 'text-orange-600'
  if (['room_booked', 'extend_ok'].includes(step)) return 'text-green-600'
  return 'text-gray-700'
}
</script>
