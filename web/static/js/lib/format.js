// Moroccan number format, identical to domain.FormatNumber on the server:
// "." groups thousands, "," marks decimals, at most 2 decimals, no trailing
// zeros, Western digits in both languages (1250.5 -> "1.250,5").

export function formatNumber(value) {
  const cents = Math.round(Math.abs(value) * 100)
  const neg = value < 0 && cents > 0
  const int = String(Math.floor(cents / 100))
  let frac = String(cents % 100).padStart(2, '0').replace(/0+$/, '')
  let out = ''
  for (let i = 0; i < int.length; i++) {
    if (i > 0 && (int.length - i) % 3 === 0) out += '.'
    out += int[i]
  }
  if (frac) out += `,${frac}`
  return (neg ? '-' : '') + out
}

// "1.250,5 درهم" / "1.250,5 MAD" (currency word from the translations).
export const formatMad = (value, currency) => `${formatNumber(value)} ${currency}`

// Fills {placeholders}; unknown ones stay as they are.
export const fill = (template, values) =>
  template.replace(/\{(\w+)\}/g, (m, k) => (k in values ? String(values[k]) : m))
