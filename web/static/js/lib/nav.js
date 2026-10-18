// In-place navigation: the site and the admin behave like a single-page app
// without becoming one. Same-site links and form submits are fetched in the
// background and the new page is swapped in (title, head metadata, <html>
// lang/dir, body), so the browser never blanks and reloads. Every page is
// still a real server-rendered page: without JS, links and forms work as
// before, and the address bar, back/forward and reload stay true.
//
// A full load happens instead whenever a swap could be wrong: another part of
// the site (admin and public pages load different scripts or styles), a page
// that carries its own <script> in the body (Turnstile), a response that is
// not HTML (CSV export), or any network or parsing failure.
//
// Page code registers with onPage(fn): fn(signal) runs on the first load and
// after every swap; signal aborts when the page is left, so listeners on
// window/document and timers started for one page do not pile up.

import { doneProgress, startProgress } from './progress.js'

const pageHandlers = []
let pageController = new AbortController()
let started = false
let navigating = 0
let announcer

/** Registers page code; it runs now (once the DOM is ready) and after each swap. */
export function onPage(fn) {
  pageHandlers.push(fn)
  if (started) fn(pageController.signal)
}

function runPage() {
  pageController.abort()
  pageController = new AbortController()
  for (const fn of pageHandlers) {
    try {
      fn(pageController.signal)
    } catch (err) {
      console.error(err)
    }
  }
}

const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches

// Assets that define a "part" of the site: if they differ, swap is unsafe.
const assetKey = (doc) =>
  [...doc.querySelectorAll('head link[rel="stylesheet"], head script[src]')]
    .map((el) => el.getAttribute('href') || el.getAttribute('src'))
    .sort()
    .join('|')

// Head elements that belong to the page, replaced on each swap.
const HEAD_PAGE =
  'meta[name="description"], meta[name="robots"], meta[name="referrer"], link[rel="canonical"], link[rel="alternate"], meta[property^="og:"], meta[name^="twitter:"], script[type="application/ld+json"], link[rel="preload"][as="image"]'

function sameDocument(url) {
  return url.origin === location.origin && url.pathname === location.pathname && url.search === location.search
}

// Paths that are never pages (files, uploads, API).
function swappablePath(url) {
  return !/^\/(static|uploads|api)\//.test(url.pathname) && !/\.(csv|pdf|jpe?g|png|webp|avif|svg|xml|txt)$/i.test(url.pathname)
}

function saveScroll() {
  try {
    history.replaceState({ ...history.state, zaytona: true, scroll: window.scrollY }, '')
  } catch {}
}

/** Visits a URL in place (falls back to a full load). */
export async function visit(href, { replace = false, keepScroll = false } = {}) {
  const url = new URL(href, location.href)
  if (url.origin !== location.origin || !swappablePath(url)) {
    location.assign(url.href)
    return
  }
  await load(url, { method: 'GET' }, { replace, keepScroll })
}

/** Submits a form in place (falls back to the native submit). */
export function submit(form, submitter) {
  const method = (submitter?.getAttribute('formmethod') || form.getAttribute('method') || 'get').toLowerCase()
  const action = new URL(submitter?.getAttribute('formaction') || form.getAttribute('action') || location.href, location.href)
  if (action.origin !== location.origin || !swappablePath(action)) {
    form.submit()
    return
  }
  const data = new FormData(form, submitter || undefined)
  if (method === 'get') {
    action.search = new URLSearchParams([...data].filter(([, v]) => typeof v === 'string')).toString()
    load(action, { method: 'GET' }, { form })
    return
  }
  const multipart = (form.getAttribute('enctype') || '').toLowerCase() === 'multipart/form-data'
  const body = multipart ? data : new URLSearchParams([...data].filter(([, v]) => typeof v === 'string'))
  load(action, { method: 'POST', body }, { form, submitter })
}

async function load(url, init, { replace = false, keepScroll = false, form, submitter, pop = false, scroll } = {}) {
  const ticket = ++navigating
  startProgress()
  let res
  let html
  try {
    res = await fetch(url.href, {
      ...init,
      credentials: 'same-origin',
      redirect: 'follow',
      headers: { Accept: 'text/html' },
    })
    const type = res.headers.get('Content-Type') || ''
    if (!type.includes('text/html')) {
      // Not a page (a file, JSON): a GET opens it; a POST is never re-sent.
      doneProgress()
      if (init.method === 'GET') location.assign(res.url || url.href)
      else location.assign(res.redirected ? res.url : location.href)
      return
    }
    html = await res.text()
  } catch {
    if (ticket === navigating) fallback(url, init, form, submitter)
    return
  }
  if (ticket !== navigating) return // a newer navigation won
  // A response URL never carries the fragment: keep the one asked for.
  const finalURL = new URL(res.url || url.href)
  if (url.hash && !finalURL.hash) finalURL.hash = url.hash
  const doc = new DOMParser().parseFromString(html, 'text/html')
  if (assetKey(doc) !== assetKey(document) || doc.body.querySelector('script:not([type="application/ld+json"])')) {
    // Another part of the site, or a page with its own scripts: load it
    // fully. A POST is never sent twice: show where it led (or reload).
    doneProgress()
    location.assign(init.method === 'GET' || res.redirected ? finalURL.href : location.href)
    return
  }
  if (!pop) saveScroll()
  const apply = () => swap(doc)
  if (document.startViewTransition && !reduced()) {
    await document.startViewTransition(apply).updateCallbackDone.catch(() => apply())
  } else {
    apply()
  }
  // A refused POST is answered in place (422 with the messages): the address
  // bar keeps the page it was sent from, so a reload never re-posts.
  const shown = init.method === 'POST' && !res.redirected ? new URL(location.href) : finalURL
  const state = { zaytona: true, scroll: 0 }
  if (!pop) {
    if (replace || shown.href === location.href) history.replaceState(state, '', shown.href)
    else history.pushState(state, '', shown.href)
  }
  doneProgress()
  runPage()
  place(finalURL, { keepScroll, scroll, form, status: res.status })
  announce(document.title)
}

