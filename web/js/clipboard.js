const CODE_HINTS = /[{};()=>\[\]]/

function isHttpUrl(text) {
  try {
    const u = new URL(text)
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

export function detectContentKind(text) {
  if (!text) return 'text'
  const trimmed = text.trim()
  if (isHttpUrl(trimmed)) return 'link'
  if (CODE_HINTS.test(trimmed) && trimmed.includes('\n')) return 'code'
  return 'text'
}

export function setupClipboardHandler(onText, onImage, onFiles) {
  document.addEventListener('paste', (e) => {
    const items = e.clipboardData?.items
    if (!items) return

    for (const item of items) {
      if (item.kind === 'file') {
        const file = item.getAsFile()
        if (!file) continue

        if (file.type.startsWith('image/')) {
          e.preventDefault()
          onImage(file)
          return
        }
        e.preventDefault()
        onFiles([file])
        return
      }
    }

    // Text content
    const text = e.clipboardData.getData('text/plain')
    if (text && !isInputFocused(e)) {
      e.preventDefault()
      onText(text)
    }
  })
}

export async function copyToClipboard(text) {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    // Fallback
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    // execCommand is deprecated but remains the only synchronous fallback
    // when navigator.clipboard is unavailable (e.g. non-secure HTTP context).
    // eslint-disable-next-line no-document-execcommand
    const ok = document.execCommand('copy')
    document.body.removeChild(textarea)
    return ok
  }
}

function isInputFocused(e) {
  const el = document.activeElement
  if (!el) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || el.isContentEditable
}
