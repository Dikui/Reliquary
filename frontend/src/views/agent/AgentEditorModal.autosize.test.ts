import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

for (const locale of ['zh-CN', 'en-US']) {
  test(`${locale}: agent editor opens, changes mode and sections, and closes without rejected autosize work`, () => {
    const result = spawnSync(process.execPath, ['--import', 'tsx', new URL('../../components/__tests__/textareaMountProbe.mjs', import.meta.url).pathname, 'agent', locale], { encoding: 'utf8' })
    assert.equal(result.status, 0, result.stdout + result.stderr)
    const summary = JSON.parse(result.stdout.trim().split('\n').at(-1)!)
    assert.equal(summary.invalidMeasurements, 0)
    assert.equal(summary.resizeObserversAfterUnmount, 0)
  })
}
