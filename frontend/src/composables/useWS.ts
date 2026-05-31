import { ref, watch, onUnmounted } from 'vue'
import { useGameStore } from '../stores/game'
import type { WSEvent } from '../types/game'

export function useWS() {
  const store = useGameStore()
  const ws = ref<WebSocket | null>(null)
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null

  function connect(sessionId: string) {
    if (!sessionId) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/ws?session=${sessionId}`
    ws.value = new WebSocket(url)

    ws.value.onmessage = (e) => {
      try {
        const event: WSEvent = JSON.parse(e.data)
        store.handleWSEvent(event.type, event.payload)
      } catch { /* ignore malformed */ }
    }

    ws.value.onclose = () => {
      // Reconnect after 2s if game still active
      if (store.status === 'playing') {
        reconnectTimer = setTimeout(() => connect(sessionId), 2000)
      }
    }

    ws.value.onerror = () => ws.value?.close()
  }

  function disconnect() {
    if (reconnectTimer) clearTimeout(reconnectTimer)
    ws.value?.close()
    ws.value = null
  }

  // Auto-connect when session is created
  watch(() => store.sessionId, (id) => {
    disconnect()
    if (id) connect(id)
  }, { immediate: true })

  onUnmounted(disconnect)

  return { connect, disconnect }
}
