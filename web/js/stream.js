import { encrypt, decrypt, encryptBytes } from './crypto.js'
import { renderFileProgress, removeFeedCard } from './ui.js'

const CHUNK_SIZE = 64 * 1024 // 64KB
const TRANSFER_TIMEOUT = 5 * 60 * 1000 // 5 minutes
// Cap peer-supplied total_chunks to prevent memory exhaustion (~1 GB max).
const MAX_TOTAL_CHUNKS = 16384
// Cap the total assembled size before Uint8Array allocation to avoid OOM on
// low-memory devices. 512 MB is a generous limit that should rarely be hit.
const MAX_FILE_BYTES = 512 * 1024 * 1024

// Active transfers: fileId -> { chunks[], meta, received }
const incomingTransfers = new Map()

export async function sendFile(file, key, transport, deviceLabel) {
  const fileId = typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : Array.from(crypto.getRandomValues(new Uint8Array(16)), b => b.toString(16).padStart(2, '0')).join('')
  const totalChunks = Math.ceil(file.size / CHUNK_SIZE)

  // Encrypt file name
  const nameCrypto = encrypt(file.name, key)

  // Send file_meta
  transport.send({
    type: 'file_meta',
    payload: {
      file_id: fileId,
      encrypted_name: nameCrypto.encrypted,
      nonce: nameCrypto.nonce,
      size: file.size,
      chunk_size: CHUNK_SIZE,
      total_chunks: totalChunks,
      device_label: deviceLabel
    },
    ts: Date.now()
  })

  try {
    for (let i = 0; i < totalChunks; i++) {
      const start = i * CHUNK_SIZE
      const end = Math.min(start + CHUNK_SIZE, file.size)
      const chunk = new Uint8Array(await file.slice(start, end).arrayBuffer())
      const chunkCrypto = encryptBytes(chunk, key)

      transport.send({
        type: 'file_chunk',
        payload: {
          file_id: fileId,
          index: i,
          encrypted_data: chunkCrypto.encrypted,
          nonce: chunkCrypto.nonce
        },
        ts: Date.now()
      })

      renderFileProgress(fileId, file.name, file.size, (i + 1) / totalChunks)

      // Small delay to avoid flooding
      if (i % 10 === 9) {
        await new Promise(r => setTimeout(r, 0))
      }
    }

    transport.send({
      type: 'file_complete',
      payload: { file_id: fileId },
      ts: Date.now()
    })
  } catch (err) {
    transport.send({
      type: 'file_cancel',
      payload: { file_id: fileId },
      ts: Date.now()
    })
    throw err
  }

  return fileId
}

export function handleFileMeta(payload, key) {
  // Reject unreasonable values from untrusted peers before allocating memory.
  if (!payload.total_chunks || payload.total_chunks < 1 || payload.total_chunks > MAX_TOTAL_CHUNKS) {
    console.warn('stream: rejected file_meta with invalid total_chunks', payload.total_chunks)
    return
  }
  const safeSize = (typeof payload.size === 'number' && payload.size >= 0 && payload.size <= MAX_FILE_BYTES) ? payload.size : 0

  let fileName = 'unknown'
  try {
    const decoded = new TextDecoder().decode(decrypt(payload.encrypted_name, payload.nonce, key))
    // Cap to 255 chars — a malicious peer cannot cause DOM/memory issues with an enormous name
    fileName = decoded.length > 255 ? decoded.slice(0, 252) + '...' : decoded
  } catch {
    fileName = 'encrypted-file'
  }

  const cleanupTimer = setTimeout(() => {
    if (incomingTransfers.has(payload.file_id)) {
      incomingTransfers.delete(payload.file_id)
      removeFeedCard(payload.file_id)
    }
  }, TRANSFER_TIMEOUT)

  incomingTransfers.set(payload.file_id, {
    meta: { ...payload, size: safeSize },
    fileName,
    chunks: new Array(payload.total_chunks),
    received: 0,
    cleanupTimer
  })

  renderFileProgress(payload.file_id, fileName, safeSize, 0)
}

