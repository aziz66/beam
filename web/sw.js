const CACHE_NAME = 'beam-v29'
const SHELL_ASSETS = [
  '/',
  '/css/style.css',
  '/js/app.js',
  '/js/transport.js',
  '/js/crypto.js',
  '/js/stream.js',
  '/js/clipboard.js',
  '/js/ui.js',
  '/js/preview.js',
  '/js/device.js',
  '/js/webrtc.js',
  '/js/pdf-thumbnail.js',
  '/lib/tweetnacl.min.js',
  '/lib/tweetnacl-util.min.js',
  '/lib/qrcode.min.js',
  '/favicon.svg',
  '/manifest.json',
  '/icons/icon-192.png',
  '/icons/icon-512.png'
]

self.addEventListener('install', (event) => {
  // skipWaiting inside waitUntil so it only runs after the cache is fully
  // populated — activating with an incomplete cache causes blank-screen failures.
  event.waitUntil(
    caches.open(CACHE_NAME)
      .then((cache) => cache.addAll(SHELL_ASSETS))
      .then(() => self.skipWaiting())
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((names) =>
      Promise.all(
        names
          .filter((name) => name !== CACHE_NAME)
          .map((name) => caches.delete(name))
      )
    ).then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)

  // Share target: POST /share — read FormData inside respondWith so the
  // request body stream is still available when we parse it.
  if (url.pathname === '/share' && event.request.method === 'POST') {
    event.respondWith(
      (async () => {
        let text = null, file = null
        try {
          const data = await event.request.formData()
          text = data.get('text') || data.get('url') || data.get('title')
          file = data.get('file')
        } catch {}
        const clients = await self.clients.matchAll({ type: 'window', includeUncontrolled: false })
        // Prefer focused window in a room, then any room window, then last window
        const roomClients = clients.filter(c => new URL(c.url).pathname.startsWith('/r/'))
        // Prefer focused room windows first — a focused landing page should not
        // win over an unfocused room window, as the SW redirects to '/?shared=true'
        // after posting the message, making the landing page likely to be focused.
        const target = roomClients.find(c => c.focused) ||
                       roomClients[roomClients.length - 1] ||
                       clients.find(c => c.focused) ||
                       clients[clients.length - 1]
        if (target) target.postMessage({ type: 'share-target', text, file })
        return Response.redirect('/?shared=true', 303)
      })()
    )
    return
  }

  // Don't intercept WebSocket, API, or other non-GET requests
  if (
    url.pathname.startsWith('/ws/') ||
    url.pathname.startsWith('/api/') ||
    event.request.method !== 'GET'
  ) {
    return
  }

  // Network-first for HTML (SPA routes), cache-first for assets
  if (event.request.mode === 'navigate' || url.pathname.startsWith('/r/')) {
    event.respondWith(
      fetch(event.request)
        .then((response) => {
          if (response.ok) {
            const clone = response.clone()
            caches.open(CACHE_NAME).then((cache) => cache.put('/', clone))
          }
          return response
        })
        .catch(() => caches.match('/'))
    )
    return
  }

  // Cache-first for static assets
  event.respondWith(
    caches.match(event.request).then((cached) => {
      if (cached) return cached
      return fetch(event.request).then((response) => {
        if (response.ok) {
          const clone = response.clone()
          caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone))
        }
        return response
      })
    })
  )
})
