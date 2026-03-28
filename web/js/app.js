import { Transport } from './transport.js'
import { generateKey, encrypt, decryptToString } from './crypto.js'
import { detectContentKind, setupClipboardHandler, copyToClipboard } from './clipboard.js'
import { getDeviceLabel } from './device.js'
import { renderItem, showNotification, updateDeviceList, updateStatusBar, setupDragDrop, completeFileTransfer } from './ui.js'
import { sendFile, handleFileMeta, handleFileChunk, handleFileComplete } from './stream.js'
import { WebRTCManager } from './webrtc.js'

// State
let state = 'INIT' // INIT, CREATING, JOINING, CONNECTED, DISCONNECTED, RECONNECTING
let roomCode = null
let encryptionKey = null
let deviceLabel = getDeviceLabel()
let devices = []
let ttlMinutes = 30
let myDeviceId = null
let roomPassphrase = ''

const transport = new Transport()
const webrtc = new WebRTCManager(transport)

// DOM refs
const landingView = document.getElementById('landing-view')
const roomView = document.getElementById('room-view')
const roomCodeEl = document.getElementById('room-code')
const headerRoomCode = document.getElementById('header-room-code')
const joinInput = document.getElementById('join-input')
const joinBtn = document.getElementById('join-btn')
const copyLinkBtn = document.getElementById('copy-link-btn')
const openRoomBtn = document.getElementById('open-room-btn')
const headerAgentBtn = document.getElementById('header-agent-btn')
const headerCopyBtn = document.getElementById('header-copy-btn')
const headerQrBtn = document.getElementById('header-qr-btn')
const qrModal = document.getElementById('qr-modal')
const qrModalCloseBtn = document.getElementById('qr-modal-close-btn')
const headerSettingsBtn = document.getElementById('header-settings-btn')
const settingsModal = document.getElementById('settings-modal')
const settingsCloseBtn = document.getElementById('settings-close-btn')
const settingsSaveBtn = document.getElementById('settings-save-btn')
const settingsDeviceLabel = document.getElementById('settings-device-label')
const settingsTtl = document.getElementById('settings-ttl')
const inputField = document.getElementById('input-field')
const sendBtn = document.getElementById('send-btn')
const attachBtn = document.getElementById('attach-btn')
const fileInput = document.getElementById('file-input')
const landingThemeBtn = document.getElementById('landing-theme-btn')
const roomThemeBtn = document.getElementById('room-theme-btn')
const pinToggle = document.getElementById('pin-toggle')
const pinPassphraseWrap = document.getElementById('pin-passphrase-wrap')
const pinPassphrase = document.getElementById('pin-passphrase')
const passphraseModal = document.getElementById('passphrase-modal')
const passphraseInput = document.getElementById('passphrase-input')
const passphraseSubmitBtn = document.getElementById('passphrase-submit-btn')

// ── Theme ──────────────────────────────────────────────────────────────────

const SUN_ICON = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>'
const MOON_ICON = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>'

function initTheme() {
  const saved = localStorage.getItem('beam-theme')
  if (saved) document.documentElement.setAttribute('data-theme', saved)
  updateThemeIcons()
}

