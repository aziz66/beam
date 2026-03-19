import { formatTimeAgo, formatBytes, renderTextContent, renderLinkPreview, highlightCode, detectLanguage } from './preview.js'
import { copyToClipboard } from './clipboard.js'

const feed = document.getElementById('feed')
const feedEmpty = document.getElementById('feed-empty')
const dropOverlay = document.getElementById('drop-overlay')
const toastContainer = document.getElementById('toast-container')

let autoScroll = true

export function renderItem(item) {
  feedEmpty.style.display = 'none'

  const card = document.createElement('div')
  card.className = 'item'
  card.dataset.itemId = item.item_id || ''

  const header = document.createElement('div')
  header.className = 'item__header'

  const kind = document.createElement('span')
  kind.className = 'item__kind'
  kind.textContent = item.kind || 'text'

  const meta = document.createElement('span')
  meta.className = 'item__meta'
  meta.textContent = `${item.device_label || 'Unknown'}`

  header.appendChild(kind)
  header.appendChild(meta)
  card.appendChild(header)

  const content = document.createElement('div')
  content.className = 'item__content'

  switch (item.kind) {
    case 'link':
      content.appendChild(renderLinkPreview(item.text))
      break
    case 'code': {
      content.className += ' item__content--code'
      const lang = detectLanguage(item.text)
      content.innerHTML = highlightCode(item.text, lang)
      break
    }
    case 'image':
      if (item.blob_url) {
        const img = document.createElement('img')
        img.className = 'item__image'
        img.src = item.blob_url
        img.alt = 'Shared image'
        content.appendChild(img)
      }
      break
    case 'file':
      content.innerHTML = `<strong>${escapeHtml(item.file_name || 'File')}</strong> <span style="color:var(--text-secondary)">${formatBytes(item.file_size || 0)}</span>`
      break
    default:
      content.innerHTML = renderTextContent(item.text || '')
  }

  card.appendChild(content)

  // Actions
  const actions = document.createElement('div')
  actions.className = 'item__actions'

  if (item.kind === 'text' || item.kind === 'code' || item.kind === 'link') {
    const copyBtn = document.createElement('button')
    copyBtn.className = 'btn btn--secondary btn--small'
    copyBtn.textContent = 'Copy'
    copyBtn.addEventListener('click', async () => {
      const ok = await copyToClipboard(item.text)
      if (ok) showNotification('Copied!', 'success')
    })
    actions.appendChild(copyBtn)
  }

  if (item.kind === 'link') {
    const openBtn = document.createElement('button')
    openBtn.className = 'btn btn--secondary btn--small'
    openBtn.textContent = 'Open'
    openBtn.addEventListener('click', () => {
      window.open(item.text, '_blank', 'noopener')
    })
    actions.appendChild(openBtn)
  }

  if (item.kind === 'file' && item.blob_url) {
    const dlBtn = document.createElement('button')
    dlBtn.className = 'btn btn--primary btn--small'
    dlBtn.textContent = 'Download'
    dlBtn.addEventListener('click', () => {
      const a = document.createElement('a')
      a.href = item.blob_url
      a.download = item.file_name || 'download'
      a.click()
    })
    actions.appendChild(dlBtn)
  }

  if (item.kind === 'image' && item.blob_url) {
    const dlBtn = document.createElement('button')
    dlBtn.className = 'btn btn--primary btn--small'
    dlBtn.textContent = 'Download'
    dlBtn.addEventListener('click', () => {
      const a = document.createElement('a')
      a.href = item.blob_url
      a.download = item.file_name || 'image.png'
      a.click()
    })
    actions.appendChild(dlBtn)
  }

  card.appendChild(actions)
  feed.appendChild(card)

  if (autoScroll) {
    feed.scrollTop = feed.scrollHeight
  }
}

export function renderFileProgress(fileId, fileName, fileSize, progress) {
  let card = feed.querySelector(`[data-file-id="${fileId}"]`)
  if (!card) {
    feedEmpty.style.display = 'none'
    card = document.createElement('div')
    card.className = 'item'
    card.dataset.fileId = fileId
    card.innerHTML = `
      <div class="item__header">
        <span class="item__kind">file</span>
        <span class="item__meta"></span>
      </div>
      <div class="item__content">
        <strong>${escapeHtml(fileName)}</strong>
        <span style="color:var(--text-secondary)">${formatBytes(fileSize)}</span>
      </div>
      <div class="item__progress">
        <div class="item__progress-bar"><div class="item__progress-fill" style="width:0%"></div></div>
        <div class="item__progress-text">0%</div>
      </div>`
    feed.appendChild(card)
  }

  const fill = card.querySelector('.item__progress-fill')
  const text = card.querySelector('.item__progress-text')
  if (fill) fill.style.width = `${Math.round(progress * 100)}%`
  if (text) text.textContent = `${Math.round(progress * 100)}%`

  if (autoScroll) feed.scrollTop = feed.scrollHeight
}

export function showDropOverlay() {
  dropOverlay.classList.add('drop-overlay--active')
}

export function hideDropOverlay() {
  dropOverlay.classList.remove('drop-overlay--active')
}

export function showNotification(text, type = 'success') {
  const toast = document.createElement('div')
  toast.className = `toast toast--${type}`
  toast.textContent = text
  toastContainer.appendChild(toast)
  setTimeout(() => {
    toast.remove()
  }, 3000)
}

export function updateDeviceList(devices) {
  const el = document.getElementById('header-devices')
  const count = devices.length
  const dots = devices.map(() => '<span class="room__device-dot"></span>').join('')
  el.innerHTML = `${count} device${count !== 1 ? 's' : ''} <span class="room__devices-dots">${dots}</span>`
}

export function updateStatusBar(state) {
  const bar = document.getElementById('status-bar')
  bar.className = 'status-bar'
  if (state === 'disconnected') {
    bar.classList.add('status-bar--disconnected')
    bar.textContent = 'Disconnected'
  } else if (state === 'reconnecting') {
    bar.classList.add('status-bar--reconnecting')
    bar.textContent = 'Reconnecting...'
  } else {
    bar.style.display = 'none'
  }
}

export function setupDragDrop(onFiles) {
  let dragCounter = 0

  document.addEventListener('dragenter', (e) => {
    e.preventDefault()
    dragCounter++
    showDropOverlay()
  })

  document.addEventListener('dragleave', (e) => {
    e.preventDefault()
    dragCounter--
    if (dragCounter <= 0) {
      dragCounter = 0
      hideDropOverlay()
    }
  })

  document.addEventListener('dragover', (e) => {
    e.preventDefault()
  })

  document.addEventListener('drop', (e) => {
    e.preventDefault()
    dragCounter = 0
    hideDropOverlay()
    const files = Array.from(e.dataTransfer.files)
    if (files.length > 0) onFiles(files)
  })
}

// Track scroll position
feed.addEventListener('scroll', () => {
  const atBottom = feed.scrollHeight - feed.scrollTop - feed.clientHeight < 50
  autoScroll = atBottom
})

function escapeHtml(str) {
  const div = document.createElement('div')
  div.textContent = str
  return div.innerHTML
}
