// Order form enhancement. The form already works as a plain post; this adds
// the three steps with per-step validation (the server validates again), the
// live total, the review step, city search, a 7-day draft, the proof of work
// in the background, and sending with fetch: one automatic retry, then a
// recovery box (details kept, Retry, Send by WhatsApp with the order filled in).
import { currentAttribution } from './attribution.js'
import { CITIES } from './cities.js'
import { fill, formatMad } from './format.js'
import { visit } from './nav.js'
import { createPow } from './pow.js'
import { doneProgress, startProgress } from './progress.js'

const DRAFT_KEY = 'order-draft'
const DRAFT_TTL = 7 * 24 * 60 * 60 * 1000
const DRAFT_FIELDS = ['offer', 'liters', 'name', 'phone', 'city', 'address', 'note']
const FIELD_IDS = { customLiters: 'order-liters', name: 'order-name', phone: 'order-phone', city: 'order-city', address: 'order-address', note: 'order-note' }
const STEP_OF = { choice: 0, customLiters: 0, name: 1, phone: 1, city: 1, address: 1, note: 1 }

// Same rules as domain.NormalizeMoroccanPhone.
export function normalizePhone(input) {
  const ascii = input
    .replace(/[\u0660-\u0669]/g, (d) => String(d.charCodeAt(0) - 0x660))
    .replace(/[\u06f0-\u06f9]/g, (d) => String(d.charCodeAt(0) - 0x6f0))
    .replace(/[\s.\-()\u200e\u200f]/g, '')
  const m = /^(?:\+212|00212|0)([5-7]\d{8})$/.exec(ascii)
  return m ? `+212${m[1]}` : null
}

const displayPhone = (e164) => `0${e164.slice(4)}`.replace(/^(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})$/, '$1 $2 $3 $4 $5')

