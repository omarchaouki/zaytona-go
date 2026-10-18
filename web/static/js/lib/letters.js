// Hero headline entrance, letter by letter. The text stays in the page as
// written (no JS: it simply shows); the script wraps it in spans that rise in
// turn and keeps the whole sentence as the accessible name. Arabic letters
// join each other, so Arabic text rises word by word instead.
const ARABIC = /\p{Script=Arabic}/u
const LETTER_STEP = 0.028 // seconds between letters
const WORD_STEP = 0.11 // seconds between Arabic words

export function initLetters() {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches
  for (const el of document.querySelectorAll('[data-letters]')) {
    const text = el.textContent.replace(/\s+/g, ' ').trim()
    if (!text || reduced) {
      el.classList.add('letters-ready')
      continue
    }
    const byWord = ARABIC.test(text)
    const parts = []
    let i = 0
    for (const [n, word] of text.split(' ').entries()) {
      if (n > 0) parts.push(document.createTextNode(' '))
      const w = document.createElement('span')
      w.className = 'word'
      w.setAttribute('aria-hidden', 'true')
      if (byWord) {
        w.classList.add('ch')
        w.style.setProperty('--d', `${(n * WORD_STEP).toFixed(3)}s`)
        w.textContent = word
      } else {
        for (const c of word) {
          const s = document.createElement('span')
          s.className = 'ch'
          s.style.setProperty('--d', `${(i * LETTER_STEP).toFixed(3)}s`)
          s.textContent = c
          w.append(s)
          i++
        }
      }
      parts.push(w)
    }
    el.setAttribute('aria-label', text)
    el.replaceChildren(...parts)
    el.classList.add('letters-ready')
  }
}
