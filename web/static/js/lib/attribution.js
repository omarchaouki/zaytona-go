// Where an order came from, for the ads: utm_* and Meta's fbclid of the
// visitor's first arrival, kept 30 days on the device, sent with the order.

const KEYS = ['utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term', 'fbclid']
const STORAGE_KEY = 'first-touch'
const TTL_MS = 30 * 24 * 60 * 60 * 1000

function stored() {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null')
    if (!parsed?.at || Date.now() - parsed.at > TTL_MS || !parsed.tags) return null
    return parsed.tags
  } catch {
    return null
  }
}

// Records this visit's tags unless an earlier (still valid) visit already did.
export function captureAttribution() {
  try {
    const params = new URLSearchParams(location.search)
    const found = {}
    for (const key of KEYS) {
      const v = params.get(key)
      if (v) found[key] = v.slice(0, 300)
    }
    if (!Object.keys(found).length || stored()) return
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ at: Date.now(), tags: found }))
  } catch {}
}

export const currentAttribution = () => stored()
