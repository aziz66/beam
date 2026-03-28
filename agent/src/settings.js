const serverUrl = document.getElementById('server-url')
const roomCode = document.getElementById('room-code')
const encryptionKey = document.getElementById('encryption-key')
const passphrase = document.getElementById('passphrase')
const autoSync = document.getElementById('auto-sync')
const connectBtn = document.getElementById('connect-btn')
const statusDot = document.getElementById('status-dot')
const statusText = document.getElementById('status-text')

function setStatus(connected, message) {
  statusDot.className = connected
    ? 'status-dot status-dot--connected'
    : 'status-dot status-dot--disconnected'
  statusText.textContent = message
}

function invoke(cmd, args) {
  return window.__TAURI__.invoke(cmd, args || {})
}

window.addEventListener('load', () => {
  if (!window.__TAURI__?.invoke) {
    setStatus(false, 'ERROR: No IPC bridge')
    return
  }
  setStatus(false, 'Ready')

  // Pre-fill form from saved config
  invoke('get_config')
    .then((config) => {
      if (!config) return
      if (config.server_url) serverUrl.value = config.server_url
      if (config.room_code) roomCode.value = config.room_code
      if (config.encryption_key) encryptionKey.value = config.encryption_key
      if (config.passphrase) passphrase.value = config.passphrase
      if (typeof config.auto_sync === 'boolean') autoSync.checked = config.auto_sync
    })
    .catch(() => {})

  // Detect Beam room URLs copied to clipboard while disconnected and prefill form.
  // Pattern: http(s)://<host>/r/<room-code>#<key>  — any domain, any port.
  const beamUrlRe = /^(https?:\/\/[^\s/]+)\/r\/([\w]+-[\w]+-\d+)#(.+)$/
  if (window.__TAURI__?.event) {
    window.__TAURI__.event.listen('clipboard-change', ({ payload: text }) => {
      invoke('get_status').then((status) => {
        if (status === 'connected') return
        if (typeof text !== 'string') return
        const m = text.trim().match(beamUrlRe)
        if (!m) return
        serverUrl.value = m[1]
        roomCode.value = m[2]
        encryptionKey.value = m[3]
        const notice = document.getElementById('paste-notice')
        notice.classList.add('paste-notice--visible')
        clearTimeout(notice._hideTimer)
        notice._hideTimer = setTimeout(() => notice.classList.remove('paste-notice--visible'), 5000)
      }).catch(() => {})
    })
  }
})

connectBtn.addEventListener('click', () => {
  const passphraseVal = passphrase.value.trim()
  const config = {
    server_url: serverUrl.value.trim(),
    room_code: roomCode.value.trim(),
    encryption_key: encryptionKey.value.trim(),
    auto_sync: autoSync.checked,
    ...(passphraseVal ? { passphrase: passphraseVal } : {})
  }

  if (!config.room_code || !config.encryption_key) {
    setStatus(false, 'Room code and key required')
    return
  }

  // Validate key is 32-byte base64 before even attempting to connect.
  // The URL fragment uses standard base64 (not URL-safe), but accept both by
  // normalising '+'/'-' and '/'/'._ before calling atob.
  try {
    const normalised = config.encryption_key.replace(/-/g, '+').replace(/_/g, '/')
    const raw = atob(normalised)
    if (raw.length !== 32) {
      setStatus(false, `Key must encode 32 bytes (got ${raw.length})`)
      return
    }
  } catch {
    setStatus(false, 'Encryption key must be base64 (copy from the room URL fragment)')
    return
  }

  setStatus(false, 'Connecting...')

  invoke('connect_room', { config })
    .then(() => {}) // status updates via polling
    .catch((err) => setStatus(false, 'Error: ' + JSON.stringify(err)))
})

// Poll connection status every second — skip when window is not visible
setInterval(() => {
  if (document.visibilityState !== 'visible') return
  invoke('get_status')
    .then((status) => {
      const connected = status === 'connected'
      setStatus(connected, connected ? 'Connected' : status)
    })
    .catch(() => {})
}, 1000)
