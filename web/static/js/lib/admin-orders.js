// Lists with a bulk bar (orders, reviews): select all, selection count;
// orders also remember the page size.
import { visit } from './nav.js'

const SIZE_KEY = 'admin.orders.size'

function store(fn) {
  try {
    return fn(window.localStorage)
  } catch {
    return null
  }
}

export function initOrders() {
  const table = document.querySelector('[data-orders]')
  if (!table && !document.querySelector('[data-bulk]')) return

  // Page size (orders list): remembered per device; applied when the URL does not set one.
  const params = new URLSearchParams(window.location.search)
  const saved = store((s) => s.getItem(SIZE_KEY))
  if (table && !params.has('size') && saved && saved !== table.dataset.size) {
    params.set('size', saved)
    visit(`${window.location.pathname}?${params}`, { replace: true })
    return
  }
  const sizeSelect = document.querySelector('[data-page-size]')
  sizeSelect?.addEventListener('change', () => {
    store((s) => s.setItem(SIZE_KEY, sizeSelect.value))
    sizeSelect.form?.requestSubmit()
  })

  const all = document.querySelector('[data-select-all]')
  const boxes = () => [...document.querySelectorAll('[data-select-row]')]
  const bar = document.querySelector('[data-bulk]')
  const count = document.querySelector('[data-bulk-count]')
  // Each order has two checkboxes (phone card and wide table): count ids once.
  const update = () => {
    const checked = new Set(boxes().filter((b) => b.checked).map((b) => b.value)).size
    if (all) {
      all.checked = checked > 0 && checked === boxes().length
      all.indeterminate = checked > 0 && checked < boxes().length
    }
    if (count) count.textContent = count.dataset.template.replace('{count}', String(checked))
    if (bar) {
      bar.hidden = checked === 0
      bar.dataset.confirmCount = String(checked)
    }
  }
  all?.removeAttribute('hidden')
  all?.addEventListener('change', () => {
    for (const b of boxes()) b.checked = all.checked
    update()
  })
  for (const b of boxes()) {
    b.addEventListener('change', () => {
      for (const twin of boxes()) if (twin.value === b.value) twin.checked = b.checked
      update()
    })
  }
  update()

  // Auto-submit the filter form when a select changes (the button stays for no-JS).
  for (const select of document.querySelectorAll('[data-autosubmit]')) {
    select.addEventListener('change', () => select.form?.requestSubmit())
  }
}
