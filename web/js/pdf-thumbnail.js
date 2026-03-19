let pdfjsLoaded = false

async function ensurePdfJs() {
  if (pdfjsLoaded) return
  await new Promise((resolve, reject) => {
    const s = document.createElement('script')
    s.src = '/lib/pdf.min.js'
    s.onload = resolve
    s.onerror = reject
    document.head.appendChild(s)
  })
  pdfjsLib.GlobalWorkerOptions.workerSrc = '/lib/pdf.worker.min.js'
  pdfjsLoaded = true
}

/**
 * Render the first page of a PDF blob URL as a thumbnail image.
 * Returns a blob URL for the rendered PNG, or null on failure.
 */
export async function renderPdfThumbnail(blobUrl, maxWidth = 400) {
  try {
    await ensurePdfJs()

    const pdf = await pdfjsLib.getDocument(blobUrl).promise
    const page = await pdf.getPage(1)
    const viewport = page.getViewport({ scale: 1 })

    const scale = maxWidth / viewport.width
    const scaledViewport = page.getViewport({ scale })

    const canvas = document.createElement('canvas')
    canvas.width = scaledViewport.width
    canvas.height = scaledViewport.height
    const ctx = canvas.getContext('2d')

    await page.render({ canvasContext: ctx, viewport: scaledViewport }).promise

    return new Promise((resolve) => {
      canvas.toBlob((blob) => {
        if (blob) {
          resolve(URL.createObjectURL(blob))
        } else {
          resolve(null)
        }
      }, 'image/png')
    })
  } catch (err) {
    console.error('PDF thumbnail failed:', err)
    return null
  }
}
