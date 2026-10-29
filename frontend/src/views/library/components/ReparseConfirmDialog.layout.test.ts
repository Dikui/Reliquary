import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../../../utils/sfcStyleTestHelpers'

for (const width of [1440, 720, 390]) test(`reparse settings fit the ${width}px viewport inside the shared dialog`, async () => {
  const rules = await compiledSFCStyleRules(new URL('./ReparseConfirmDialog.vue', import.meta.url))
  assert.equal(styleDeclarationsAtWidth(rules, '.reparse-dialog', width).width, 'min(880px, calc(100vw - 32px))')
  const shared = readFileSync(new URL('../../../components/ui/ui.css', import.meta.url), 'utf8')
  assert.match(shared, /\.rq-dialog__body[^}]*overflow:\s*auto/)
  assert.match(shared, /\.rq-dialog__footer[^}]*flex-wrap:\s*wrap/)
})

test('reparse actions occupy the shared footer outside the scrolling settings', () => {
  const source = readFileSync(new URL('./ReparseConfirmDialog.vue', import.meta.url), 'utf8')
  assert.match(source, /<Dialog[^>]*:title="dialogTitle"/)
  assert.match(source, /<template #footer>[\s\S]*@click="handleCancel"[\s\S]*@click="handleConfirm"/)
  assert.match(source, /v-model:config="uiState.chunkingConfig"/)
})
