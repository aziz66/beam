const serverUrl = document.getElementById('server-url')
const roomCode = document.getElementById('room-code')
const encryptionKey = document.getElementById('encryption-key')
const autoSync = document.getElementById('auto-sync')
const connectBtn = document.getElementById('connect-btn')
const statusDot = document.getElementById('status-dot')
const statusText = document.getElementById('status-text')

connectBtn.addEventListener('click', () => {
  const config = {
    server_url: serverUrl.value,
    room_code: roomCode.value,
    encryption_key: encryptionKey.value,
    auto_sync: autoSync.checked
  }

  if (!config.room_code || !config.encryption_key) {
    statusText.textContent = 'Room code and key required'
    return
  }

  // Send config to Tauri backend
  if (window.__TAURI__) {
    window.__TAURI__.invoke('connect_room', { config })
      .then(() => {
        statusDot.className = 'status-dot status-dot--connected'
        statusText.textContent = 'Connected'
      })
      .catch((err) => {
        statusText.textContent = 'Error: ' + err
      })
  } else {
    statusText.textContent = 'Not running in Tauri'
  }
})

// Listen for status updates from backend
if (window.__TAURI__) {
  window.__TAURI__.event.listen('connection-status', (event) => {
    const connected = event.payload === 'connected'
    statusDot.className = connected
      ? 'status-dot status-dot--connected'
      : 'status-dot status-dot--disconnected'
    statusText.textContent = connected ? 'Connected' : 'Disconnected'
  })
}
