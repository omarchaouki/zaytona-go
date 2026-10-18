// Live new-order notifications: polls /admin/notifications every 10 s (a small
// server-rendered fragment), shows the newest unseen order, updates the red
// badges and "(3)" in the tab title, and chimes when a new order arrives.
// Closed notifications stay closed for this browser session.

const POLL_MS = 10_000
const SOUND_KEY = 'admin-new-order-sound'
const DISMISSED_KEY = 'admin-dismissed-orders'

// ---------------------------------------------------------------- chime
// Browsers keep audio muted until the first click or key press, and an
// AudioContext created before that stays suspended. So there is one shared
// context, resumed on the first gesture; later chimes reuse it.
let audio = null
const unlockListeners = new Set()

function context() {
  if (!audio) {
    const Ctx = window.AudioContext ?? window.webkitAudioContext
    if (!Ctx) return null
    audio = new Ctx()
  }
  return audio
}

const audioUnlocked = () => audio?.state === 'running'

function armAudioUnlock() {
  const unlock = () => {
    const c = context()
    if (!c) return
    c.resume().then(() => {
      if (c.state !== 'running') return
      window.removeEventListener('pointerdown', unlock)
      window.removeEventListener('keydown', unlock)
      for (const fn of unlockListeners) fn()
    })
  }
  window.addEventListener('pointerdown', unlock)
  window.addEventListener('keydown', unlock)
}

// Three rising notes, about 0.7 s.
function chime() {
  const c = context()
  if (!c) return false
  if (c.state !== 'running') {
    c.resume()
    return false
  }
  const start = c.currentTime + 0.02
  for (const [i, freq] of [660, 880, 1320].entries()) {
    const t = start + i * 0.16
    const osc = c.createOscillator()
    const gain = c.createGain()
    osc.type = 'sine'
    osc.frequency.value = freq
    gain.gain.setValueAtTime(0.0001, t)
    gain.gain.exponentialRampToValueAtTime(0.3, t + 0.02)
    gain.gain.exponentialRampToValueAtTime(0.0001, t + 0.35)
    osc.connect(gain).connect(c.destination)
    osc.start(t)
    osc.stop(t + 0.4)
  }
  return true
}

function storage(kind, fn) {
  try {
    return fn(window[kind])
  } catch {
    return null
  }
}

const soundOn = () => storage('localStorage', (s) => s.getItem(SOUND_KEY)) !== '0'

// ---------------------------------------------------------------- bell
function initSoundToggles() {
  const toggles = [...document.querySelectorAll('[data-sound-toggle]')]
  const render = () => {
    const on = soundOn()
    const blocked = on && !audioUnlocked()
    for (const b of toggles) {
      const label = blocked ? b.dataset.labelBlocked : on ? b.dataset.labelOn : b.dataset.labelOff
      b.hidden = false
      b.setAttribute('aria-pressed', String(on))
      b.setAttribute('aria-label', label)
      b.title = label
      b.querySelector('[data-sound-on]')?.toggleAttribute('hidden', !on)
      b.querySelector('[data-sound-off]')?.toggleAttribute('hidden', on)
      b.classList.toggle('text-oil-bright', on && !blocked)
    }
  }
  for (const b of toggles) {
    b.addEventListener('click', () => {
      if (soundOn() && !audioUnlocked()) {
        window.setTimeout(() => {
          chime()
          render()
        }, 60)
        return
      }
      const next = !soundOn()
      storage('localStorage', (s) => s.setItem(SOUND_KEY, next ? '1' : '0'))
      if (next) chime()
      render()
    })
  }
  // One page at a time: the header (and its bell) is replaced on each swap.
  unlockListeners.clear()
  unlockListeners.add(render)
  render()
}

// ---------------------------------------------------------------- poll
function dismissed() {
  const raw = storage('sessionStorage', (s) => s.getItem(DISMISSED_KEY))
  try {
    return raw ? JSON.parse(raw) : []
  } catch {
    return []
  }
}

function dismiss(ids) {
  const next = [...new Set([...dismissed(), ...ids])].slice(-50)
  storage('sessionStorage', (s) => s.setItem(DISMISSED_KEY, JSON.stringify(next)))
}

// The poll runs once for the whole visit; pages swapped in place get the
// current count and cards at once. Elements are looked up at each use.
let polling = false
let armed = false
let pollNow = () => {}

export function initNotifications() {
  initSoundToggles()
  if (polling) {
    pollNow()
    return
  }
  if (!document.querySelector('[data-notifications]')) return
  polling = true

  let known = null
  let shown = ''
  let timer

  const setCount = (count) => {
    const baseTitle = document.title.replace(/^\(\d+\)\s*/, '')
    document.title = count > 0 ? `(${count}) ${baseTitle}` : baseTitle
    for (const badge of document.querySelectorAll('[data-unseen-badge]')) {
      badge.textContent = String(count)
      badge.hidden = count === 0
    }
  }

  async function poll() {
    window.clearTimeout(timer)
    const box = document.querySelector('[data-notifications]')
    if (!box) {
      // Signed out (the login page has no box): stop until the next sign-in.
      polling = false
      return
    }
    const params = new URLSearchParams()
    const skip = dismissed()
    if (skip.length) params.set('skip', skip.join(','))
    try {
      const res = await fetch(`/admin/notifications?${params}`, {
        headers: { 'X-Requested-With': 'fetch' },
        credentials: 'same-origin',
        cache: 'no-store',
      })
      const target = res.headers.get('X-Redirect')
      if (target && target.startsWith('/admin')) {
        window.location.assign(target)
        return
      }
      if (res.ok) {
        const doc = new DOMParser().parseFromString(await res.text(), 'text/html')
        const root = doc.querySelector('[data-notify-root]')
        if (root) {
          const count = Number(root.dataset.count || 0)
          const ids = (root.dataset.ids || '').split(',').filter(Boolean)
          if (known !== null && ids.some((id) => !known.has(id)) && soundOn()) chime()
          known = new Set(ids)
          setCount(count)
          // Swap only when something changed (the card animates in once), or
          // when a new page brought an empty box.
          if (root.innerHTML !== shown || box.childElementCount !== root.childElementCount) {
            shown = root.innerHTML
            box.replaceChildren(...[...root.children].map((n) => document.importNode(n, true)))
          }
        }
      }
    } catch {
      // Offline or server restarting: try again at the next tick.
    }
    timer = window.setTimeout(poll, POLL_MS)
  }
  pollNow = poll
  if (armed) {
    poll()
    return
  }
  armed = true
  armAudioUnlock()

  document.addEventListener('click', async (event) => {
    if (!(event.target instanceof Element) || !event.target.closest('[data-notifications]')) return
    const close = event.target.closest('[data-notify-close]')
    if (close) {
      dismiss(close.dataset.notifyClose.split(','))
      pollNow()
      return
    }
    const all = event.target.closest('[data-notify-seen-all]')
    if (all) {
      event.preventDefault()
      const token = document.body.dataset.csrf
      const body = new URLSearchParams({ csrf: token })
      await fetch('/admin/orders/seen-all', {
        method: 'POST',
        body,
        headers: { 'X-Requested-With': 'fetch', 'X-CSRF-Token': token },
        credentials: 'same-origin',
      }).catch(() => {})
      pollNow()
    }
  })

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && polling) pollNow()
  })
  poll()
}