// No swap possible: do what the browser would have done.
function fallback(url, init, form, submitter, finalHref) {
  doneProgress()
  if (init.method === 'GET') {
    location.assign(finalHref || url.href)
    return
  }
  if (form) {
    // Native submit with the clicked button's name/value kept.
    if (submitter?.name) {
      const hidden = document.createElement('input')
      hidden.type = 'hidden'
      hidden.name = submitter.name
      hidden.value = submitter.value
      form.append(hidden)
    }
    form.submit()
  }
}

function swap(doc) {
  document.title = doc.title
  for (const el of document.head.querySelectorAll(HEAD_PAGE)) el.remove()
  for (const el of doc.head.querySelectorAll(HEAD_PAGE)) document.head.append(document.importNode(el, true))
  const html = document.documentElement
  for (const name of ['lang', 'dir']) {
    const v = doc.documentElement.getAttribute(name)
    if (v) html.setAttribute(name, v)
  }
  const body = document.body
  for (const { name } of [...body.attributes]) body.removeAttribute(name)
  for (const { name, value } of [...doc.body.attributes]) body.setAttribute(name, value)
  body.replaceChildren(...[...doc.body.childNodes].map((n) => document.importNode(n, true)))
  delete html.dataset.mobileBar
}

// Scroll and focus after a swap: the fragment, the first invalid field after a
// refused form, the saved position on back/forward, else the top; focus goes
// to the main landmark so keyboard and screen-reader users start there.
function place(url, { keepScroll, scroll, form, status }) {
  const main = document.getElementById('main')
  if (keepScroll) return
  if (typeof scroll === 'number') {
    jump(scroll)
    return
  }
  if (form && status >= 400) {
    const bad = document.querySelector('[aria-invalid="true"], [role="alert"]:not([hidden])')
    if (bad) {
      bad.scrollIntoView({ block: 'center', behavior: 'instant' })
      if (bad.matches('input, textarea, select')) bad.focus({ preventScroll: true })
      return
    }
  }
  const target = url.hash && document.getElementById(decodeURIComponent(url.hash.slice(1)))
  if (target) {
    target.scrollIntoView({ behavior: 'instant' })
    return
  }
  jump(0)
  if (main) {
    if (!main.hasAttribute('tabindex')) main.setAttribute('tabindex', '-1')
    main.focus({ preventScroll: true })
  }
}

// A new page starts where it should at once (the site scrolls smoothly for
// in-page links, which would make a swapped page glide).
const jump = (top) => window.scrollTo({ top, left: 0, behavior: 'instant' })

// Screen readers hear the new page's title.
function announce(text) {
  if (!announcer?.isConnected) {
    announcer = document.createElement('p')
    announcer.className = 'sr-only'
    announcer.setAttribute('aria-live', 'polite')
    document.body.append(announcer)
  }
  announcer.textContent = ''
  window.setTimeout(() => {
    announcer.textContent = text
  }, 60)
}

function onClick(event) {
  if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
  const a = event.target instanceof Element ? event.target.closest('a[href]') : null
  if (!a || a.hasAttribute('download') || a.hasAttribute('data-no-swap')) return
  if ((a.getAttribute('target') || '_self') !== '_self') return
  const url = new URL(a.href, location.href)
  if (url.origin !== location.origin || !/^https?:$/.test(url.protocol) || !swappablePath(url)) return
  // Same page, another fragment: the browser scrolls.
  if (sameDocument(url) && url.hash) return
  event.preventDefault()
  load(url, { method: 'GET' }, {})
}

function onSubmit(event) {
  if (event.defaultPrevented) return
  const form = event.target
  if (!(form instanceof HTMLFormElement) || form.hasAttribute('data-no-swap')) return
  const submitter = event.submitter
  const method = (submitter?.getAttribute('formmethod') || form.getAttribute('method') || 'get').toLowerCase()
  if (method === 'dialog') return
  if ((submitter?.getAttribute('formtarget') || form.getAttribute('target') || '_self') !== '_self') return
  event.preventDefault()
  submit(form, submitter)
}

function onPop(event) {
  if (!event.state?.zaytona) return
  load(new URL(location.href), { method: 'GET' }, { pop: true, scroll: event.state.scroll ?? 0 })
}

/** Starts in-place navigation and runs the page code for the first page. */
export function startNav() {
  if (started) return
  started = true
  // Scroll is restored by us (swapped pages are not known to the browser);
  // a reload or a return from another site keeps its place too.
  const saved = history.state?.scroll
  const kind = performance.getEntriesByType?.('navigation')[0]?.type
  if ('scrollRestoration' in history) history.scrollRestoration = 'manual'
  if (typeof saved === 'number' && (kind === 'reload' || kind === 'back_forward')) {
    window.addEventListener('load', () => jump(saved), { once: true })
  }
  history.replaceState({ ...history.state, zaytona: true, scroll: window.scrollY }, '')
  // Bubble phase on document: element handlers (dialogs, filters, the order
  // form) run first and may take the event (defaultPrevented).
  document.addEventListener('click', onClick)
  document.addEventListener('submit', onSubmit)
  window.addEventListener('popstate', onPop)
  window.addEventListener('pagehide', saveScroll)
  runPage()
}
