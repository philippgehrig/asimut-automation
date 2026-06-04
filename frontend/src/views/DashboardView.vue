<template>
  <div class="max-w-lg mx-auto p-4 space-y-4">
    <div class="flex justify-between items-center">
      <h1 class="text-xl font-bold">Bookings</h1>
      <router-link to="/create" class="px-4 py-2 text-sm text-white bg-blue-600 rounded-lg hover:bg-blue-700">+ New</router-link>
    </div>
    <nav class="flex gap-2 text-sm">
      <router-link to="/rooms" class="text-blue-600 hover:underline">Rooms</router-link>
      <router-link to="/settings" class="text-blue-600 hover:underline">Settings</router-link>
    </nav>
    <div v-if="bookingsStore.loading" class="text-gray-500">Loading...</div>
    <div v-else class="space-y-2">
      <BookingCard v-for="booking in paginatedBookings" :key="booking.id" :booking="booking" @delete="bookingsStore.remove($event)" />
      <p v-if="bookingsStore.bookings.length === 0" class="text-gray-500 text-center py-8">No bookings yet</p>
      <div v-if="totalPages > 1" class="flex justify-center items-center gap-2 pt-2">
        <button @click="page--" :disabled="page <= 1" class="px-3 py-1 text-sm rounded border disabled:opacity-30">&laquo;</button>
        <span class="text-sm text-gray-600">{{ page }} / {{ totalPages }}</span>
        <button @click="page++" :disabled="page >= totalPages" class="px-3 py-1 text-sm rounded border disabled:opacity-30">&raquo;</button>
      </div>
    </div>
    <div v-if="recurrencesStore.recurrences.length > 0" class="pt-4 border-t">
      <h2 class="text-lg font-semibold mb-2">Recurring</h2>
      <div v-for="r in recurrencesStore.recurrences" :key="r.id" class="flex items-center justify-between p-3 bg-white border rounded-lg mb-2">
        <div>
          <p class="text-sm font-medium">{{ dayName(r.day_of_week) }} {{ r.start_time }}</p>
          <p class="text-xs text-gray-500">{{ r.duration_minutes }} min</p>
        </div>
        <div class="flex items-center gap-2">
          <button @click="recurrencesStore.toggleActive(r.id, !r.active)" :class="r.active ? 'bg-green-500' : 'bg-gray-300'" class="w-10 h-5 rounded-full relative">
            <span :class="r.active ? 'translate-x-5' : 'translate-x-0'" class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform"></span>
          </button>
          <button @click="recurrencesStore.remove(r.id)" class="text-red-400 hover:text-red-600 text-sm">&#x2715;</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useBookingsStore } from '../stores/bookings'
import { useRecurrencesStore } from '../stores/recurrences'
import BookingCard from '../components/BookingCard.vue'

const bookingsStore = useBookingsStore()
const recurrencesStore = useRecurrencesStore()

const PAGE_SIZE = 10
const page = ref(1)

const sortedBookings = computed(() =>
  [...bookingsStore.bookings].sort((a, b) => {
    const dateA = a.date + a.start_time
    const dateB = b.date + b.start_time
    return dateB.localeCompare(dateA)
  })
)

const totalPages = computed(() => Math.ceil(sortedBookings.value.length / PAGE_SIZE))

const paginatedBookings = computed(() => {
  const start = (page.value - 1) * PAGE_SIZE
  return sortedBookings.value.slice(start, start + PAGE_SIZE)
})

const dayNames = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
function dayName(d: number) { return dayNames[d] }

onMounted(() => {
  bookingsStore.fetch()
  recurrencesStore.fetch()
})
</script>
