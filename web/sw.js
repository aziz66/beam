const CACHE_NAME = 'beam-v7'
const SHELL_ASSETS = [
  '/',
  '/css/style.css?v=999',
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
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(SHELL_ASSETS))
  )
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((names) =>
      Promise.all(
        names
          .filter((name) => name !== CACHE_NAME)
          .map((name) => caches.delete(name))
      )
    )
  )
  self.clients.claim()
})

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)

  // Don't cache WebSocket, API, or POST requests
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
          const clone = response.clone()
          caches.open(CACHE_NAME).then((cache) => cache.put('/', clone))
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
        const clone = response.clone()
        caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone))
        return response
      })
    })
  )
})

// Handle share target
self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)
  if (url.pathname === '/share' && event.request.method === 'POST') {
    event.respondWith(Response.redirect('/?shared=true', 303))
    event.waitUntil(
      (async () => {
        const data = await event.request.formData()
        const text = data.get('text') || data.get('url') || data.get('title')
        const file = data.get('file')
        const clients = await self.clients.matchAll({ type: 'window' })
        for (const client of clients) {
          client.postMessage({
            type: 'share-target',
            text,
            file
          })
        }
      })()
    )
  }
})
