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

export function highlightCode(text, lang) {
  // Simple token-based highlighting
  let escaped = escapeHtml(text)

  // Strings
  escaped = escaped.replace(/(["'`])(?:(?!\1|\\).|\\.)*\1/g, '<span style="color:var(--success)">$&</span>')

  // Comments
  escaped = escaped.replace(/(\/\/.*$|#.*$)/gm, '<span style="color:var(--text-secondary)">$&</span>')

  // Numbers
  escaped = escaped.replace(/\b(\d+\.?\d*)\b/g, '<span style="color:var(--warning)">$1</span>')

  // Keywords
  const keywords = 'const|let|var|function|return|if|else|for|while|class|import|export|from|async|await|def|func|package|struct|interface|defer|go|chan|range|try|except|with|as|lambda|elif'
  const kwRegex = new RegExp(`\\b(${keywords})\\b`, 'g')
  escaped = escaped.replace(kwRegex, '<span style="color:var(--accent)">$1</span>')

  return escaped
}

export function renderTextContent(text, maxLength = 500) {
  if (text.length <= maxLength) {
    return escapeHtml(text)
  }
  const truncated = escapeHtml(text.slice(0, maxLength))
  return `${truncated}<span class="item__more" style="color:var(--accent);cursor:pointer"> ...show more</span>`
}

export function renderLinkPreview(url) {
  const el = document.createElement('div')
  el.innerHTML = `<a href="${escapeHtml(url)}" target="_blank" rel="noopener" class="item__content--link">${escapeHtml(url)}</a>`

  // Try to fetch OG preview
  fetch(`/api/preview?url=${encodeURIComponent(url)}`)
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      if (!data || !data.title) return
      el.innerHTML = ''

      const card = document.createElement('a')
      card.href = url
      card.target = '_blank'
      card.rel = 'noopener'
      card.className = 'link-preview'

      if (data.image) {
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

export function formatTimeAgo(ts) {
  const diff = Math.floor((Date.now() - ts) / 1000)
  if (diff < 5) return 'just now'
  if (diff < 60) return `${diff}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

function escapeHtml(str) {
  const div = document.createElement('div')
  div.textContent = str
  return div.innerHTML
}
