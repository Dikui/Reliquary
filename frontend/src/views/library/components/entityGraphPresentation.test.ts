import assert from 'node:assert/strict'
import test from 'node:test'
import { graphElements, graphStyles, overviewStyles } from './entityGraphPresentation'
import type { GraphSearchResponse } from '../../../types/graph'
const data = {
  entities: ['center', 'near', 'far'].map(id => ({ id, canonical_name: id, entity_type: 'company' })),
  edges: [
    { id: 'a', subject_id: 'near', object_id: 'center', relation_type: 'owns' },
    { id: 'b', subject_id: 'near', object_id: 'far', relation_type: 'uses' },
    { id: 'c', subject_id: 'center', object_id: 'near', relation_type: 'funds' },
  ],
} as GraphSearchResponse

test('graph assigns shortest undirected hop distance while preserving edge direction and parallel relations', () => {
  const elements = graphElements(data, 'center')
  assert.deepEqual(elements.slice(0, 3).map(item => [item.data.id, item.classes]), [
    ['entity:center', 'center'], ['entity:near', 'near'], ['entity:far', 'far'],
  ])
  assert.deepEqual(elements.slice(3).map(item => [item.data.source, item.data.target, item.data.label]), [
    ['entity:near', 'entity:center', 'owns'], ['entity:near', 'entity:far', 'uses'], ['entity:center', 'entity:near', 'funds'],
  ])
})

test('overview nodes grow with degree and edge labels only show when highlighted', () => {
  assert.deepEqual(graphElements(data, '').slice(0, 3).map(item => item.data.degree), [2, 3, 1])
  const styles = overviewStyles({ text: '#111', bg: '#fff', muted: '#888', border: '#ccc', select: '#fc0', onSelect: '#111', font: 'Helvetica', accent: '#e2231a' })
  const style = (selector: string) => (styles.find(item => item.selector === selector)! as unknown as { style: Record<string, unknown> }).style
  assert.equal(style('edge').label, undefined)
  assert.equal(style('edge.highlight, edge.selected').label, 'data(label)')
  assert.equal(style('node.highlight')['background-color'], '#e2231a')
})

test('both themes use inverted center, neutral distant nodes and readable yellow selected edges', () => {
  for (const dark of [false, true]) {
    const colors = { text: dark ? '#f2f2f2' : '#111111', bg: dark ? '#0d0d0d' : '#ffffff', muted: '#888888', border: '#555555', select: '#ffcc00', onSelect: '#111111', font: 'Helvetica' }
    const styles = graphStyles(colors)
    const style = (selector: string) => (styles.find(item => item.selector === selector)! as unknown as { style: Record<string, unknown> }).style as Record<string, unknown>
    assert.equal(style('node.center')['background-color'], colors.text)
    assert.equal(style('node.center').color, colors.bg)
    assert.equal(style('node.far').color, colors.muted)
    assert.equal(style('edge.selected')['line-color'], colors.select)
    assert.equal(style('edge.selected')['text-background-color'], colors.select)
    assert.equal(style('edge.selected').color, colors.onSelect)
  }
})