export function handleFileChunk(payload, key) {
  const transfer = incomingTransfers.get(payload.file_id)
  if (!transfer) return

  // Reject out-of-range indices without resetting the cleanup timer — a malicious
  // peer could repeatedly send bad indices to keep the transfer alive indefinitely
  // without making actual progress.
  if (payload.index < 0 || payload.index >= transfer.meta.total_chunks) return

  // Ignore duplicate chunks (network retransmit) — they would corrupt received count
  if (transfer.chunks[payload.index] !== undefined) return

  // Reset the stale-transfer cleanup timer on each *new valid* chunk so that a
  // slow but active transfer is not abandoned partway through (the 5-minute
  // window is per-chunk-gap, not total transfer time).
  clearTimeout(transfer.cleanupTimer)
  transfer.cleanupTimer = setTimeout(() => {
    if (incomingTransfers.has(payload.file_id)) {
      incomingTransfers.delete(payload.file_id)
      removeFeedCard(payload.file_id)
    }
  }, TRANSFER_TIMEOUT)

  try {
    const decrypted = decrypt(payload.encrypted_data, payload.nonce, key)
    transfer.chunks[payload.index] = decrypted
    transfer.received++

    renderFileProgress(
      payload.file_id,
      transfer.fileName,
      transfer.meta.size,
      transfer.received / transfer.meta.total_chunks
    )
  } catch (err) {
    console.error('chunk decrypt failed:', err)
  }
}

export function handleFileComplete(payload, onComplete) {
  const transfer = incomingTransfers.get(payload.file_id)
  if (!transfer) return

  clearTimeout(transfer.cleanupTimer)

  // Verify all chunks arrived — missing chunks produce a silently-truncated file
  if (transfer.received !== transfer.meta.total_chunks) {
    console.error(`file_complete: received ${transfer.received}/${transfer.meta.total_chunks} chunks — discarding incomplete file`)
    incomingTransfers.delete(payload.file_id)
    removeFeedCard(payload.file_id)
    return
  }

  // Combine chunks — guard against OOM from oversized transfers
  const totalSize = transfer.chunks.reduce((sum, c) => sum + (c ? c.length : 0), 0)
  if (totalSize > MAX_FILE_BYTES) {
    console.error(`file_complete: assembled size ${totalSize} exceeds ${MAX_FILE_BYTES} bytes — discarding`)
    incomingTransfers.delete(payload.file_id)
    removeFeedCard(payload.file_id)
    return
  }
  const combined = new Uint8Array(totalSize)
  let offset = 0
  for (const chunk of transfer.chunks) {
    if (chunk) {
      combined.set(chunk, offset)
      offset += chunk.length
    }
  }

  const mimeType = getMimeFromName(transfer.fileName)
  const blob = new Blob([combined], mimeType ? { type: mimeType } : undefined)
  const blobUrl = URL.createObjectURL(blob)

  incomingTransfers.delete(payload.file_id)

  if (onComplete) {
    onComplete({
      file_id: payload.file_id,
      file_name: transfer.fileName,
      file_size: transfer.meta.size,
      blob_url: blobUrl,
      device_label: transfer.meta.device_label || ''
    })
  }
}

const MIME_MAP = {
  pdf: 'application/pdf',
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
  svg: 'image/svg+xml',
  bmp: 'image/bmp',
  mp4: 'video/mp4',
  webm: 'video/webm',
  mov: 'video/quicktime',
  avi: 'video/x-msvideo',
  mkv: 'video/x-matroska',
  mp3: 'audio/mpeg',
  wav: 'audio/wav',
  ogg: 'audio/ogg',
  m4a: 'audio/mp4',
  flac: 'audio/flac',
  aac: 'audio/aac',
  txt: 'text/plain',
  html: 'text/html',
  css: 'text/css',
  js: 'text/javascript',
  json: 'application/json',
  zip: 'application/zip',
  doc: 'application/msword',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  xls: 'application/vnd.ms-excel',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  ppt: 'application/vnd.ms-powerpoint',
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
}

function getMimeFromName(name) {
  if (!name) return null
  const ext = name.toLowerCase().replace(/.*\.(\w+)$/, '$1')
  return MIME_MAP[ext] || null
}
