// Public pages: everything here only enhances a page that already works
// without JavaScript. No inline styles (CSP): state goes through classes,
// attributes and CSSOM custom properties. Pages change in place (lib/nav.js):
// listeners for the whole visit are set up once; page code runs in onPage,
// again after every swap, and gives back its window listeners and observers
// through the page signal.
import { captureAttribution } from './lib/attribution.js'
import { initGauge } from './lib/gauge.js'
import { initLetters } from './lib/letters.js'
import { onPage, startNav } from './lib/nav.js'
import { clearOrderDraft, initOrderForm } from './lib/order.js'
import { initPixel, pixelPageView } from './lib/pixel.js'
import { initProgress } from './lib/progress.js'
import { initReviews } from './lib/reviews.js'
import { initStage } from './lib/stage.js'

const doc = document.documentElement
// Read at use: the language can change with an in-place swap.
const locale = () => (doc.lang.startsWith('ar') ? 'ar' : 'fr')

// Save-Data: the drifting photographs stand still (CSS reads html.save-data).
if (navigator.connection?.saveData) doc.classList.add('save-data')

// Scroll reveals: [data-reveal] gets data-revealed when it scrolls into view.
// A MutationObserver also picks up content added later.
function initReveals() {
  const reveal = (el) => el.setAttribute('data-revealed', '')
  if (!('IntersectionObserver' in window)) {
    for (const el of document.querySelectorAll('[data-reveal]')) reveal(el)
    return
  }
  const io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (!e.isIntersecting) continue
        reveal(e.target)
        io.unobserve(e.target)
      }
    },
    { rootMargin: '0px 0px -8% 0px', threshold: 0.08 },
  )
  const watch = (root) => {
    for (const el of root.querySelectorAll('[data-reveal]:not([data-revealed])')) io.observe(el)
  }
  watch(document)
  new MutationObserver((records) => {
    for (const r of records) {
      for (const node of r.addedNodes) {
        if (!(node instanceof Element)) continue
        if (node.matches('[data-reveal]:not([data-revealed])')) io.observe(node)
        watch(node)
      }
    }
  }).observe(document.body, { childList: true, subtree: true })
}

// Anonymous WhatsApp click counter (placement + locale, no personal data).
function initWhatsAppClicks() {
  document.addEventListener(
    'click',
    (event) => {
      const link = event.target instanceof Element ? event.target.closest('[data-wa-placement]') : null
      if (!link) return
      const body = JSON.stringify({ placement: link.dataset.waPlacement, locale: locale() })
      const blob = new Blob([body], { type: 'application/json' })
      if (!navigator.sendBeacon?.('/api/wa-click', blob)) {
        fetch('/api/wa-click', {
          method: 'POST',
          body,
          keepalive: true,
          headers: { 'content-type': 'application/json' },
        }).catch(() => {})
      }
    },
    { capture: true },
  )
}

// Phone sticky bar: visible once the hero buttons are gone, hidden while the
// order section is on screen. html[data-mobile-bar] makes room in the footer.
function initMobileBar(signal) {
  const bar = document.querySelector('[data-mobile-bar]')
  const hero = document.querySelector('[data-hero-cta]')
  const order = document.getElementById('order')
  if (!bar || !hero || !order || !('IntersectionObserver' in window)) return
  let heroVisible = true
  let orderVisible = false
  const update = () => {
    const visible = !heroVisible && !orderVisible
    bar.toggleAttribute('data-visible', visible)
    bar.toggleAttribute('inert', !visible)
    bar.setAttribute('aria-hidden', String(!visible))
    doc.dataset.mobileBar = visible ? 'on' : 'off'
  }
  const heroIO = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (e.target === hero) heroVisible = e.isIntersecting
      if (e.target === order) orderVisible = e.isIntersecting
    }
    update()
  })
  heroIO.observe(hero)
  const orderIO = new IntersectionObserver((entries) => {
    for (const e of entries) orderVisible = e.isIntersecting
    update()
  })
  orderIO.observe(order)
  signal.addEventListener('abort', () => {
    heroIO.disconnect()
    orderIO.disconnect()
  })
}