function isDarkActive() {
  const forced = document.documentElement.getAttribute('data-theme')
  if (forced) return forced === 'dark'
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

function toggleTheme() {
  const next = isDarkActive() ? 'light' : 'dark'
  document.documentElement.setAttribute('data-theme', next)
  localStorage.setItem('beam-theme', next)
  updateThemeIcons()
  // Re-render the landing QR so its colors match the new theme
  if (roomCode && landingView && landingView.classList.contains('view--active')) {
    renderQR(buildRoomLink())
  }
}

function updateThemeIcons() {
  const dark = isDarkActive()
  if (landingThemeBtn) landingThemeBtn.innerHTML = dark ? SUN_ICON : MOON_ICON
  if (roomThemeBtn) roomThemeBtn.innerHTML = dark ? SUN_ICON : MOON_ICON
}

landingThemeBtn.addEventListener('click', toggleTheme)
roomThemeBtn.addEventListener('click', toggleTheme)

// ── Initialize
function init() {
  const path = window.location.pathname
  const hash = window.location.hash.slice(1) // remove #

  if (path.startsWith('/r/') && hash) {
    // Join existing room — trim trailing slash before extracting code
    roomCode = path.slice(3).replace(/\/$/, '').split('/')[0]
    encryptionKey = hash
    joinRoom()
  } else {
    // Show landing with new room
    showLanding()
  }
}

function showLanding() {
  state = 'CREATING'
  encryptionKey = generateKey()
  roomCode = null

  // Generate a room code by creating a temporary room via the server
  // For now, just generate one client-side that matches the pattern
  roomCode = generateClientCode()

  const link = buildRoomLink()
  roomCodeEl.textContent = roomCode

  renderQR(link)

  landingView.classList.add('view--active')
  roomView.classList.remove('view--active')
}

function generateClientCode() {
  // Use crypto.getRandomValues for unpredictable room codes
  const adj = ['amber', 'azure', 'bold', 'calm', 'coral', 'crisp', 'dawn', 'deep', 'fair', 'fresh', 'frost', 'gentle', 'golden', 'jade', 'keen', 'light', 'lunar', 'mist', 'noble', 'opal', 'pine', 'quick', 'rose', 'sage', 'silk', 'snow', 'swift', 'teal', 'warm', 'wild']
  const noun = ['anchor', 'birch', 'brook', 'cedar', 'cloud', 'crane', 'delta', 'eagle', 'fern', 'flame', 'grove', 'hawk', 'lake', 'maple', 'moon', 'oak', 'peak', 'rain', 'reef', 'river', 'sage', 'shell', 'star', 'stone', 'storm', 'tiger', 'trail', 'wave', 'wind', 'wolf']
  const rnd = new Uint32Array(3)
  crypto.getRandomValues(rnd)
  const a = adj[rnd[0] % adj.length]
  const n = noun[rnd[1] % noun.length]
  const num = 10 + (rnd[2] % 90)
  return `${a}-${n}-${num}`
}

function generateSecureId() {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
}

function buildRoomLink() {
  return `${window.location.origin}/r/${roomCode}#${encryptionKey}`
}

function joinRoom() {
  state = 'JOINING'
  landingView.classList.remove('view--active')
  roomView.classList.add('view--active')
  headerRoomCode.textContent = roomCode

  setupTransport()
  transport.connect(roomCode)
}

function setupTransport() {
  // Route WebRTC data-channel messages through the same handlers as WebSocket
  webrtc.onMessage = (env) => {
    const handler = transport.handlers.get(env.type)
    if (handler) handler(env)
  }

  transport.on('connected', () => {
    state = 'CONNECTED'
    updateStatusBar('connected')

    // Send join with device label and passphrase (passphrase is required for protected rooms)
    transport.send({
      type: 'join',
      payload: { room_code: roomCode, device_label: deviceLabel, passphrase: roomPassphrase },
      ts: Date.now()
    })
  })

  transport.on('disconnected', () => {
    state = 'DISCONNECTED'
    updateStatusBar('disconnected')
    webrtc.close()
  })

  transport.on('reconnecting', () => {
    state = 'RECONNECTING'
    updateStatusBar('reconnecting')
    webrtc.close()
  })

  transport.on('joined', (env) => {
    myDeviceId = env.payload && env.payload.device_id ? env.payload.device_id : null
    showNotification('Connected to room', 'success')
  })

  transport.on('device_list', (env) => {
    const payload = env.payload
    devices = payload.devices || []
    updateDeviceList(devices)
    if (myDeviceId) webrtc.tryConnect(devices, myDeviceId)
  })

  transport.on('item', (env) => {
    const payload = env.payload
    // Deduplicate: server may relay the same item twice on reconnect
    if (payload.item_id && document.querySelector(`[data-item-id="${payload.item_id}"]`)) return
    try {
      const text = decryptToString(payload.encrypted_data, payload.nonce, encryptionKey)
      // Re-detect kind locally from decrypted content — do NOT trust the sender's
      // claimed kind field, which could be used to force a javascript: URL through
      // renderLinkPreview by claiming kind='link' for malicious text.
      const kind = detectContentKind(text)
      renderItem({
        item_id: payload.item_id,
        kind,
        text,
        device_label: payload.device_label,
        ts: env.ts
      })
    } catch (err) {
      console.error('decrypt failed:', err)
      renderItem({
        item_id: payload.item_id,
        kind: 'text',
        text: '[decryption failed]',
        device_label: payload.device_label,
        ts: env.ts
      })
    }
  })

  transport.on('file_meta', (env) => {
    handleFileMeta(env.payload, encryptionKey)
  })

  transport.on('file_chunk', (env) => {
    handleFileChunk(env.payload, encryptionKey)
  })

  transport.on('file_complete', (env) => {
    handleFileComplete(env.payload, (fileInfo) => {
      const imageExts = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.svg']
      const ext = (fileInfo.file_name || '').toLowerCase().replace(/.*(\.\w+)$/, '$1')
      const isImage = imageExts.includes(ext)

      completeFileTransfer(fileInfo.file_id, {
        kind: isImage ? 'image' : 'file',
        file_name: fileInfo.file_name,
        file_size: fileInfo.file_size,
        blob_url: fileInfo.blob_url,
        device_label: fileInfo.device_label || 'Peer'
      })
      showNotification(`File received: ${fileInfo.file_name}`, 'success')
    })
  })

  transport.on('error', (env) => {
    const payload = env.payload
    if (payload && payload.code === 'auth_required') {
      // Stop all auto-reconnect attempts while the user types their passphrase.
      // The server closes the connection right after sending this error, which
      // would normally trigger _scheduleReconnect(). Setting intentionalClose
      // makes the imminent onclose emit 'disconnected' instead of retrying —
      // preventing the modal from being interrupted or re-shown mid-typing.
      clearTimeout(transport.reconnectTimer)
      transport.intentionalClose = true
      if (passphraseInput) passphraseInput.value = ''
      if (passphraseModal) passphraseModal.classList.add('modal-backdrop--active')
      if (passphraseInput) passphraseInput.focus()
      return
    }
    showNotification((payload && payload.message) || 'An error occurred', 'error')
  })
}

function sendTextItem(text) {
  if (!text.trim() || state !== 'CONNECTED') return

  const kind = detectContentKind(text)
  const { encrypted, nonce } = encrypt(text, encryptionKey)
  const itemId = typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : generateSecureId()

  const envelope = {
    type: 'item',
    payload: {
      item_id: itemId,
      kind,
      encrypted_data: encrypted,
      nonce,
      ttl: ttlMinutes * 60,
      device_label: deviceLabel
    },
    ts: Date.now()
  }
  if (!webrtc.send(envelope)) {
    transport.send(envelope)
  }

  // Render locally
  renderItem({
    item_id: itemId,
    kind,
    text,
    device_label: deviceLabel + ' (you)',
    is_mine: true,
    ts: Date.now()
  })
}

async function sendFiles(files) {
  const MAX_FILES_PER_SEND = 10
  if (files.length > MAX_FILES_PER_SEND) {
    showNotification(`Select at most ${MAX_FILES_PER_SEND} files at a time`, 'error')
    return
  }
  for (const file of files) {
    let blobUrl = null
    try {
      const fileId = await sendFile(file, encryptionKey, transport, deviceLabel)

      // Upgrade the progress card to show final preview
      blobUrl = URL.createObjectURL(file)
      const isImage = file.type.startsWith('image/')
      completeFileTransfer(fileId, {
        kind: isImage ? 'image' : 'file',
        file_name: file.name,
        file_size: file.size,
        blob_url: blobUrl,
        device_label: deviceLabel + ' (you)',
        is_mine: true
      })
    } catch (err) {
      // Revoke blob URL if created before the error so it doesn't leak
      if (blobUrl) URL.revokeObjectURL(blobUrl)
      console.error('send file failed:', err)
      showNotification('Failed to send file', 'error')
    }
  }
}

// Event listeners
roomCodeEl.addEventListener('click', async () => {
  const ok = await copyToClipboard(buildRoomLink())
  if (ok) showNotification('Link copied!', 'success')
})

copyLinkBtn.addEventListener('click', async () => {
  const ok = await copyToClipboard(buildRoomLink())
  if (ok) showNotification('Link copied!', 'success')
})

openRoomBtn.addEventListener('click', async () => {
  if (pinToggle && pinToggle.checked) {
    const pass = pinPassphrase ? pinPassphrase.value.trim() : ''
    const body = { pinned: true }
    if (pass) body.passphrase = pass
    try {
      const resp = await fetch('/api/rooms', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      })
      if (!resp.ok) throw new Error('server error')
      const data = await resp.json()
      if (!data.room_code) throw new Error('missing room_code')
      roomCode = data.room_code
      roomCodeEl.textContent = roomCode
      renderQR(buildRoomLink())
    } catch {
      showNotification('Failed to pin room', 'error')
      return
    }
  }
  window.location.href = buildRoomLink()
})

