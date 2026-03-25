const langKeywords = {
  javascript: /\b(const|let|var|function|return|if|else|for|while|class|import|export|from|async|await|=>)\b/,
  python: /\b(def|class|import|from|return|if|elif|else|for|while|with|as|try|except|lambda)\b/,
  go: /\b(func|package|import|return|if|else|for|range|struct|interface|defer|go|chan)\b/,
  html: /<\/?[a-z][\s\S]*>/i,
  css: /[{};].*[:;].*[{};]/,
  sql: /\b(SELECT|FROM|WHERE|INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|JOIN|ON|AND|OR)\b/i,
  json: /^\s*[{\[]/,
  yaml: /^\s*\w+\s*:/m,
  bash: /\b(echo|cd|ls|grep|awk|sed|chmod|chown|curl|wget|sudo|apt|yum|brew)\b/
}

export function detectLanguage(text) {
  if (!text || text.length < 5) return null

  try {
    JSON.parse(text)
    return 'json'
  } catch {}

  for (const [lang, pattern] of Object.entries(langKeywords)) {
    if (pattern.test(text)) return lang
  }
  return null
}

const _kwRegex = /\b(const|let|var|function|return|if|else|for|while|class|import|export|from|async|await|def|func|package|struct|interface|defer|go|chan|range|try|except|with|as|lambda|elif)\b/g

export function highlightCode(text, lang) {
  const pre = document.createElement('pre')
  pre.className = 'item__code'

  // Process tokens using safe DOM manipulation
  // escapeHtml gives us a safe HTML string; we use it to build spans
  let escaped = escapeHtml(text)

  // Apply highlighting regexes to the escaped string (safe: only injecting hardcoded span tags around already-escaped content)
  escaped = escaped.replace(/(["'`])(?:(?!\1|\\).|\\.)*\1/g, '<span class="hl-string">$&</span>')
  escaped = escaped.replace(/(\/\/.*$|#.*$)/gm, '<span class="hl-comment">$&</span>')
  escaped = escaped.replace(/\b(\d+\.?\d*)\b/g, '<span class="hl-number">$1</span>')
  _kwRegex.lastIndex = 0
  escaped = escaped.replace(_kwRegex, '<span class="hl-keyword">$1</span>')

  pre.innerHTML = escaped  // safe: escaped is HTML-escaped user content + hardcoded span tags
  return pre
}

export function renderTextContent(text, maxLength = 500) {
  const frag = document.createDocumentFragment()
  if (text.length <= maxLength) {
    frag.appendChild(document.createTextNode(text))
    return frag
  }
  frag.appendChild(document.createTextNode(text.slice(0, maxLength)))
  const more = document.createElement('span')
  more.className = 'item__more'
  more.style.cssText = 'color:var(--accent);cursor:pointer'
  more.textContent = ' ...show more'
  frag.appendChild(more)
  return frag
}

export function renderLinkPreview(url) {
  const el = document.createElement('div')
  el.innerHTML = `<a href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer" class="item__content--link">${escapeHtml(url)}</a>`

  // AbortController so the fetch can be cancelled when the item is pruned
  const controller = new AbortController()
  el._abortFetch = () => controller.abort()

  // Try to fetch OG preview
  fetch(`/api/preview?url=${encodeURIComponent(url)}`, { signal: controller.signal })
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      if (!data || !data.title) return
      if (!document.contains(el)) return
      el.innerHTML = ''

      const card = document.createElement('a')
      card.href = url
      card.target = '_blank'
      card.rel = 'noopener noreferrer'
      card.className = 'link-preview'

      // Only allow https image URLs — reject http, data:, javascript:, etc.
      if (data.image && data.image.startsWith('https://')) {
        const img = document.createElement('img')
        img.className = 'link-preview__image'
        img.src = data.image
        img.alt = data.title || ''
        img.onerror = () => img.remove()
        card.appendChild(img)
      }

      const body = document.createElement('div')
      body.className = 'link-preview__body'

      const title = document.createElement('div')
      title.className = 'link-preview__title'
      title.textContent = data.title
      body.appendChild(title)

      if (data.description) {
        const desc = document.createElement('div')
        desc.className = 'link-preview__description'
        desc.textContent = data.description
        body.appendChild(desc)
      }

      const domain = document.createElement('div')
      domain.className = 'link-preview__domain'
      try { domain.textContent = new URL(url).hostname } catch { domain.textContent = url }
      body.appendChild(domain)

      card.appendChild(body)
      el.appendChild(card)
    })
    .catch(() => {})

  return el
}

export function formatBytes(n) {
  if (n === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  const val = n / Math.pow(1024, i)
  return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
}


function escapeHtml(str) {
  const div = document.createElement('div')
  div.textContent = str
  return div.innerHTML
}
