// Runs in the page (ported from the reference scripts/audit-mobile.ts).
// Returns findings: elements sticking out of the screen, content spilling out
// of its own box, text under 12px, tap targets under 44px, cramped fields,
// and fixed elements overlapping each other.
(() => {
  const vw = document.documentElement.clientWidth
  const out = []
  const sectionOf = (el) => {
    const s = el.closest('section, header, footer, nav, main')
    return (s && (s.getAttribute('aria-labelledby') || s.getAttribute('aria-label') || s.tagName.toLowerCase())) || 'body'
  }
  const label = (el) =>
    `${el.tagName.toLowerCase()}${el.id ? `#${el.id}` : ''}.${String(el.className).split(' ').slice(0, 3).join('.')} "${(el.textContent || el.getAttribute('aria-label') || '').trim().slice(0, 40)}"`
  // Inside intended horizontal scrollers, sticking out is the point. Content
  // cut off by an overflow-hidden box is still a bug unless it is decoration.
  const inScroller = (el) => {
    if (el.closest('.marquee')) return true
    for (let p = el.parentElement; p; p = p.parentElement) {
      const ox = getComputedStyle(p).overflowX
      if (ox === 'auto' || ox === 'scroll') return true
    }
    for (let p = el.parentElement; p; p = p.parentElement) {
      const ox = getComputedStyle(p).overflowX
      // Single-line text cut with an ellipsis is designed truncation.
      if (ox === 'hidden' && getComputedStyle(p).textOverflow === 'ellipsis') return true
      if (ox === 'hidden' || ox === 'clip') {
        return getComputedStyle(el).position === 'absolute' || Boolean(el.closest('.absolute, [aria-hidden="true"]'))
      }
    }
    return false
  }
  if (document.documentElement.scrollWidth > vw + 1) {
    out.push({ kind: 'page-overflow', section: 'html', what: `scrollWidth ${document.documentElement.scrollWidth} > ${vw}` })
  }
  for (const el of document.querySelectorAll('body *')) {
    const r = el.getBoundingClientRect()
    const cs = getComputedStyle(el)
    if (!r.width || cs.visibility === 'hidden' || cs.display === 'none') continue
    if (el.closest('[aria-hidden="true"], [inert]') && !el.matches('a,button')) continue
    if (el.closest('[inert]')) continue
    if ((r.right > vw + 1 || r.left < -1) && !inScroller(el) && cs.position !== 'fixed') {
      out.push({ kind: 'overflow', section: sectionOf(el), what: `${label(el)} ${Math.round(r.left)}->${Math.round(r.right)} (vw ${vw})` })
    }
    const sticksOut = (c) => {
      const cr = c.getBoundingClientRect()
      return cr.right > r.right + 2 || cr.left < r.left - 2
    }
    const kids = [...el.children]
    const onlyBadge =
      kids.some((c) => getComputedStyle(c).position === 'absolute' && sticksOut(c)) &&
      !kids.some((c) => getComputedStyle(c).position !== 'absolute' && sticksOut(c))
    if (!el.closest('svg') && !onlyBadge && cs.display !== 'inline' && cs.overflowX === 'visible' &&
      el.scrollWidth > el.clientWidth + 2 && el.clientWidth > 0 && !inScroller(el)) {
      out.push({ kind: 'spills', section: sectionOf(el), what: `${label(el)} content ${el.scrollWidth}px in ${el.clientWidth}px` })
    }
    if (el.matches('input:not([type=checkbox]):not([type=radio]):not([type=hidden]):not([type=file]), textarea, select') &&
      r.width < (el.matches('select') ? 72 : 120)) {
      out.push({ kind: 'cramped-field', section: sectionOf(el), what: `${label(el)} ${Math.round(r.width)}px wide` })
    }
    const text = [...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim())
    // SVG text scales with its drawing; its font-size is in drawing units.
    if (text && !el.closest('svg') && Number.parseFloat(cs.fontSize) < 12) {
      out.push({ kind: 'tiny-text', section: sectionOf(el), what: `${label(el)} ${cs.fontSize}` })
    }
    if (el.matches('a[href],button,input:not([type=hidden]),select,textarea,[role=tab],summary')) {
      const inline = el.matches('p a, li > a:only-child') && cs.display === 'inline'
      const box = el.closest('label')?.getBoundingClientRect()
      const inBigLabel = Boolean(box && box.height >= 43.5 && box.width >= 43.5)
      if (!inline && !inBigLabel && !el.classList.contains('sr-only') && (r.height < 43.5 || r.width < 43.5) && r.height > 2) {
        out.push({ kind: 'small-target', section: sectionOf(el), what: `${label(el)} ${Math.round(r.width)}x${Math.round(r.height)}` })
      }
    }
  }
  // Fixed controls (floating WhatsApp, sticky bar, notification cards) must not
  // cover each other. A modal dialog sits in the top layer over an inert page:
  // covering is its job, so it and the page behind it are left out.
  const modal = document.querySelector(':modal')
  const fixed = [...document.querySelectorAll('body *')].filter((el) => {
    if (modal && (el === modal || !modal.contains(el))) return false
    const cs = getComputedStyle(el)
    if (cs.position !== 'fixed' || cs.display === 'none' || cs.visibility === 'hidden' || el.closest('[inert]')) return false
    const r = el.getBoundingClientRect()
    return r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < window.innerHeight && !el.classList.contains('top-progress') && !el.parentElement.closest('[class*="fixed"]')
  })
  for (let i = 0; i < fixed.length; i++) {
    for (let j = i + 1; j < fixed.length; j++) {
      const a = fixed[i].getBoundingClientRect()
      const b = fixed[j].getBoundingClientRect()
      if (a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom) {
        out.push({ kind: 'fixed-overlap', section: 'fixed', what: `${label(fixed[i])} x ${label(fixed[j])}` })
      }
    }
  }
  return out
})()
