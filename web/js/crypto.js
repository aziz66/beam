// TweetNaCl is loaded as global (nacl) via script tag in index.html
// We lazy-load to ensure it's available

function getNacl() {
  if (typeof nacl === 'undefined') {
    throw new Error('TweetNaCl not loaded')
  }
  return nacl
}

function getNaclUtil() {
  if (typeof nacl === 'undefined' || typeof nacl.util === 'undefined') {
    throw new Error('TweetNaCl-util not loaded')
  }
  return nacl.util
}

export function generateKey() {
  const key = getNacl().randomBytes(32)
  return getNaclUtil().encodeBase64(key)
}

export function encrypt(plaintext, keyBase64) {
  const n = getNacl()
  const util = getNaclUtil()
  const key = util.decodeBase64(keyBase64)
  const nonce = n.randomBytes(24)
  const messageBytes = typeof plaintext === 'string' ? util.decodeUTF8(plaintext) : plaintext
  const encrypted = n.secretbox(messageBytes, nonce, key)
  return {
    encrypted: util.encodeBase64(encrypted),
    nonce: util.encodeBase64(nonce)
  }
}

export function decrypt(encryptedBase64, nonceBase64, keyBase64) {
  const n = getNacl()
  const util = getNaclUtil()
  const key = util.decodeBase64(keyBase64)
  if (!key) throw new Error('invalid key encoding')
  const nonce = util.decodeBase64(nonceBase64)
  if (!nonce) throw new Error('invalid nonce encoding')
  const encrypted = util.decodeBase64(encryptedBase64)
  if (!encrypted) throw new Error('invalid ciphertext encoding')
  const decrypted = n.secretbox.open(encrypted, nonce, key)
  if (!decrypted) throw new Error('decryption failed')
  return decrypted
}

export function decryptToString(encryptedBase64, nonceBase64, keyBase64) {
  const bytes = decrypt(encryptedBase64, nonceBase64, keyBase64)
  return getNaclUtil().encodeUTF8(bytes)
}

export function encryptBytes(bytes, keyBase64) {
  const n = getNacl()
  const util = getNaclUtil()
  const key = util.decodeBase64(keyBase64)
  const nonce = n.randomBytes(24)
  const encrypted = n.secretbox(bytes, nonce, key)
  return {
    encrypted: util.encodeBase64(encrypted),
    nonce: util.encodeBase64(nonce)
  }
}