// Floating WhatsApp tooltip, once per session (4 s after load, for 8 s).
function initWhatsAppTooltip() {
  const tip = document.querySelector('[data-wa-tooltip]')
  if (!tip) return
  const KEY = 'wa-tooltip-seen'
  try {
    if (sessionStorage.getItem(KEY) === '1') return
  } catch {
    return
  }
  const hide = () => {
    tip.hidden = true
  }
  tip.querySelector('[data-wa-tooltip-close]')?.addEventListener('click', hide)
  window.setTimeout(() => {
    tip.hidden = false
    try {
      sessionStorage.setItem(KEY, '1')
    } catch {}
    window.setTimeout(hide, 8000)
  }, 4000)
}

// "Order this offer" links point to ?offer=<slug>#order (preselected by the
// server without JS). With JS they stay on the page: pick the offer in the
// form and scroll to it.
function initOfferLinks() {
  document.addEventListener('click', (event) => {
    const link = event.target instanceof Element ? event.target.closest('[data-offer]') : null
    if (!link || !document.querySelector('[data-order-form]')) return
    event.preventDefault()
    window.dispatchEvent(new CustomEvent('offer:select', { detail: link.dataset.offer }))
    const order = document.getElementById('order')
    order?.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
    history.replaceState(null, '', '#order')
  })
}

// Confirmation page: copy the reference; the order is sent, so drop the draft.
function initConfirmation() {
  if (document.querySelector('[data-lead]')) clearOrderDraft()
  for (const button of document.querySelectorAll('[data-copy]')) {
    button.hidden = !navigator.clipboard
    button.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(button.dataset.copy)
        const label = button.querySelector('[data-copy-label]')
        const before = label.textContent
        label.textContent = button.dataset.copiedLabel
        setTimeout(() => {
          label.textContent = before
        }, 2000)
      } catch {
        // Clipboard blocked: the reference stays selectable on screen.
      }
    })
  }
}

// The map iframe is heavy: replace the link with it only when asked.
function initMapReveal() {
  for (const link of document.querySelectorAll('[data-map-reveal]')) {
    link.addEventListener('click', (event) => {
      event.preventDefault()
      const frame = document.createElement('iframe')
      frame.src = link.href
      frame.title = link.dataset.mapTitle || ''
      frame.loading = 'lazy'
      frame.referrerPolicy = 'no-referrer-when-downgrade'
      frame.className = 'mt-4 aspect-[4/3] w-full max-w-[22rem] rounded-card border-0'
      link.replaceWith(frame)
    })
  }
}

// Certificate scan: a link to the image without JS, a <dialog> with it.
function initDialogs() {
  for (const opener of document.querySelectorAll('[data-dialog-open]')) {
    const dialog = document.getElementById(opener.dataset.dialogOpen)
    if (!(dialog instanceof HTMLDialogElement)) continue
    opener.addEventListener('click', (event) => {
      event.preventDefault()
      dialog.showModal()
    })
    dialog.addEventListener('click', (event) => {
      if (event.target === dialog) dialog.close()
    })
  }
}

// Once per visit: delegated listeners and observers that outlive pages.
initReveals()
initWhatsAppClicks()
initOfferLinks()
initProgress()
initPixel()

// Every page: the first one and each one swapped in.
let firstPage = true
onPage((signal) => {
  initLetters()
  initStage(signal)
  initGauge()
  initMobileBar(signal)
  initWhatsAppTooltip()
  initMapReveal()
  initDialogs()
  captureAttribution()
  initOrderForm(signal)
  initReviews()
  initConfirmation()
  if (!firstPage) pixelPageView()
  firstPage = false
})

startNav()