joinBtn.addEventListener('click', () => {
  const code = joinInput.value.trim()
  if (!code) return

  // Check if it's a full URL — validate same-origin to prevent open redirect
  if (code.includes('/r/') && code.includes('#')) {
    try {
      const parsed = new URL(code)
      if (parsed.origin !== window.location.origin) {
        showNotification('Invalid room link — must be from this server', 'error')
        return
      }
      window.location.href = parsed.href
    } catch {
      showNotification('Invalid URL', 'error')
    }
    return
  }

  // Just a room code — can't join without encryption key
  showNotification('Please use a full room link (with encryption key)', 'error')
})

joinInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') joinBtn.click()
})

headerAgentBtn.addEventListener('click', () => {
  // Build a beam:// (or beams:// for TLS) deep-link URL.
  // Use a hidden iframe — unlike anchor clicks or location.href, iframes with
  // custom scheme URLs never fire beforeunload on the parent page, so the
  // WebSocket stays open while the OS protocol handler launches the agent.
  const scheme = window.location.protocol === 'https:' ? 'beams' : 'beam'
  const agentUrl = `${scheme}://${window.location.host}/${roomCode}#${encryptionKey}`
  const iframe = document.createElement('iframe')
  iframe.style.cssText = 'display:none;width:0;height:0;border:0;position:absolute'
  iframe.src = agentUrl
  document.body.appendChild(iframe)
  setTimeout(() => { if (iframe.parentNode) iframe.parentNode.removeChild(iframe) }, 2000)
})

