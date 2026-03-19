const RECONNECT_BASE = 1000
const RECONNECT_MAX = 30000
const PING_INTERVAL = 25000
const PONG_TIMEOUT = 10000

export class Transport {
  constructor() {
    this.ws = null
    this.url = null
    this.handlers = new Map()
    this.queue = []
    this.reconnectDelay = RECONNECT_BASE
    this.reconnectTimer = null
    this.pingTimer = null
    this.pongTimer = null
    this.state = 'disconnected'
    this.intentionalClose = false
  }

  connect(roomCode) {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    this.url = `${proto}//${location.host}/ws/${roomCode}`
    this.intentionalClose = false
    this._connect()

    // iOS Safari: when a page is loaded in the background (e.g. tapping a QR link
    // while another app is open), Safari runs scripts immediately but suspends all
    // network activity. The WebSocket gets stuck in CONNECTING state indefinitely.
    // When the page becomes visible, force a fresh connection if the socket is not
    // already OPEN — this handles CONNECTING, CLOSING, and CLOSED states.
    const onVisible = () => {
      if (document.visibilityState !== 'visible' || this.intentionalClose) return
      if (this.ws && this.ws.readyState === WebSocket.OPEN) return
      clearTimeout(this.reconnectTimer)
      this.reconnectDelay = RECONNECT_BASE
      if (this.ws) {
        this.ws.onclose = null
        this.ws.onerror = null
        this.ws.close()
        this.ws = null
      }
      this._connect()
    }

    document.addEventListener('visibilitychange', onVisible)

    // pageshow fires on bfcache restore (Safari back/forward navigation) where
    // visibilitychange does not fire. e.persisted === true means page came from cache.
    window.addEventListener('pageshow', (e) => {
      if (e.persisted) onVisible()
    })
  }

  _connect() {
    if (this.ws) {
      this.ws.onclose = null
      this.ws.close()
    }

    this.ws = new WebSocket(this.url)
    this.state = 'connecting'

    this.ws.onopen = () => {
      this.state = 'connected'
      this.reconnectDelay = RECONNECT_BASE
      this._emit('connected')
      this._flushQueue()
      this._startPing()
    }

    this.ws.onmessage = (event) => {
      let env
      try {
        env = JSON.parse(event.data)
      } catch {
        return
      }

      if (env.type === 'pong') {
        this._clearPongTimeout()
        return
      }

      const handler = this.handlers.get(env.type)
      if (handler) handler(env)
    }

    this.ws.onclose = () => {
      this._stopPing()
      if (this.intentionalClose) {
        this.state = 'disconnected'
        this._emit('disconnected')
        return
      }
      this.state = 'reconnecting'
      this._emit('reconnecting')
      this._scheduleReconnect()
    }

    this.ws.onerror = () => {
      // onclose will fire after this
    }
  }

  send(envelope) {
    const data = JSON.stringify(envelope)
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(data)
    } else {
      this.queue.push(data)
    }
  }

  on(type, handler) {
    this.handlers.set(type, handler)
  }

  close() {
    this.intentionalClose = true
    this._stopPing()
    clearTimeout(this.reconnectTimer)
    if (this.ws) this.ws.close()
  }

  _flushQueue() {
    while (this.queue.length > 0 && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(this.queue.shift())
    }
  }

  _scheduleReconnect() {
    clearTimeout(this.reconnectTimer)
    this.reconnectTimer = setTimeout(() => {
      this._connect()
    }, this.reconnectDelay)
    this.reconnectDelay = Math.min(this.reconnectDelay * 2, RECONNECT_MAX)
  }

  _startPing() {
    this._stopPing()
    this.pingTimer = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.send({ type: 'ping', payload: null, ts: Date.now() })
        this.pongTimer = setTimeout(() => {
          // No pong received, reconnect
          if (this.ws) this.ws.close()
        }, PONG_TIMEOUT)
      }
    }, PING_INTERVAL)
  }

  _stopPing() {
    clearInterval(this.pingTimer)
    this._clearPongTimeout()
  }

  _clearPongTimeout() {
    clearTimeout(this.pongTimer)
    this.pongTimer = null
  }

  _emit(event) {
    const handler = this.handlers.get(event)
    if (handler) handler()
  }
}
