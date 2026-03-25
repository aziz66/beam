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

