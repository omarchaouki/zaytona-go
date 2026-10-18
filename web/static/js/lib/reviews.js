// Reviews: the star bars and "load more" work as plain links without JS;
// with JS they fetch the server-rendered fragment and update the list in
// place. The form posts (multipart, photo included) after the proof of work
// is solved, in place like any form (lib/nav.js).
import { submit, visit } from './nav.js'
import { createPow } from './pow.js'

const parse = (html) => new DOMParser().parseFromString(html, 'text/html')

async function fetchHTML(url) {
  const res = await fetch(url, { headers: { accept: 'text/html' }, credentials: 'same-origin' })
  if (!res.ok) throw new Error(String(res.status))
  return parse(await res.text())
}

function initList(section) {
  const box = section.querySelector('[data-reviews]')
  if (!box) return
  const setBars = (stars) => {
    for (const bar of section.querySelectorAll('a[data-review-filter]')) {
      const value = Number(bar.dataset.reviewFilter)
      if (value === 0) continue
      const active = value === stars
      bar.classList.toggle('bg-oil-soft/70', active)
      if (active) bar.setAttribute('aria-current', 'true')
      else bar.removeAttribute('aria-current')
    }
  }

  section.addEventListener('click', async (event) => {
    const filter = event.target.closest('a[data-review-filter]')
    const more = event.target.closest('a[data-review-more]')
    const list = box.querySelector('[data-review-list]')
    const base = list?.dataset.fragment
    if (!base || (!filter && !more)) return
    event.preventDefault()
    try {
      if (filter) {
        const current = section.querySelector('a[data-review-filter][aria-current]')
        let stars = Number(filter.dataset.reviewFilter)
        if (current && current === filter) stars = 0
        const doc = await fetchHTML(`${base}?list=1${stars ? `&stars=${stars}` : ''}`)
        const fresh = doc.querySelector('[data-review-list]')
        if (fresh) list.replaceWith(document.importNode(fresh, true))
        setBars(stars)
        box.scrollIntoView({ block: 'nearest' })
        return
      }
      more.setAttribute('aria-busy', 'true')
      const stars = Number(more.dataset.stars || 0)
      const doc = await fetchHTML(`${base}?page=${more.dataset.reviewMore}${stars ? `&stars=${stars}` : ''}`)
      const items = list.querySelector('[data-review-items]')
      for (const li of doc.querySelectorAll('[data-review-page] > ul > li')) {
        items?.append(document.importNode(li, true))
      }
      const next = doc.querySelector('[data-review-page] a[data-review-more]')
      if (next) more.replaceWith(document.importNode(next, true))
      else more.remove()
    } catch {
      // Network trouble: follow the plain link instead.
      visit((filter || more).href)
    }
  })

  // Photo dialogs of cards added later (the page script binds the first ones).
  box.addEventListener('click', (event) => {
    if (event.defaultPrevented) return
    const opener = event.target.closest('[data-dialog-open]')
    const dialog = opener && document.getElementById(opener.dataset.dialogOpen)
    if (!(dialog instanceof HTMLDialogElement) || dialog.open) return
    event.preventDefault()
    dialog.showModal()
    dialog.addEventListener('click', (e) => e.target === dialog && dialog.close(), { once: true })
  })
}

function initForm(form) {
  const pow = createPow()
  const altcha = form.querySelector('[data-altcha]')
  const error = form.querySelector('[data-review-error]')
  form.addEventListener('focusin', () => pow.warm(), { once: true })
  form.addEventListener('submit', async (event) => {
    if (event.submitter?.getAttribute('formmethod') === 'dialog') return
    event.preventDefault()
    const button = event.submitter || form.querySelector('button[type=submit]:not([formmethod])')
    const label = button?.textContent
    if (button) {
      button.disabled = true
      if (button.dataset.busyLabel) button.textContent = button.dataset.busyLabel
    }
    const payload = await pow.take()
    if (!payload) {
      if (button) {
        button.disabled = false
        button.textContent = label
      }
      if (error) {
        error.textContent ||= error.dataset.generic
        error.hidden = false
      }
      return
    }
    altcha.value = payload
    submit(form, button) // posted in place (lib/nav.js); natively if that fails
  })
}

export function initReviews() {
  const section = document.getElementById('reviews')
  if (section) initList(section)
  for (const form of document.querySelectorAll('form[data-review-form]')) initForm(form)
}