headerCopyBtn.addEventListener('click', async () => {
  const ok = await copyToClipboard(buildRoomLink())
  if (ok) showNotification('Link copied!', 'success')
})

headerRoomCode.addEventListener('click', async () => {
  const ok = await copyToClipboard(buildRoomLink())
  if (ok) showNotification('Link copied!', 'success')
})

headerQrBtn.addEventListener('click', () => {
  const link = buildRoomLink()
  renderQR(link, document.getElementById('qr-modal-canvas'))
  document.getElementById('qr-modal-link').textContent = link
  qrModal.classList.add('modal-backdrop--active')
})

qrModalCloseBtn.addEventListener('click', () => {
  qrModal.classList.remove('modal-backdrop--active')
})

headerSettingsBtn.addEventListener('click', () => {
  settingsDeviceLabel.value = deviceLabel
  settingsTtl.value = ttlMinutes
  settingsModal.classList.add('modal-backdrop--active')
})

settingsCloseBtn.addEventListener('click', () => {
  settingsModal.classList.remove('modal-backdrop--active')
})

function submitPassphrase() {
  const pass = passphraseInput ? passphraseInput.value.trim() : ''
  if (!pass) return
  roomPassphrase = pass
  if (passphraseModal) passphraseModal.classList.remove('modal-backdrop--active')
  // Re-enable auto-reconnect (was paused while the modal was open) then
  // force a fresh connection so the join is sent with the new passphrase.
  transport.intentionalClose = false
  transport._forceReconnect()
}

if (passphraseSubmitBtn) {
  passphraseSubmitBtn.addEventListener('click', submitPassphrase)
}
if (passphraseInput) {
  passphraseInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') submitPassphrase()
  })
}

settingsSaveBtn.addEventListener('click', () => {
  const rawLabel = settingsDeviceLabel.value.trim()
  if (rawLabel) {
    // Truncate to 64 chars (server enforces this too, but cap client-side for UX)
    deviceLabel = rawLabel.slice(0, 64)
    // Re-send join to update label (include passphrase for protected rooms)
    transport.send({
      type: 'join',
      payload: { room_code: roomCode, device_label: deviceLabel, passphrase: roomPassphrase },
      ts: Date.now()
    })
  }
  const parsedTtl = parseInt(settingsTtl.value, 10)
  if (!isNaN(parsedTtl) && parsedTtl >= 1 && parsedTtl <= 1440) {
    ttlMinutes = parsedTtl
  } else if (settingsTtl.value.trim() !== '') {
    showNotification('TTL must be between 1 and 1440 minutes', 'error')
    return
  }
  settingsModal.classList.remove('modal-backdrop--active')
  showNotification('Settings saved', 'success')
})

sendBtn.addEventListener('click', () => {
  const text = inputField.innerText.trim()
  if (!text) return
  // Guard against very large pastes that would block the main thread during encryption
  if (text.length > 200_000) {
    showNotification('Text too large to send (max 200 000 chars)', 'error')
    return
  }
  sendTextItem(text)
  inputField.innerHTML = ''
})

inputField.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendBtn.click()
  }
})

