import { describe, it, expect } from 'vitest'
import { autoLayout } from './workflow-layout'

describe('autoLayout', () => {
  const nodes = [{ id: 'n1' }, { id: 'n2' }, { id: 'n3' }]
  const edges = [
    { source: 'n1', target: 'n2' },
    { source: 'n2', target: 'n3' },
  ]

  it('returns a coordinate for every node', () => {
    const pos = autoLayout(nodes, edges)
    expect(Object.keys(pos).sort()).toEqual(['n1', 'n2', 'n3'])
  })

  it('produces finite, grid-aligned (20px) coordinates', () => {
    const pos = autoLayout(nodes, edges)
    for (const id of Object.keys(pos)) {
      const { x, y } = pos[id]
      expect(Number.isFinite(x)).toBe(true)
      expect(Number.isFinite(y)).toBe(true)
      expect(x % 20).toBe(0)
      expect(y % 20).toBe(0)
    }
  })

  it('is deterministic across calls', () => {
    const a = autoLayout(nodes, edges)
    const b = autoLayout([...nodes].reverse(), edges)
    // Same graph → same coordinates per id, regardless of input order.
    expect(b).toEqual(a)
  })

  it('lays out a chain top-to-bottom (source above target)', () => {
    const pos = autoLayout(nodes, edges)
    expect(pos.n1.y).toBeLessThan(pos.n2.y)
    expect(pos.n2.y).toBeLessThan(pos.n3.y)
  })

  it('ignores stale edges that target an unknown node', () => {
    const pos = autoLayout(nodes, [...edges, { source: 'n1', target: 'ghost' }])
    expect(pos.n1).toBeDefined()
    expect(pos.ghost).toBeUndefined()
  })
})
