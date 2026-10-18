// The split stage: each [data-stage-split] holds a stack of photographs
// ([data-stage-shot]) and sections that name one ([data-stage]). The shot of
// the section crossing the middle of the screen is shown. Without JS (or
// without IntersectionObserver) the first shot stays, which is the LCP image.
export function initStage(signal) {
  if (!('IntersectionObserver' in window)) return
  for (const split of document.querySelectorAll('[data-stage-split]')) {
    const shots = [...split.querySelectorAll('[data-stage-shot]')]
    const sections = [...split.querySelectorAll('[data-stage]')]
    if (shots.length < 2 || sections.length === 0) continue
    const show = (key) => {
      if (!shots.some((s) => s.dataset.stageShot === key)) return
      for (const s of shots) s.toggleAttribute('data-on', s.dataset.stageShot === key)
    }
    // A thin band across the middle of the viewport: the section inside it wins.
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) if (e.isIntersecting) show(e.target.dataset.stage)
      },
      { rootMargin: '-45% 0px -45% 0px' },
    )
    for (const s of sections) io.observe(s)
    signal?.addEventListener('abort', () => io.disconnect())
  }
}
