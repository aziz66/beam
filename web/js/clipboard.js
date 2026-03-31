const CODE_HINTS = /[{};()=>\[\]]/

function isHttpUrl(text) {
  try {
    const u = new URL(text)
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

// Detect bare domains like "google.com" or "www.google.com/path"
function looksLikeBareUrl(text) {
  if (text.includes(' ') || text.length < 4) return false
  if (/^[a-z]+:\/\//i.test(text)) return false // already has scheme
  try {
    const u = new URL('https://' + text)
    // Require a dot in the hostname and a recognisable alpha TLD
    return /\.([a-zA-Z]{2,})$/.test(u.hostname)
  } catch {
    return false
  }
}

// Prepend https:// to bare domains; leave fully-qualified URLs untouched.
export function normalizeUrl(text) {
  const trimmed = text.trim()
  if (isHttpUrl(trimmed)) return trimmed
  if (looksLikeBareUrl(trimmed)) return 'https://' + trimmed
  return trimmed
}

export function detectContentKind(text) {
  if (!text) return 'text'
  const trimmed = text.trim()
  if (isHttpUrl(trimmed) || looksLikeBareUrl(trimmed)) return 'link'
  if (CODE_HINTS.test(trimmed) && trimmed.includes('\n')) return 'code'
  return 'text'
}

export function setupClipboardHandler(onText, onImage, onFiles) {
  document.addEventListener('paste', (e) => {
    const items = e.clipboardData?.items
    if (!items) return

    for (const item of items) {
      if (item.kind === 'file') {
        // Don't intercept file pastes when the user has a text input focused
        // (e.g. the passphrase modal) — it prevents the input from receiving text
        // and silently sends the file to the room instead.
        if (isInputFocused(e)) continue

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
