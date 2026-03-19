import { formatTimeAgo, formatBytes, renderTextContent, renderLinkPreview, highlightCode, detectLanguage } from './preview.js'
import { copyToClipboard } from './clipboard.js'

const feed = document.getElementById('feed')
const feedEmpty = document.getElementById('feed-empty')
const dropOverlay = document.getElementById('drop-overlay')
const toastContainer = document.getElementById('toast-container')

let autoScroll = true
let activeFilter = 'all'
const isIOS = /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)

// Filter bar logic
document.querySelectorAll('.room__filter').forEach(btn => {
  btn.addEventListener('click', () => {
    const filter = btn.dataset.filter
    if (filter === activeFilter) {
      activeFilter = 'all'
    } else {
      activeFilter = filter
    }
    document.querySelectorAll('.room__filter').forEach(b => b.classList.remove('room__filter--active'))
    const activeBtn = document.querySelector(`.room__filter[data-filter="${activeFilter}"]`)
    if (activeBtn) activeBtn.classList.add('room__filter--active')
    applyFilter()
  })
})

function applyFilter() {
  const items = feed.querySelectorAll('.item[data-kind]')
  let visibleCount = 0
  items.forEach(item => {
    if (activeFilter === 'all' || item.dataset.kind === activeFilter) {
      item.style.display = ''
      visibleCount++
    } else {
      item.style.display = 'none'
    }
  })
  // Show empty message only if there are items but none match the filter
  const totalItems = items.length + feed.querySelectorAll('.item:not([data-kind])').length
  if (totalItems > 0 && visibleCount === 0) {
    feedEmpty.style.display = ''
    feedEmpty.textContent = 'No items match this filter'
  } else if (totalItems > 0) {
    feedEmpty.style.display = 'none'
  } else {
    feedEmpty.style.display = ''
    feedEmpty.textContent = 'Drop files or paste anything to share'
  }
}

export function renderItem(item) {
  feedEmpty.style.display = 'none'

  const card = document.createElement('div')
  card.className = 'item'
  card.dataset.itemId = item.item_id || ''
  card.dataset.kind = item.kind || 'text'

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
      if (isPdfFile(item.file_name) && item.blob_url && !isIOS) {
        const preview = document.createElement('iframe')
        preview.className = 'item__pdf-preview'
        preview.src = item.blob_url
        preview.title = item.file_name || 'PDF preview'
        content.appendChild(preview)
      } else if (isVideoFile(item.file_name) && item.blob_url) {
        const video = document.createElement('video')
        video.className = 'item__video-preview'
        video.src = item.blob_url
        video.controls = true
        video.preload = 'metadata'
        content.appendChild(video)
      } else if (isAudioFile(item.file_name) && item.blob_url) {
        const nameEl = document.createElement('div')
        nameEl.innerHTML = `<strong>${escapeHtml(item.file_name || 'File')}</strong> <span style="color:var(--text-secondary)">${formatBytes(item.file_size || 0)}</span>`
        content.appendChild(nameEl)
        const audio = document.createElement('audio')
        audio.className = 'item__audio-preview'
        audio.src = item.blob_url
        audio.controls = true
        audio.preload = 'metadata'
        content.appendChild(audio)
      } else {
        content.innerHTML = `<strong>${escapeHtml(item.file_name || 'File')}</strong> <span style="color:var(--text-secondary)">${formatBytes(item.file_size || 0)}</span>`
      }
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

  // Hide if it doesn't match the active filter
  if (activeFilter !== 'all' && card.dataset.kind !== activeFilter) {
    card.style.display = 'none'
  }

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
    card.dataset.kind = 'file'

    const isImage = isImageFile(fileName)
    const kindLabel = isImage ? 'image' : 'file'

    card.innerHTML = `
      <div class="item__header">
        <span class="item__kind">${kindLabel}</span>
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

export function completeFileTransfer(fileId, item) {
  const card = feed.querySelector(`[data-file-id="${fileId}"]`)
  if (!card) {
    // No progress card found, fall back to rendering a new item
    renderItem(item)
    return
  }

  // Determine the final kind
  const finalKind = item.kind || 'file'
  card.dataset.kind = finalKind

  // Rebuild the card content in-place
  card.innerHTML = ''

  // Header
  const header = document.createElement('div')
  header.className = 'item__header'
  const kindEl = document.createElement('span')
  kindEl.className = 'item__kind'
  kindEl.textContent = finalKind
  const meta = document.createElement('span')
  meta.className = 'item__meta'
  meta.textContent = item.device_label || ''
  header.appendChild(kindEl)
  header.appendChild(meta)
  card.appendChild(header)

  // Content
  const content = document.createElement('div')
  content.className = 'item__content'

  if (finalKind === 'image' && item.blob_url) {
    const img = document.createElement('img')
    img.className = 'item__image'
    img.src = item.blob_url
    img.alt = item.file_name || 'Shared image'
    content.appendChild(img)
  } else if (isPdfFile(item.file_name) && item.blob_url) {
    const preview = document.createElement('iframe')
    preview.className = 'item__pdf-preview'
    preview.src = item.blob_url
    preview.title = item.file_name || 'PDF preview'
    content.appendChild(preview)
  } else if (isVideoFile(item.file_name) && item.blob_url) {
    const video = document.createElement('video')
    video.className = 'item__video-preview'
    video.src = item.blob_url
    video.controls = true
    video.preload = 'metadata'
    content.appendChild(video)
  } else if (isAudioFile(item.file_name) && item.blob_url) {
    const nameEl = document.createElement('div')
    nameEl.innerHTML = `<strong>${escapeHtml(item.file_name || 'File')}</strong> <span style="color:var(--text-secondary)">${formatBytes(item.file_size || 0)}</span>`
    content.appendChild(nameEl)
    const audio = document.createElement('audio')
    audio.className = 'item__audio-preview'
    audio.src = item.blob_url
    audio.controls = true
    audio.preload = 'metadata'
    content.appendChild(audio)
  } else {
    content.innerHTML = `<strong>${escapeHtml(item.file_name || 'File')}</strong> <span style="color:var(--text-secondary)">${formatBytes(item.file_size || 0)}</span>`
  }

  card.appendChild(content)

  // Actions — always show download for files/images with blob_url
  const actions = document.createElement('div')
  actions.className = 'item__actions'

  if (item.blob_url) {
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

  card.appendChild(actions)

  // Respect active filter
  if (activeFilter !== 'all' && card.dataset.kind !== activeFilter) {
    card.style.display = 'none'
  } else {
    card.style.display = ''
  }

  if (autoScroll) feed.scrollTop = feed.scrollHeight
}

function isImageFile(name) {
  if (!name) return false
  return /\.(png|jpe?g|gif|webp|svg|bmp|ico)$/i.test(name)
}

function isPdfFile(name) {
  if (!name) return false
  return /\.pdf$/i.test(name)
}

function isVideoFile(name) {
  if (!name) return false
  return /\.(mp4|webm|mov|avi|mkv)$/i.test(name)
}

function isAudioFile(name) {
  if (!name) return false
  return /\.(mp3|wav|ogg|m4a|flac|aac)$/i.test(name)
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
