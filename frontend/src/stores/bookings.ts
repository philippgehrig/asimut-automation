import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../api'

export interface BookingWish {
  id: string
  date: string
  start_time: string
  duration_minutes: number
  room_priorities: number[]
  recurrence_id?: string
  status: string
  result_room?: string
  result_duration?: number
  result_event_id?: number
  failure_reason?: string
  created_at: string
  updated_at: string
}

export const useBookingsStore = defineStore('bookings', () => {
  const bookings = ref<BookingWish[]>([])
  const loading = ref(false)

  async function fetch() {
    loading.value = true
    try {
      bookings.value = await api.getBookings()
    } finally {
      loading.value = false
    }
  }

  async function create(data: Partial<BookingWish>) {
    await api.createBooking(data)
    await fetch()
  }

  async function remove(id: string) {
    await api.deleteBooking(id)
    await fetch()
  }

  return { bookings, loading, fetch, create, remove }
})
