// Meta (Facebook / Instagram) Pixel, loaded only when the owner enabled it
// (body[data-pixel-id]) and, if asked, after the visitor accepted in the bar.
// PageView on each page; Lead once per order (eventID = reference) on the
// confirmation page. fbevents.js is loaded by this nonced module, which the
// CSP allows ('strict-dynamic'); Meta origins are in the CSP only while on.

const CONSENT_KEY = 'meta-pixel-consent'
const LEAD_KEY = 'meta-lead:'

function readConsent() {
  try {
    const v = localStorage.getItem(CONSENT_KEY)
    return v === 'granted' || v === 'denied' ? v : null
  } catch {
    return null
  }
}

function loadPixel(id) {
  if (window.fbq) return
  const fbq = (...args) => (fbq.callMethod ? fbq.callMethod(...args) : fbq.queue.push(args))
  fbq.queue = []
  fbq.loaded = true
  fbq.version = '2.0'
  fbq.push = fbq
  window.fbq = fbq
  window._fbq = fbq
  const script = document.createElement('script')
  script.async = true
  script.src = 'https://connect.facebook.net/en_US/fbevents.js'
  document.head.append(script)
  fbq('init', id)
  fbq('track', 'PageView')
  trackLead()
}

// Lead for the order on this page, at most once per reference on this device.
function trackLead() {
  const el = document.querySelector('[data-lead]')
  if (!el || !window.fbq) return
  const ref = el.dataset.lead
  try {
    if (localStorage.getItem(LEAD_KEY + ref)) return
    localStorage.setItem(LEAD_KEY + ref, '1')
  } catch {}
  window.fbq('track', 'Lead', { value: Number(el.dataset.leadValue), currency: 'MAD', content_name: el.dataset.leadContent }, { eventID: ref })
}

// A page swapped in place counts as a page view (and may be a confirmation).
export function pixelPageView() {
  if (!window.fbq) return
  window.fbq('track', 'PageView')
  trackLead()
}

export function initPixel() {
  const id = document.body.dataset.pixelId
  if (!id) return
  const needsConsent = document.body.hasAttribute('data-pixel-consent')
  const load = () => {
    const run = () => loadPixel(id)
    if ('requestIdleCallback' in window) window.requestIdleCallback(run, { timeout: 3000 })
    else window.setTimeout(run, 1500)
  }
  if (!needsConsent || readConsent() === 'granted') {
    load()
    return
  }
  if (readConsent() === 'denied') return
  const bar = document.querySelector('[data-consent]')
  if (!bar) return
  bar.hidden = false
  for (const button of bar.querySelectorAll('[data-consent-choice]')) {
    button.addEventListener('click', () => {
      const choice = button.dataset.consentChoice
      try {
        localStorage.setItem(CONSENT_KEY, choice)
      } catch {}
      bar.hidden = true
      if (choice === 'granted') load()
    })
  }
}
