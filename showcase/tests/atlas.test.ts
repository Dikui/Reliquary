import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createGlobePainter } from '../src/atlas'

function countingGlobe() {
  let vertices = 0
  const noop = () => {}
  const countVertex = () => { vertices++ }
  const gradient = () => ({ addColorStop: noop })
  // Stand in for Canvas 2D while counting the vertices sent to its paths.
  const context = {
    moveTo: countVertex, lineTo: countVertex, closePath: noop, beginPath: noop,
    setTransform: noop, clearRect: noop, save: noop, restore: noop,
    fillRect: noop, fill: noop, stroke: noop, roundRect: noop, clip: noop, arc: noop,
    createRadialGradient: gradient, createLinearGradient: gradient,
  }
  const painter = createGlobePainter(context as unknown as CanvasRenderingContext2D)
  return (options: Partial<Parameters<typeof painter.draw>[0]> = {}) => {
    vertices = 0
    painter.draw({
      width: 1024, height: 572, pixelRatio: 1,
      x: 512, y: 286, radius: 250, camera: { yaw: -105, pitch: -23, zoom: 1 },
      ...options,
    })
    return vertices
  }
}

// Showcase performance regression, covering full-world views on both canvases.
test('moving world views submit at most 8,000 path vertices', () => {
  const draw = countingGlobe()
  for (const [yaw, pitch] of [[-105, -23], [-12, -44], [90, -20], [-180, -60]]) {
    const vertices = draw({ camera: { yaw, pitch, zoom: 1 }, moving: true })
    assert.ok(vertices <= 8000, `view ${yaw}, ${pitch} submitted ${vertices} vertices`)
  }
  const bridgeVertices = draw({
    width: 1978, height: 1200, pixelRatio: 2, x: 989, y: 900, radius: 900,
    camera: { yaw: -28, pitch: -10, zoom: 1 }, detail: .5, moving: true,
  })
  assert.ok(bridgeVertices <= 8000, `wide bridge submitted ${bridgeVertices} vertices`)
})

test('a settled globe redraw uses full-detail geography after a moving frame', () => {
  const draw = countingGlobe()
  const moving = draw({ moving: true })
  const settled = draw()
  assert.ok(settled >= 50_000 && settled > moving * 5,
    `settled frame submitted ${settled} vertices after a ${moving}-vertex moving frame`)
})
