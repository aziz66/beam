const RECONNECT_BASE = 1000
const RECONNECT_MAX = 30000
const PING_INTERVAL = 25000
const PONG_TIMEOUT = 10000
const CONNECT_TIMEOUT = 5000 // watchdog: give up on CONNECTING after this long

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
    this.watchdogTimer = null
    this.state = 'disconnected'
    this.intentionalClose = false
  }

  connect(roomCode) {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    this.url = `${proto}//${location.host}/ws/${roomCode}`
    this.intentionalClose = false
    this._connect()

    // iOS Safari may load the page in the background and suspend network activity
    // before the WebSocket handshake completes. The socket gets stuck in CONNECTING
    // state indefinitely — onclose/onerror never fire, so auto-retry never kicks in.
    //
    // We register multiple events to catch every way iOS can bring a page to the
    // foreground, and force a fresh connection whenever we detect we're visible
    // but not connected.
    const forceReconnect = () => {
      if (!this.url || this.intentionalClose) return
      if (this.ws && this.ws.readyState === WebSocket.OPEN) return
      clearTimeout(this.reconnectTimer)
      clearTimeout(this.watchdogTimer)
      this.reconnectDelay = RECONNECT_BASE
      if (this.ws) {
        this.ws.onopen = null
        this.ws.onclose = null
        this.ws.onerror = null
        this.ws.onmessage = null
        this.ws.close()
        this.ws = null
      }
      this._connect()
    }

    // visibilitychange: page moved to foreground
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') forceReconnect()
    })

    // focus: window/tab received focus (complements visibilitychange on some iOS versions)
    window.addEventListener('focus', forceReconnect)

    // pageshow with persisted=true: Safari restored page from bfcache (back/forward),
    // visibilitychange does NOT fire in this case
    window.addEventListener('pageshow', (e) => {
      if (e.persisted) forceReconnect()
    })
  }

  _connect() {
    if (this.ws) {
      this.ws.onopen = null
      this.ws.onclose = null
      this.ws.onerror = null
      this.ws.onmessage = null
      this.ws.close()
    }

    try {
      this.ws = new WebSocket(this.url)
    } catch (err) {
      // Invalid URL or browser blocked the connection entirely
      this.state = 'reconnecting'
      this._emit('reconnecting')
      this._scheduleReconnect()
      return
    }

    this.state = 'connecting'

    // Watchdog: if the socket is still CONNECTING after CONNECT_TIMEOUT ms,
    // something (iOS network suspension, a silent browser block, etc.) has frozen
    // the handshake. Abandon it and try again from scratch.
    clearTimeout(this.watchdogTimer)
    this.watchdogTimer = setTimeout(() => {
      if (this.ws && this.ws.readyState === WebSocket.CONNECTING) {
        this.ws.onopen = null
        this.ws.onclose = null
        this.ws.onerror = null
        this.ws.onmessage = null
        this.ws.close()
        this.ws = null
        if (!this.intentionalClose) {
          this.state = 'reconnecting'
          this._emit('reconnecting')
          this._scheduleReconnect()
        }
      }
    }, CONNECT_TIMEOUT)

    this.ws.onopen = () => {
      clearTimeout(this.watchdogTimer)
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
      clearTimeout(this.watchdogTimer)
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
      // onclose will fire after this — watchdog covers the case where it doesn't
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
    clearTimeout(this.watchdogTimer)
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
