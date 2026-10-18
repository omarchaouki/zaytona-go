// The litre gauge: the visitor picks a quantity and reads the real total from
// the price per litre; the drawn bidon fills to match (--fill, CSSOM). Without
// JS the form is a plain GET to ?offer=custom&liters=<n>#order. With the order
// form on the page, "order this quantity" fills it in place instead.
import { fill, formatMad } from './format.js'

export function initGauge() {
  for (const form of document.querySelectorAll('[data-gauge]')) {
    const perLiter = Number(form.dataset.perLiter)
    const min = Number(form.dataset.min)
    const max = Number(form.dataset.max)
    if (!(perLiter > 0) || !(max >= min)) continue
    const number = form.querySelector('input[name="liters"]')
    const range = form.querySelector('[data-gauge-range]')
    const result = form.querySelector('[data-gauge-result]')
    const total = form.querySelector('[data-gauge-total]')
    const clamp = (v) => Math.min(max, Math.max(min, Math.round(v)))

    const render = (liters) => {
      form.style.setProperty('--fill', String(max > 0 ? Math.max(0.06, liters / max) : 0.5))
      if (result) {
        result.textContent = fill(form.dataset.result || '', { liters, price: form.dataset.perLiterLabel || '' })
      }
      if (total) total.textContent = formatMad(liters * perLiter, form.dataset.currency || '')
    }
    const current = () => {
      const v = Number.parseInt(number.value, 10)
      return Number.isFinite(v) ? clamp(v) : clamp(Number(range?.value) || min)
    }

    range?.addEventListener('input', () => {
      number.value = range.value
      render(current())
    })
    number.addEventListener('input', () => {
      const v = Number.parseInt(number.value, 10)
      if (!Number.isFinite(v)) return
      if (range) range.value = String(clamp(v))
      render(clamp(v))
    })
    number.addEventListener('change', () => {
      number.value = String(current())
    })

    form.addEventListener('submit', (event) => {
      const order = document.querySelector('[data-order-form]')
      if (!order) return
      event.preventDefault()
      const liters = current()
      number.value = String(liters)
      const field = order.querySelector('input[name="liters"]')
      if (field) {
        field.value = String(liters)
        field.dispatchEvent(new Event('input', { bubbles: true }))
      }
      window.dispatchEvent(new CustomEvent('offer:select', { detail: 'custom' }))
      document.getElementById('order')?.scrollIntoView({
        behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
      })
      history.replaceState(null, '', '#order')
    })

    render(current())
  }
}
