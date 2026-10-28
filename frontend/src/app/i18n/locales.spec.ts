import { describe, expect, it } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import es from './locales/es.json'
import en from './locales/en.json'
import { normalizeLocale } from '.'

// PP-F1-FE-010: i18n es/en completo. Paridad de claves, sin textos vacíos y
// toda clave estática usada en el código (t('…')) existe en ambos idiomas.
type Tree = { [k: string]: string | Tree }

function flatten(tree: Tree, prefix = ''): Map<string, string> {
  const out = new Map<string, string>()
  for (const [k, v] of Object.entries(tree)) {
    const key = prefix + k
    if (typeof v === 'string') out.set(key, v)
    else for (const [kk, vv] of flatten(v, key + '.')) out.set(kk, vv)
  }
  return out
}

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) return sources(p)
    return /\.(vue|ts)$/.test(name) && !/\.(spec|d)\.ts$/.test(name) ? [p] : []
  })
}

const esKeys = flatten(es as Tree)
const enKeys = flatten(en as Tree)

describe('PP-F1-FE-010 locales', () => {
  it('es y en tienen exactamente las mismas claves', () => {
    expect([...esKeys.keys()].filter((k) => !enKeys.has(k))).toEqual([])
    expect([...enKeys.keys()].filter((k) => !esKeys.has(k))).toEqual([])
  })

  // Vacíos intencionados: claves dinámicas sin texto para algún valor.
  const INTENTIONALLY_EMPTY = new Set(['orgs.status.active_hint'])

  it('ningún texto está vacío', () => {
    const empty = [...esKeys, ...enKeys]
      .filter(([k, v]) => v.trim() === '' && !INTENTIONALLY_EMPTY.has(k))
      .map(([k]) => k)
    expect(empty).toEqual([])
  })

  it('toda clave estática usada en el código existe', () => {
    const root = join(__dirname, '..', '..')
    const used = new Set<string>()
    for (const file of sources(root)) {
      const text = readFileSync(file, 'utf8')
      for (const m of text.matchAll(/\bt\(\s*'([a-zA-Z][\w.]*)'/g)) used.add(m[1]!)
    }
    expect(used.size).toBeGreaterThan(100)
    expect([...used].filter((k) => !esKeys.has(k) || !enKeys.has(k))).toEqual([])
  })

  it('normaliza etiquetas BCP 47 a idiomas soportados', () => {
    expect(normalizeLocale('en-US')).toBe('en')
    expect(normalizeLocale('ES')).toBe('es')
    expect(normalizeLocale('fr-FR')).toBeNull()
    expect(normalizeLocale(undefined)).toBeNull()
  })
})