// Fold case, accents and Arabic letter variants so "fes", "Fès" and "فاس" match.
function fold(value) {
  return value
    .normalize('NFD')
    .replace(/\p{M}/gu, '')
    .replace(/\u0640/g, '')
    .replace(/[\u0623\u0625\u0622\u0671]/g, '\u0627')
    .replace(/\u0629/g, '\u0647')
    .replace(/\u0649/g, '\u064a')
    .replace(/[\u06af\u06a9]/g, '\u0643')
    .toLowerCase()
    .replace(/[\s'\u2019-]+/g, ' ')
    .trim()
}

function searchCities(query, locale, limit = 8) {
  const q = fold(query)
  const label = (c) => (locale === 'ar' ? c[0] : c[1])
  if (!q) return CITIES.slice(0, limit).map(label)
  const starts = []
  const contains = []
  for (const c of CITIES) {
    const keys = [fold(c[0]), fold(c[1])]
    if (keys.some((k) => k.startsWith(q))) starts.push(label(c))
    else if (keys.some((k) => k.includes(q))) contains.push(label(c))
  }
  return [...starts, ...contains].slice(0, limit)
}

export function initOrderForm(signal) {
  const form = document.querySelector('[data-order-form]')
  if (!form) return
  const msg = JSON.parse(form.dataset.messages || '{}')
  const locale = form.dataset.locale
  const steps = [...form.querySelectorAll('[data-step]')]
  const submit = form.querySelector('[data-submit]')
  const back = form.querySelector('[data-back]')
  const errorBox = form.querySelector('[data-form-error]')
  const recovery = form.querySelector('[data-recovery]')
  const duplicateBox = form.querySelector('[data-duplicate]')
  const pow = createPow()
  let step = Math.max(0, steps.findIndex((s) => s.hasAttribute('data-current')))
  let sending = false

  const field = (name) => form.elements.namedItem(name)
  const choice = () => form.querySelector('input[name="offer"]:checked')?.value || ''
  const money = (v) => formatMad(v, msg.currency)

  // ------------------------------------------------------------ errors
  function setError(name, text) {
    const el =
      name === 'choice' ? form.querySelector('#choice-error') : form.querySelector(`#${FIELD_IDS[name]}-error`)
    const input = name === 'choice' ? null : form.querySelector(`#${FIELD_IDS[name]}`)
    if (el) {
      el.textContent = text || ''
      el.hidden = !text
    }
    if (input) {
      if (text) input.setAttribute('aria-invalid', 'true')
      else input.removeAttribute('aria-invalid')
      const ids = (input.getAttribute('aria-describedby') || '').split(' ').filter((id) => id && id !== el?.id)
      if (text && el) ids.unshift(el.id)
      if (ids.length) input.setAttribute('aria-describedby', ids.join(' '))
      else input.removeAttribute('aria-describedby')
    }
  }

  function formError(text, withRecovery = false) {
    errorBox.querySelector('[data-form-error-text]').textContent = text || ''
    errorBox.hidden = !text
    recovery.hidden = !withRecovery
  }

  // ---------------------------------------------------- client checks
  function validateStep(i) {
    const errs = {}
    if (i === 0) {
      const c = choice()
      if (!c) errs.choice = msg.itemsRequired
      if (c === 'custom') {
        const radio = form.querySelector('input[name="offer"][value="custom"]')
        const raw = field('liters').value.trim()
        const n = Number.parseInt(raw, 10)
        if (!raw) errs.customLiters = msg.litersRequired
        else if (!(n >= Number(radio.dataset.min) && n <= Number(radio.dataset.max))) errs.customLiters = msg.litersRange
      }
    }
    if (i === 1) {
      const name = field('name').value.trim()
      if (name.length < 2) errs.name = msg.nameRequired
      else if (name.length > 80) errs.name = msg.nameTooLong
      const phone = field('phone').value.trim()
      if (!phone) errs.phone = msg.phoneRequired
      else if (!normalizePhone(phone)) errs.phone = msg.phoneInvalid
      const city = field('city').value.trim()
      if (city.length < 2) errs.city = msg.cityRequired
      else if (city.length > 80) errs.city = msg.cityTooLong
      if (field('address').value.trim().length > 300) errs.address = msg.addressTooLong
      if (field('note').value.trim().length > 500) errs.note = msg.noteTooLong
    }
    const names = i === 0 ? ['choice', 'customLiters'] : i === 1 ? ['name', 'phone', 'city', 'address', 'note'] : []
    for (const n of names) setError(n, errs[n])
    return Object.keys(errs).length === 0
  }

  function focusFirstInvalid() {
    requestAnimationFrame(() => {
      const el = steps[step].querySelector('[aria-invalid="true"]') || steps[step].querySelector('input[name="offer"]:not(:disabled)')
      el?.focus()
    })
  }

  // ---------------------------------------------------------- totals
  function lines() {
    const c = choice()
    if (!c) return []
    const radio = form.querySelector(`input[name="offer"][value="${CSS.escape(c)}"]`)
    if (c === 'custom') {
      const n = Number.parseInt(field('liters').value, 10)
      if (!(n > 0)) return []
      const price = Math.round(Number(radio.dataset.pricePerLiter) * n * 100) / 100
      return [{ label: fill(msg.customLine, { liters: n }), amount: price, quantity: 1 }]
    }
    return [{ label: radio.dataset.label, amount: Number(radio.dataset.price), quantity: 1 }]
  }

  function totals() {
    const subtotal = Math.round(lines().reduce((s, l) => s + l.amount * l.quantity, 0) * 100) / 100
    const mode = form.dataset.deliveryMode
    const fee = form.dataset.deliveryFee === '' ? null : Number(form.dataset.deliveryFee)
    const freeAbove = form.dataset.deliveryFreeAbove === '' ? null : Number(form.dataset.deliveryFreeAbove)
    let delivery = null
    if (mode === 'flat' && fee !== null) delivery = fee
    if (mode === 'free_above' && fee !== null) delivery = freeAbove !== null && subtotal >= freeAbove ? 0 : fee
    return { subtotal, delivery, total: Math.round((subtotal + (delivery ?? 0)) * 100) / 100 }
  }

  function renderTotals() {
    const t = totals()
    const label = t.delivery === null ? msg.totalWithoutDelivery : msg.total
    for (const el of form.querySelectorAll('[data-total]')) el.textContent = money(t.total)
    for (const el of form.querySelectorAll('[data-total-label]')) el.textContent = label
    form.querySelector('[data-subtotal]').textContent = money(t.subtotal)
    form.querySelector('[data-delivery]').textContent =
      t.delivery === null ? msg.deliveryConfirm : t.delivery === 0 ? msg.deliveryFree : money(t.delivery)
  }

  function renderReview() {
    const list = form.querySelector('[data-review-lines]')
    list.replaceChildren(
      ...lines().map((l) => {
        const li = document.createElement('li')
        li.className = 'flex justify-between gap-3'
        const a = document.createElement('span')
        a.textContent = l.label
        const b = document.createElement('span')
        b.className = 'shrink-0 font-medium tabular-nums'
        b.textContent = money(l.amount * l.quantity)
        li.append(a, b)
        return li
      }),
    )
    const phone = field('phone').value.trim()
    const e164 = normalizePhone(phone)
    const values = {
      name: field('name').value.trim(),
      phone: e164 ? displayPhone(e164) : phone,
      city: field('city').value.trim(),
      address: field('address').value.trim(),
      note: field('note').value.trim() ? `\u201c${field('note').value.trim()}\u201d` : '',
    }
    for (const [k, v] of Object.entries(values)) {
      const el = form.querySelector(`[data-review="${k}"]`)
      el.textContent = v
      el.hidden = !v
    }
  }

  // ----------------------------------------------------------- steps
  function show(next, focus = true) {
    step = next
    steps.forEach((s, i) => s.toggleAttribute('data-current', i === step))
    for (const b of form.querySelectorAll('[data-stepper] [data-goto]')) {
      const i = Number(b.dataset.goto)
      b.disabled = i >= step
      b.toggleAttribute('data-done', i < step)
      if (i === step) b.setAttribute('aria-current', 'step')
      else b.removeAttribute('aria-current')
    }
    form.querySelectorAll('[data-step-line]').forEach((l, i) => l.toggleAttribute('data-done', i < step))
    form.querySelector('[data-step-status]').textContent = `${fill(msg.stepOf, { current: step + 1, total: 3 })}: ${msg[`step${step + 1}`]}`
    back.hidden = step === 0
    if (!sending) submit.textContent = step < 2 ? submit.dataset.labelNext : submit.dataset.labelSubmit
    submit.toggleAttribute('data-final', step === 2)
    if (step === 2) renderReview()
    saveDraft()
    if (focus) requestAnimationFrame(() => steps[step].querySelector('[data-step-heading]')?.focus())
  }

  function goToFirstInvalid(fieldNames) {
    const target = Math.min(...fieldNames.map((n) => STEP_OF[n] ?? 2), step)
    show(target, false)
    focusFirstInvalid()
  }

  // ----------------------------------------------------------- draft
  function saveDraft() {
    try {
      const values = {}
      for (const f of DRAFT_FIELDS) {
        const v = f === 'offer' ? choice() : field(f)?.value
        if (v) values[f] = v
      }
      if (!Object.keys(values).length && step === 0) localStorage.removeItem(DRAFT_KEY)
      else localStorage.setItem(DRAFT_KEY, JSON.stringify({ at: Date.now(), values, step }))
    } catch {}
  }

  function restoreDraft() {
    try {
      const d = JSON.parse(localStorage.getItem(DRAFT_KEY) || 'null')
      if (!d?.at || Date.now() - d.at > DRAFT_TTL) {
        localStorage.removeItem(DRAFT_KEY)
        return
      }
      for (const f of DRAFT_FIELDS) {
        const v = d.values?.[f]
        if (typeof v !== 'string' || v.length > 600) continue
        if (f === 'offer') {
          const radio = form.querySelector(`input[name="offer"][value="${CSS.escape(v)}"]:not(:disabled)`)
          if (radio && !choice()) radio.checked = true
        } else if (field(f) && !field(f).value) {
          field(f).value = v
        }
      }
      if (Number.isInteger(d.step)) step = Math.min(Math.max(d.step, 0), 2)
    } catch {}
  }

  const clearDraft = () => {
    try {
      localStorage.removeItem(DRAFT_KEY)
    } catch {}
  }

  // ----------------------------------------------------- city search
  function initCity() {
    const input = form.querySelector('#order-city')
    const list = form.querySelector('#order-city-list')
    let active = -1
    let items = []
    input.setAttribute('role', 'combobox')
    input.setAttribute('aria-autocomplete', 'list')
    input.setAttribute('aria-controls', list.id)
    input.setAttribute('aria-expanded', 'false')
    const close = () => {
      list.hidden = true
      input.setAttribute('aria-expanded', 'false')
      input.removeAttribute('aria-activedescendant')
      active = -1
    }
    const render = () => {
      items = searchCities(input.value, locale)
      if (items.length === 1 && items[0] === input.value) items = []
      list.replaceChildren(
        ...items.map((city, i) => {
          const opt = document.createElement('div')
          opt.id = `${list.id}-${i}`
          opt.setAttribute('role', 'option')
          opt.setAttribute('aria-selected', String(i === active))
          opt.className = `flex min-h-11 cursor-pointer items-center px-4 text-ink ${i === active ? 'bg-oil-soft' : 'hover:bg-sand/60'}`
          opt.textContent = city
          opt.addEventListener('mousedown', (e) => e.preventDefault())
          opt.addEventListener('click', () => pick(city))
          return opt
        }),
      )
      list.hidden = items.length === 0
      input.setAttribute('aria-expanded', String(!list.hidden))
      if (active >= 0) input.setAttribute('aria-activedescendant', `${list.id}-${active}`)
      else input.removeAttribute('aria-activedescendant')
    }
    const pick = (city) => {
      input.value = city
      close()
      saveDraft()
    }
    input.addEventListener('input', () => {
      active = -1
      render()
    })
    input.addEventListener('focus', render)
    input.addEventListener('blur', () => setTimeout(close, 120))
    input.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        active = Math.min(active + 1, items.length - 1)
        render()
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        active = Math.max(active - 1, 0)
        render()
      } else if (e.key === 'Enter' && !list.hidden && active >= 0) {
        e.preventDefault()
        pick(items[active])
      } else if (e.key === 'Escape' && !list.hidden) {
        e.preventDefault()
        close()
      }
    })
  }

  // ------------------------------------------------------------ send
  function whatsappFallback() {
    const number = form.dataset.waNumber
    if (!number) return null
    const items = lines()
      .map((l) => (choice() === 'custom' ? l.label : `${l.quantity} \u00d7 ${l.label}`))
      .join(locale === 'ar' ? '\u060c ' : ', ')
    const dash = '-'
    const text = fill(msg.whatsappOrder, {
      items: items || dash,
      name: field('name').value.trim() || dash,
      phone: field('phone').value.trim() || dash,
      city: field('city').value.trim() || dash,
      address: field('address').value.trim() || dash,
    })
    return `https://wa.me/${number}?text=${encodeURIComponent(text)}`
  }

  async function attempt(confirmDuplicate) {
    form.querySelector('[data-altcha]').value = await pow.take()
    const tags = currentAttribution()
    form.querySelector('[data-attribution]').value = tags ? JSON.stringify(tags) : ''
    const body = new URLSearchParams(new FormData(form))
    body.delete('confirm_duplicate')
    if (confirmDuplicate) body.set('confirm_duplicate', '1')
    const res = await fetch(form.action, { method: 'POST', body, headers: { accept: 'application/json' }, credentials: 'same-origin' })
    if (!res.ok && res.status !== 422) throw new Error(`HTTP ${res.status}`)
    return res.json()
  }

  async function send(confirmDuplicate) {
    if (sending) return
    sending = true
    formError('')
    duplicateBox.hidden = true
    submit.disabled = true
    submit.textContent = submit.dataset.labelSending
    startProgress()
    let result
    try {
      try {
        result = await attempt(confirmDuplicate)
      } catch {
        // A dropped connection on a phone: wait a moment and try once more.
        await new Promise((r) => setTimeout(r, 1200))
        result = await attempt(confirmDuplicate)
      }
    } catch {
      doneProgress()
      sending = false
      submit.disabled = false
      submit.textContent = submit.dataset.labelSubmit
      const wa = whatsappFallback()
      const link = recovery.querySelector('[data-wa-placement]')
      if (link && wa) link.href = wa
      formError(msg.errorNetwork, true)
      return
    }
    if (result.status === 'created' && result.redirect) {
      clearDraft()
      visit(result.redirect)
      return
    }
    doneProgress()
    sending = false
    submit.disabled = false
    submit.textContent = submit.dataset.labelSubmit
    if (result.status === 'invalid') {
      for (const name of Object.keys(STEP_OF)) setError(name, result.errors?.[name])
      formError(result.formError)
      goToFirstInvalid(Object.keys(result.errors || {}))
    } else if (result.status === 'duplicate') {
      duplicateBox.querySelector('[data-duplicate-text]').textContent = result.duplicate
      duplicateBox.hidden = false
      show(2, false)
    } else {
      formError(result.formError || msg.errorRejected)
      if (Number.isInteger(result.step)) show(result.step, false)
    }
  }

  // ---------------------------------------------------------- events
  form.addEventListener('submit', (e) => {
    e.preventDefault()
    const confirm = e.submitter?.hasAttribute('data-confirm-duplicate')
    if (step < 2) {
      if (validateStep(step)) show(step + 1)
      else focusFirstInvalid()
      return
    }
    for (const i of [0, 1]) {
      if (!validateStep(i)) {
        show(i, false)
        focusFirstInvalid()
        return
      }
    }
    send(Boolean(confirm))
  })
  back.addEventListener('click', () => show(Math.max(0, step - 1)))
  for (const b of form.querySelectorAll('[data-goto]')) {
    b.addEventListener('click', () => {
      const i = Number(b.dataset.goto)
      if (i < step) show(i)
    })
  }
  let saveTimer
  form.addEventListener('input', (e) => {
    if (e.target.name === 'liters') e.target.value = e.target.value.replace(/\D/g, '').slice(0, 3)
    if (e.target.name === 'offer' && e.target.value === 'custom') requestAnimationFrame(() => field('liters')?.focus())
    renderTotals()
    clearTimeout(saveTimer)
    saveTimer = setTimeout(saveDraft, 250)
  })

  // An offer picked on a card (landing.js) selects it and brings step 1 back.
  // The listener goes with the page (signal) when another page is swapped in.
  window.addEventListener(
    'offer:select',
    (e) => {
      const radio = form.querySelector(`input[name="offer"][value="${CSS.escape(e.detail)}"]:not(:disabled)`)
      if (!radio) return
      radio.checked = true
      renderTotals()
      show(0, false)
      if (e.detail === 'custom') requestAnimationFrame(() => field('liters')?.focus())
    },
    { signal },
  )

  // Solve the proof of work while the visitor reads, once the form is near.
  if ('IntersectionObserver' in window) {
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((en) => en.isIntersecting)) {
          pow.warm()
          io.disconnect()
        }
      },
      { rootMargin: '600px 0px' },
    )
    io.observe(form)
    signal?.addEventListener('abort', () => io.disconnect())
  } else {
    pow.warm()
  }

  // A server re-render (with errors) wins over the draft.
  if (errorBox.hidden && duplicateBox.hidden) restoreDraft()
  form.dataset.enhanced = ''
  initCity()
  renderTotals()
  show(step, false)
}

export function clearOrderDraft() {
  try {
    localStorage.removeItem(DRAFT_KEY)
  } catch {}
}