// Force plain-text paste in the input field so that URLs copied from browsers
// (which Chrome packages as rich HTML with the page title as link text) arrive
// as the raw URL string rather than the rendered title.
inputField.addEventListener('paste', (e) => {
  e.preventDefault()
  const text = e.clipboardData.getData('text/plain')
  if (text) {
    const sel = window.getSelection()
    const range = sel && sel.rangeCount > 0 ? sel.getRangeAt(0) : null
    if (range) {
      range.deleteContents()
      range.insertNode(document.createTextNode(text))
      range.collapse(false)
      sel.removeAllRanges()
      sel.addRange(range)
    }
  }
})

attachBtn.addEventListener('click', () => fileInput.click())
fileInput.addEventListener('change', () => {
  const files = Array.from(fileInput.files)
  if (files.length > 0) sendFiles(files)
  fileInput.value = ''
})

// Clipboard handler (paste anywhere)
setupClipboardHandler(
  (text) => { if (state === 'CONNECTED') sendTextItem(text) },
  (imageFile) => { if (state === 'CONNECTED') sendFiles([imageFile]) },
  (files) => { if (state === 'CONNECTED') sendFiles(files) }
)

// Drag and drop
setupDragDrop((files) => {
  if (state === 'CONNECTED') sendFiles(files)
})

// QR code rendering
function renderQR(text, targetCanvas) {
  const canvas = targetCanvas || document.getElementById('qr-canvas')
  if (typeof qrcode === 'undefined') return
  try {
    const qr = qrcode(0, 'M')
    qr.addData(text)
    qr.make()
    const size = window.innerWidth <= 640 ? 140 : 180
    const modules = qr.getModuleCount()
    const cellSize = size / modules
    const ctx = canvas.getContext('2d')
    canvas.width = size
    canvas.height = size

    // Determine colors based on color scheme (respects manual theme override)
    const isDark = isDarkActive()
    ctx.fillStyle = isDark ? '#1e1e3a' : '#ffffff'
    ctx.fillRect(0, 0, size, size)
    ctx.fillStyle = isDark ? '#e4e4e7' : '#1d1d1f'

    for (let row = 0; row < modules; row++) {
      for (let col = 0; col < modules; col++) {
        if (qr.isDark(row, col)) {
          ctx.fillRect(col * cellSize, row * cellSize, cellSize + 0.5, cellSize + 0.5)
        }
      }
    }
  } catch (err) {
    console.error('QR generation failed:', err)
  }
}

// Load vendored scripts then init
function loadScript(src) {
  return new Promise((resolve, reject) => {
    const s = document.createElement('script')
    s.src = src
    s.onload = resolve
    s.onerror = reject
    document.head.appendChild(s)
  })
}

async function boot() {
  try {
    await loadScript('/lib/tweetnacl.min.js')
    await loadScript('/lib/tweetnacl-util.min.js')
    await loadScript('/lib/qrcode.min.js')
  } catch (err) {
    console.error('Failed to load libraries:', err)
    // Encryption libraries are required — show a hard error and stop.
    document.body.innerHTML = '<div style="padding:2rem;font-family:sans-serif">Failed to load required libraries. Please reload the page.</div>'
    return
  }

  // Register service worker
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').catch(() => {})
    navigator.serviceWorker.addEventListener('message', (event) => {
      if (event.data.type === 'share-target' && state === 'CONNECTED') {
        if (event.data.text) sendTextItem(event.data.text)
        if (event.data.file) sendFiles([event.data.file]).catch(err => {
          console.error('share-target sendFiles failed:', err)
          showNotification('Failed to share file', 'error')
        })
      }
    })
  }

  // Clean up WebRTC and WebSocket on page unload to release peer connections promptly
  window.addEventListener('beforeunload', () => {
    webrtc.close()
    transport.close()
  })

  initTheme()
  init()
}

pinToggle.addEventListener('change', () => {
  pinPassphraseWrap.style.display = pinToggle.checked ? '' : 'none'
})

// Modal keyboard handling: Escape closes, Tab cycles focus within active modal
document.addEventListener('keydown', (e) => {
  // Find the currently open modal (if any)
  const activeModal = [qrModal, settingsModal, passphraseModal].find(
    m => m && m.classList.contains('modal-backdrop--active')
  )

  if (e.key === 'Escape') {
    if (activeModal) activeModal.classList.remove('modal-backdrop--active')
    return
  }

  if (e.key === 'Tab' && activeModal) {
    const focusable = Array.from(
      activeModal.querySelectorAll('button, input, select, textarea, [tabindex]:not([tabindex="-1"])')
    ).filter(el => !el.disabled && el.offsetParent !== null)
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (e.shiftKey) {
      if (document.activeElement === first) {
        e.preventDefault()
        last.focus()
      }
    } else {
      if (document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
  }
})

boot()
