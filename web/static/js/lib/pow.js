// ALTCHA v2 proof of work, solved in the background while the visitor fills
// the form. Same derivation as the server (altcha-lib-go DeriveKeySHA, cost 1):
// key = SHA-256(salt || nonce || uint32 big-endian counter), and the answer is
// the first counter whose key starts with the signed keyPrefix. The server
// checks it once (pow_used), so every submission takes a fresh solution.

const hexToBytes = (hex) => {
  const out = new Uint8Array(hex.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16)
  return out
}
const bytesToHex = (bytes) => Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')

async function solve(challenge, signal) {
  const { salt, nonce, keyPrefix, keyLength = 32 } = challenge.parameters
  const prefix = keyPrefix.toLowerCase()
  const saltBytes = hexToBytes(salt)
  const nonceBytes = hexToBytes(nonce)
  const buf = new Uint8Array(saltBytes.length + nonceBytes.length + 4)
  buf.set(saltBytes, 0)
  buf.set(nonceBytes, saltBytes.length)
  const view = new DataView(buf.buffer)
  const counterAt = saltBytes.length + nonceBytes.length
  let lastYield = performance.now()
  for (let counter = 0; counter <= 0xffffffff; counter++) {
    if (signal?.aborted) return null
    view.setUint32(counterAt, counter, false)
    const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', buf)).slice(0, keyLength)
    const hex = bytesToHex(digest)
    if (hex.startsWith(prefix)) return { counter, derivedKey: hex }
    // Let the page breathe on slow phones.
    if (performance.now() - lastYield > 200) {
      await new Promise((r) => setTimeout(r, 0))
      lastYield = performance.now()
    }
  }
  return null
}

// One solution in progress at a time; take() hands it over and starts the next.
export function createPow(url = '/api/pow') {
  let current = null
  const start = () => {
    current = (async () => {
      try {
        const res = await fetch(url, { headers: { accept: 'application/json' }, cache: 'no-store' })
        if (!res.ok) return ''
        const challenge = await res.json()
        const solution = await solve(challenge)
        return solution ? btoa(JSON.stringify({ challenge, solution })) : ''
      } catch {
        return ''
      }
    })()
  }
  return {
    warm() {
      if (!current) start()
    },
    async take() {
      if (!current) start()
      const payload = await current
      start()
      return payload
    },
  }
}
