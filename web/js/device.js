export function getDeviceLabel() {
  const ua = navigator.userAgent
  let browser = 'Browser'
  let os = ''

  if (ua.includes('Firefox')) browser = 'Firefox'
  else if (ua.includes('Edg/')) browser = 'Edge'
  else if (ua.includes('Chrome')) browser = 'Chrome'
  else if (ua.includes('Safari')) browser = 'Safari'

  if (ua.includes('iPhone') || ua.includes('iPad')) os = 'iOS'
  else if (ua.includes('Android')) os = 'Android'
  else if (ua.includes('Mac OS')) os = 'Mac'
  else if (ua.includes('Windows')) os = 'Windows'
  else if (ua.includes('Linux')) os = 'Linux'

  return os ? `${browser} on ${os}` : browser
}

export function getDeviceID() {
  let id = sessionStorage.getItem('beam_device_id')
  if (!id) {
    id = crypto.randomUUID ? crypto.randomUUID() : generateUUID()
    sessionStorage.setItem('beam_device_id', id)
  }
  return id
}

function generateUUID() {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`
}
