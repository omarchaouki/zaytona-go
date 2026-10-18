// Thin loading line at the top (Livewire / NProgress style): waits 150 ms so
// fast responses show nothing, creeps forward, then runs to the end and fades.
// Shown on same-origin page navigations and form submits. Driven through the
// --progress custom property (CSSOM, allowed by the CSP).

const SHOW_DELAY_MS = 150
const TRICKLE_MS = 250

let root
let bar
let value = 0
let showTimer
let trickleTimer

// The line lives in <body>: after an in-place page swap it is put back.
function mount() {
  if (root) {
    if (!root.isConnected) document.body.append(root)
    return
  }
  root = document.createElement('div')
  root.className = 'top-progress'
  root.setAttribute('aria-hidden', 'true')
  bar = document.createElement('div')
  bar.className = 'top-progress-bar'
  const peg = document.createElement('div')
  peg.className = 'top-progress-peg'
  bar.append(peg)
  root.append(bar)
  document.body.append(root)
}

const render = () => bar?.style.setProperty('--progress', String(value))

function trickle() {
  const step = value < 0.2 ? 0.1 : value < 0.5 ? 0.04 : value < 0.8 ? 0.02 : 0.005
  value = Math.min(0.994, value + step * (0.5 + Math.random()))
  render()
}

export function startProgress() {
  window.clearTimeout(showTimer)
  showTimer = window.setTimeout(() => {
    mount()
    root.classList.remove('is-done')
    root.classList.add('is-active')
    value = 0.08
    render()
    window.clearInterval(trickleTimer)
    trickleTimer = window.setInterval(trickle, TRICKLE_MS)
  }, SHOW_DELAY_MS)
}

export function doneProgress() {
  window.clearTimeout(showTimer)
  window.clearInterval(trickleTimer)
  if (!root?.classList.contains('is-active')) return
  mount()
  value = 1
  render()
  root.classList.add('is-done')
  window.setTimeout(() => {
    root.classList.remove('is-active', 'is-done')
    value = 0
    render()
  }, 500)
}

export function initProgress() {
  document.addEventListener('click', (event) => {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return
    }
    const a = event.target instanceof Element ? event.target.closest('a[href]') : null
    if (!a || a.target === '_blank' || a.hasAttribute('download')) return
    const url = new URL(a.href, location.href)
    if (url.origin !== location.origin) return
    // Same page, other fragment: no navigation.
    if (url.pathname === location.pathname && url.search === location.search && url.hash) return
    startProgress()
  })
  document.addEventListener('submit', (event) => {
    if (!event.defaultPrevented) startProgress()
  })
  // Back/forward cache restores the page as it was: clear the line.
  window.addEventListener('pageshow', doneProgress)
}
