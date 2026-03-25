// Single promise shared across concurrent callers — prevents double script injection
let pdfjsPromise = null

function ensurePdfJs() {
  if (!pdfjsPromise) {
    pdfjsPromise = new Promise((resolve, reject) => {
      const s = document.createElement('script')
      s.src = '/lib/pdf.min.js'
      s.onload = resolve
      s.onerror = (err) => { pdfjsPromise = null; reject(err) }
      document.head.appendChild(s)
    }).then(() => {
      pdfjsLib.GlobalWorkerOptions.workerSrc = '/lib/pdf.worker.min.js'
    }).catch((err) => {
      // Reset so the next call can retry (e.g. the script loaded but pdfjsLib was missing)
      pdfjsPromise = null
      throw err
    })
  }
  return pdfjsPromise
}

/**
 * Render the first page of a PDF blob URL as a thumbnail image.
 * Returns a blob URL for the rendered PNG, or null on failure.
 */
export async function renderPdfThumbnail(blobUrl, maxWidth = 400) {
  try {
    await ensurePdfJs()

    const pdf = await pdfjsLib.getDocument(blobUrl).promise
    let thumbUrl = null
    try {
      const page = await pdf.getPage(1)
      const viewport = page.getViewport({ scale: 1 })

      const scale = maxWidth / viewport.width
      const scaledViewport = page.getViewport({ scale })

      const canvas = document.createElement('canvas')
      canvas.width = scaledViewport.width
      canvas.height = scaledViewport.height
      const ctx = canvas.getContext('2d')

      await page.render({ canvasContext: ctx, viewport: scaledViewport }).promise

      thumbUrl = await new Promise((resolve) => {
        canvas.toBlob((blob) => {
          // Release the pixel buffer immediately — the blob holds the encoded data
          canvas.width = 0
          canvas.height = 0
          resolve(blob ? URL.createObjectURL(blob) : null)
        }, 'image/png')
      })
    } finally {
      // Always destroy the PDF document to free its memory
      pdf.destroy()
    }
    return thumbUrl
  } catch (err) {
    console.error('PDF thumbnail failed:', err)
    return null
  }
}
