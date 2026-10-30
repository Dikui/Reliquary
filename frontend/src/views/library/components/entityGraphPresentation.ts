import type { ElementDefinition, StylesheetJson } from 'cytoscape'
import type { GraphSearchResponse } from '@/types/graph'

// Distance controls visual hierarchy; edge direction remains the stored fact.
// An empty center (whole-library graph) draws every node at full weight.
export function graphElements(data: GraphSearchResponse, center: string): ElementDefinition[] {
  const distances = new Map([[center, 0]])
  const adjacent = new Map<string, string[]>()
  for (const edge of data.edges) {
    adjacent.set(edge.subject_id, [...(adjacent.get(edge.subject_id) || []), edge.object_id])
    adjacent.set(edge.object_id, [...(adjacent.get(edge.object_id) || []), edge.subject_id])
  }
  const queue = [center]
  for (let index = 0; index < queue.length; index++) {
    const id = queue[index]!
    for (const peer of adjacent.get(id) || []) {
      if (distances.has(peer)) continue
      distances.set(peer, distances.get(id)! + 1)
      queue.push(peer)
    }
  }
  return [
    ...data.entities.map(entity => ({
      data: { id: `entity:${entity.id}`, rawID: entity.id, label: entity.canonical_name, degree: adjacent.get(entity.id)?.length || 0 },
      classes: entity.id === center ? 'center' : !center || distances.get(entity.id) === 1 ? 'near' : 'far',
    })),
    ...data.edges.map(edge => ({ data: {
      id: `relation:${edge.id}`, rawID: edge.id, source: `entity:${edge.subject_id}`, target: `entity:${edge.object_id}`, label: edge.relation_type,
    } })),
  ]
}
type GraphColors = { text: string; bg: string; muted: string; border: string; select: string; onSelect: string; font: string; accent?: string }
export function graphStyles(colors: GraphColors): StylesheetJson {
  return [
    { selector: 'node', style: { shape: 'rectangle', width: 144, height: 52, label: 'data(label)', 'background-color': colors.bg, 'border-width': 1.5, 'border-color': colors.text, color: colors.text, 'font-family': colors.font, 'font-size': 14, 'font-weight': 700, 'text-valign': 'center', 'text-halign': 'center', 'text-wrap': 'wrap', 'text-max-width': '130px', 'overlay-opacity': 0 } },
    { selector: 'node.center', style: { 'background-color': colors.text, color: colors.bg, 'border-width': 2 } },
    { selector: 'node.far', style: { color: colors.muted, 'border-color': colors.border } },
    { selector: 'edge', style: { width: 1.5, 'curve-style': 'bezier', 'control-point-step-size': 42, 'target-arrow-shape': 'triangle', 'arrow-scale': 0.8, 'line-color': colors.text, 'target-arrow-color': colors.text, label: 'data(label)', color: colors.text, 'font-family': colors.font, 'font-size': 12, 'font-weight': 700, 'text-background-color': colors.bg, 'text-background-opacity': 1, 'text-background-padding': '4px', 'text-wrap': 'wrap', 'text-max-width': '110px', 'overlay-opacity': 0 } },
    { selector: 'edge.selected', style: { width: 5, 'line-color': colors.select, 'target-arrow-color': colors.select, 'text-background-color': colors.select, color: colors.onSelect } },
  ]
}
// Obsidian-like whole-library graph: dots sized by degree, names hidden on .quiet nodes
// (zoomed out), quiet edges whose relation type appears only when hovered or selected.
export function overviewStyles(colors: GraphColors): StylesheetJson {
  const accent = colors.accent || colors.text
  return [
    { selector: 'node', style: { shape: 'ellipse', width: 'mapData(degree, 1, 8, 10, 30)', height: 'mapData(degree, 1, 8, 10, 30)', 'background-color': colors.muted, 'border-width': 0, label: 'data(label)', color: colors.text, 'font-family': colors.font, 'font-size': 13, 'text-valign': 'bottom', 'text-halign': 'center', 'text-margin-y': 4, 'text-wrap': 'ellipsis', 'text-max-width': '120px', 'overlay-opacity': 0 } },
    { selector: 'node.quiet', style: { label: '' } },
    { selector: 'edge', style: { width: 1, 'curve-style': 'bezier', 'line-color': colors.border, 'target-arrow-shape': 'none', 'overlay-opacity': 0 } },
    { selector: '.faded', style: { opacity: 0.15 } },
    { selector: 'node.highlight', style: { 'background-color': accent, label: 'data(label)', 'font-weight': 700, 'z-index': 10 } },
    { selector: 'edge.highlight, edge.selected', style: { width: 2, 'line-color': accent, 'target-arrow-shape': 'triangle', 'target-arrow-color': accent, 'arrow-scale': 0.7, label: 'data(label)', color: colors.text, 'font-family': colors.font, 'font-size': 11, 'text-background-color': colors.bg, 'text-background-opacity': 1, 'text-background-padding': '2px', 'z-index': 9 } },
  ]
}
