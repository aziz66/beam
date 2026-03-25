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
    this._iosListenersRegistered = false

    // Bind once so the same function reference is used for add/remove
    this._onVisibility = () => {
      if (document.visibilityState === 'visible') this._forceReconnect()
    }
    this._onFocus = () => this._forceReconnect()
    this._onPageShow = (e) => { if (e.persisted) this._forceReconnect() }
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
    // Register once — guards against connect() being called multiple times.
    if (!this._iosListenersRegistered) {
      this._iosListenersRegistered = true
      document.addEventListener('visibilitychange', this._onVisibility)
      window.addEventListener('focus', this._onFocus)
      window.addEventListener('pageshow', this._onPageShow)
    }
  }

  _forceReconnect() {
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
      // Null out ws before scheduling reconnect so _forceReconnect (which may
      // fire concurrently from visibilitychange) cannot open a second connection.
      this.ws = null
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
      if (this.queue.length >= 200) {
        this.queue.shift() // drop oldest to prevent unbounded growth
      }
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
    if (this._iosListenersRegistered) {
      document.removeEventListener('visibilitychange', this._onVisibility)
      window.removeEventListener('focus', this._onFocus)
      window.removeEventListener('pageshow', this._onPageShow)
      this._iosListenersRegistered = false
    }
  }

  _flushQueue() {
    while (this.queue.length > 0 && this.ws && this.ws.readyState === WebSocket.OPEN) {
      const msg = this.queue[0]
      try {
        this.ws.send(msg)
        this.queue.shift()
      } catch {
        break
      }
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
        // Clear any prior pong timer before setting a new one — without this,
        // the overwritten timer becomes a ghost that fires and closes the socket.
        clearTimeout(this.pongTimer)
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
