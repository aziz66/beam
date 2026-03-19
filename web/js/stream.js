import { encrypt, decrypt, encryptBytes } from './crypto.js'
import { renderFileProgress } from './ui.js'

const CHUNK_SIZE = 64 * 1024 // 64KB
const TRANSFER_TIMEOUT = 5 * 60 * 1000 // 5 minutes

// Active transfers: fileId -> { chunks[], meta, received }
const incomingTransfers = new Map()

export async function sendFile(file, key, transport, deviceLabel) {
  const fileId = crypto.randomUUID ? crypto.randomUUID() : Math.random().toString(36).slice(2)
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

  const buffer = await file.arrayBuffer()

  for (let i = 0; i < totalChunks; i++) {
    const start = i * CHUNK_SIZE
    const end = Math.min(start + CHUNK_SIZE, file.size)
    const chunk = new Uint8Array(buffer.slice(start, end))
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

  return fileId
}

export function handleFileMeta(payload, key) {
  let fileName = 'unknown'
  try {
    fileName = new TextDecoder().decode(decrypt(payload.encrypted_name, payload.nonce, key))
  } catch {
    fileName = 'encrypted-file'
  }

  incomingTransfers.set(payload.file_id, {
    meta: payload,
    fileName,
    chunks: new Array(payload.total_chunks),
    received: 0
  })

  // Auto-clean abandoned transfers (sender disconnected before sending file_complete)
  setTimeout(() => {
    incomingTransfers.delete(payload.file_id)
  }, TRANSFER_TIMEOUT)

  renderFileProgress(payload.file_id, fileName, payload.size, 0)
}

export function handleFileChunk(payload, key) {
  const transfer = incomingTransfers.get(payload.file_id)
  if (!transfer) return

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

  // Combine chunks
  const totalSize = transfer.chunks.reduce((sum, c) => sum + (c ? c.length : 0), 0)
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
      blob_url: blobUrl
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
