import { Transport } from './transport.js'
import { generateKey, encrypt, decryptToString } from './crypto.js'
import { detectContentKind, setupClipboardHandler, copyToClipboard } from './clipboard.js'
import { getDeviceLabel } from './device.js'
import { renderItem, showNotification, updateDeviceList, updateStatusBar, setupDragDrop, completeFileTransfer } from './ui.js'
import { sendFile, handleFileMeta, handleFileChunk, handleFileComplete } from './stream.js'

// State
let state = 'INIT' // INIT, CREATING, JOINING, CONNECTED, DISCONNECTED, RECONNECTING
let roomCode = null
let encryptionKey = null
let deviceLabel = getDeviceLabel()
let devices = []

const transport = new Transport()

// DOM refs
const landingView = document.getElementById('landing-view')
const roomView = document.getElementById('room-view')
const roomCodeEl = document.getElementById('room-code')
const headerRoomCode = document.getElementById('header-room-code')
const joinInput = document.getElementById('join-input')
const joinBtn = document.getElementById('join-btn')
const copyLinkBtn = document.getElementById('copy-link-btn')
const openRoomBtn = document.getElementById('open-room-btn')
const headerCopyBtn = document.getElementById('header-copy-btn')
const headerQrBtn = document.getElementById('header-qr-btn')
const qrModal = document.getElementById('qr-modal')
const qrModalCloseBtn = document.getElementById('qr-modal-close-btn')
const headerSettingsBtn = document.getElementById('header-settings-btn')
const settingsModal = document.getElementById('settings-modal')
const settingsCloseBtn = document.getElementById('settings-close-btn')
const settingsSaveBtn = document.getElementById('settings-save-btn')
const settingsDeviceLabel = document.getElementById('settings-device-label')
const inputField = document.getElementById('input-field')
const sendBtn = document.getElementById('send-btn')
const attachBtn = document.getElementById('attach-btn')
const fileInput = document.getElementById('file-input')

// Initialize
function init() {
  const path = window.location.pathname
  const hash = window.location.hash.slice(1) // remove #

  if (path.startsWith('/r/') && hash) {
    // Join existing room
    roomCode = path.slice(3).split('/')[0]
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
  transport.on('connected', () => {
    state = 'CONNECTED'
    updateStatusBar('connected')

    // Send join with device label
    transport.send({
      type: 'join',
      payload: { room_code: roomCode, device_label: deviceLabel },
      ts: Date.now()
    })
  })

  transport.on('disconnected', () => {
    state = 'DISCONNECTED'
    updateStatusBar('disconnected')
  })

  transport.on('reconnecting', () => {
    state = 'RECONNECTING'
    updateStatusBar('reconnecting')
  })

  transport.on('joined', (env) => {
    showNotification('Connected to room', 'success')
  })

  transport.on('device_list', (env) => {
    const payload = env.payload
    devices = payload.devices || []
    updateDeviceList(devices)
  })

  transport.on('item', (env) => {
    const payload = env.payload
    try {
      const text = decryptToString(payload.encrypted_data, payload.nonce, encryptionKey)
      const kind = payload.kind || detectContentKind(text)
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
        device_label: 'Peer'
      })
      showNotification(`File received: ${fileInfo.file_name}`, 'success')
    })
  })

  transport.on('error', (env) => {
    const payload = env.payload
    showNotification(payload.message || 'An error occurred', 'error')
  })
}

function sendTextItem(text) {
  if (!text.trim() || state !== 'CONNECTED') return

  const kind = detectContentKind(text)
  const { encrypted, nonce } = encrypt(text, encryptionKey)
  const itemId = crypto.randomUUID ? crypto.randomUUID() : Math.random().toString(36).slice(2)

  transport.send({
    type: 'item',
    payload: {
      item_id: itemId,
      kind,
      encrypted_data: encrypted,
      nonce,
      ttl: 1800,
      device_label: deviceLabel
    },
    ts: Date.now()
  })

  // Render locally
  renderItem({
    item_id: itemId,
    kind,
    text,
    device_label: deviceLabel + ' (you)',
    ts: Date.now()
  })
}

async function sendFiles(files) {
  for (const file of files) {
    const fileId = await sendFile(file, encryptionKey, transport, deviceLabel)

    // Upgrade the progress card to show final preview
    const blobUrl = URL.createObjectURL(file)
    const isImage = file.type.startsWith('image/')
    completeFileTransfer(fileId, {
      kind: isImage ? 'image' : 'file',
      file_name: file.name,
      file_size: file.size,
      blob_url: blobUrl,
      device_label: deviceLabel + ' (you)'
    })
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

openRoomBtn.addEventListener('click', () => {
  window.location.href = buildRoomLink()
})

joinBtn.addEventListener('click', () => {
  const code = joinInput.value.trim()
  if (!code) return

  // Check if it's a full URL
  if (code.includes('/r/') && code.includes('#')) {
    window.location.href = code
    return
  }

  // Just a room code — can't join without encryption key
  showNotification('Please use a full room link (with encryption key)', 'error')
})

joinInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') joinBtn.click()
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
  settingsModal.classList.add('modal-backdrop--active')
})

settingsCloseBtn.addEventListener('click', () => {
  settingsModal.classList.remove('modal-backdrop--active')
})

settingsSaveBtn.addEventListener('click', () => {
  const label = settingsDeviceLabel.value.trim()
  if (label) {
    deviceLabel = label
    // Re-send join to update label
    transport.send({
      type: 'join',
      payload: { room_code: roomCode, device_label: deviceLabel },
      ts: Date.now()
    })
  }
  settingsModal.classList.remove('modal-backdrop--active')
  showNotification('Settings saved', 'success')
})

sendBtn.addEventListener('click', () => {
  const text = inputField.innerText.trim()
  if (text) {
    sendTextItem(text)
    inputField.innerHTML = ''
  }
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
  if (text) document.execCommand('insertText', false, text)
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

    // Determine colors based on color scheme
    const isDark = window.matchMedia('(prefers-color-scheme: dark)').matches
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
  }

  // Register service worker
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').catch(() => {})
    navigator.serviceWorker.addEventListener('message', (event) => {
      if (event.data.type === 'share-target' && state === 'CONNECTED') {
        if (event.data.text) sendTextItem(event.data.text)
        if (event.data.file) sendFiles([event.data.file])
      }
    })
  }

  init()
}

boot()
