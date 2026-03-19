import { encrypt, decrypt, encryptBytes } from './crypto.js'
import { renderFileProgress } from './ui.js'

const CHUNK_SIZE = 64 * 1024 // 64KB

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

  const blob = new Blob([combined])
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
