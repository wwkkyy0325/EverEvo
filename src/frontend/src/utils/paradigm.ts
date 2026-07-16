export interface ParadigmInfo {
  id: string
  name: string
  icon: string
  strength: number
}

export interface ParadigmMarker {
  paradigm: ParadigmInfo | null
  cleanText: string
}

/**
 * Parse @paradigm{...} JSON marker from the end of assistant message text.
 * Returns the paradigm info and the text with the marker stripped.
 */
export function parseParadigmMarker(text: string): ParadigmInfo | null {
  if (!text) return null
  const matchRe = text.match(/@paradigm\s*(\{[\s\S]*?\})\s*$/)
  if (!matchRe) return null
  try {
    const data = JSON.parse(matchRe[1])
    if (!data.id || !data.name) return null
    return {
      id: data.id,
      name: data.name,
      icon: '',
      strength: data.strength ?? 0.5,
    }
  } catch {
    return null
  }
}
