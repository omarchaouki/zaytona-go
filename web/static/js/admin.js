// Admin pages: every form is a plain POST that works without JavaScript.
// This file only enhances: the "More" sheet, confirm dialogs, busy buttons,
// select-all, page size memory, printing, and the new-order poll with its
// chime. No inline styles (CSP): state goes through classes and attributes.
// Pages change in place (lib/nav.js): listeners for the whole visit are set
// up once; page code runs in onPage after every swap, with the page signal.
import { initNotifications } from './lib/admin-notify.js'
import { initOrders } from './lib/admin-orders.js'
import { formatMad, fill } from './lib/format.js'
import { onPage, startNav, visit } from './lib/nav.js'
import { initProgress } from './lib/progress.js'

// Read at use: signing in swaps in a page with a new session token.
const csrf = () => document.body.dataset.csrf

// <dialog> openers: [data-dialog-open=id] (the link works without JS).
function initDialogs() {
  for (const opener of document.querySelectorAll('[data-dialog-open]')) {
    const dialog = document.getElementById(opener.dataset.dialogOpen)
    if (!(dialog instanceof HTMLDialogElement)) continue
    opener.addEventListener('click', (event) => {
      event.preventDefault()
      dialog.setAttribute('data-state', 'open')
      dialog.showModal()
    })
    dialog.addEventListener('click', (event) => {
      if (event.target === dialog) dialog.close()
    })
    dialog.addEventListener('close', () => dialog.removeAttribute('data-state'))
  }
}

// Submit buttons with [data-confirm=dialog id] open that dialog first; its
// [data-confirm-yes] button sends the form with confirm=1. Without JS the
// server answers confirm=0 with a confirmation page.
function initConfirms() {
  let pending = null
  document.addEventListener('submit', (event) => {
    const submitter = event.submitter
    const dialogId = submitter?.dataset.confirm
    if (!dialogId) return
    const form = event.target
    const field = form.querySelector('[data-confirm-field]')
    if (field?.value === '1') return
    const dialog = document.getElementById(dialogId)
    if (!(dialog instanceof HTMLDialogElement)) return
    event.preventDefault()
    const count = submitter.dataset.confirmCount ?? form.dataset.confirmCount ?? '1'
    const title = dialog.querySelector('[data-confirm-title]')
    if (title?.dataset.template) title.textContent = title.dataset.template.replace('{count}', count)
    pending = { form, submitter, field }
    dialog.showModal()
  })
  document.addEventListener('click', (event) => {
    const yes = event.target instanceof Element ? event.target.closest('[data-confirm-yes]') : null
    if (!yes || !pending?.form.isConnected) return
    const { form, submitter, field } = pending
    pending = null
    if (field) field.value = '1'
    yes.disabled = true
    if (yes.dataset.busyLabel) yes.textContent = yes.dataset.busyLabel
    yes.closest('dialog')?.close()
    form.requestSubmit(submitter)
  })
}

// Submit buttons with data-busy-label show it and lock while the page loads.
function initBusyButtons() {
  document.addEventListener('submit', (event) => {
    if (event.defaultPrevented) return
    const button = event.submitter
    if (!(button instanceof HTMLButtonElement) || !button.dataset.busyLabel) return
    window.setTimeout(() => {
      button.disabled = true
      button.textContent = button.dataset.busyLabel
    })
  })
}

// Print button on the order page (the slip has its own print CSS).
function initPrint() {
  for (const b of document.querySelectorAll('[data-print]')) {
    b.hidden = false
    b.addEventListener('click', () => window.print())
  }
}

// Opening an unseen order turns its red state off everywhere (a POST: GET
// requests never change anything).
function initOrderSeen() {
  const page = document.querySelector('[data-order]:not([data-order-seen])')
  const token = csrf()
  if (!page || !token) return
  fetch(`/admin/orders/${encodeURIComponent(page.dataset.order)}/seen`, {
    method: 'POST',
    body: new URLSearchParams({ csrf: token }),
    headers: { 'X-Requested-With': 'fetch', 'X-CSRF-Token': token },
    credentials: 'same-origin',
  }).catch(() => {})
}

// Dashboard: refreshed in place every 30 s while visible and untouched
// (scroll kept); the timer stops when another page is swapped in.
function initAutoRefresh(signal) {
  const el = document.querySelector('[data-autorefresh]')
  if (!el) return
  const seconds = Number(el.dataset.autorefresh) || 30
  const timer = window.setInterval(() => {
    if (document.visibilityState !== 'visible') return
    if (document.querySelector('dialog[open], details[open]:focus-within, :focus-visible')) return
    visit(location.href, { replace: true, keepScroll: true })
  }, seconds * 1000)
  signal.addEventListener('abort', () => window.clearInterval(timer))
}

// Settings > delivery: the sentence the customer reads, updated while typing
// (same templates as the site, filled in both languages).
function initDeliveryPreview() {
  const box = document.querySelector('[data-delivery-preview]')
  const form = box?.closest('form')
  if (!box || !form) return
  const num = (name) => {
    const v = Number.parseFloat(String(form.elements[name]?.value || '').replace(',', '.'))
    return Number.isFinite(v) && v >= 0 ? v : null
  }
  const update = () => {
    let mode = form.querySelector('input[name=mode]:checked')?.value || 'confirm'
    const fee = num('fee')
    const above = num('freeAbove')
    if ((mode === 'flat' && fee === null) || (mode === 'free_above' && (fee === null || above === null))) mode = 'confirm'
    if (mode === 'flat' && fee === 0) mode = 'free'
    for (const lang of ['ar', 'fr']) {
      const out = box.querySelector(`[data-preview-${lang}]`)
      const currency = box.dataset[`currency${lang === 'ar' ? 'Ar' : 'Fr'}`]
      const tpl = box.getAttribute(`data-${lang}-${mode}`) || ''
      if (out) out.textContent = fill(tpl, { fee: formatMad(fee ?? 0, currency), threshold: formatMad(above ?? 0, currency) })
    }
  }
  form.addEventListener('input', update)
  form.addEventListener('change', update)
}

// Once per visit.
initConfirms()
initBusyButtons()
initProgress()

// Every admin page: the first one and each one swapped in.
onPage((signal) => {
  initDialogs()
  initDeliveryPreview()
  initPrint()
  initOrders()
  initOrderSeen()
  initAutoRefresh(signal)
  initNotifications()
})

startNav()
