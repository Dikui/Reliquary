import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

function captureUpload() {
  const requests: { url: string; form: FormData; progress: unknown }[] = []
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('./knowledge-base.ts', import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    assert.equal(name, '../utils/request')
    return { postUpload: (url: string, form: FormData, progress: unknown) => {
      requests.push({ url, form, progress })
      return Promise.resolve({ success: true })
    } }
  }, module, module.exports)
  return { upload: module.exports.uploadEntryFile, requests }
}

for (const serialized of [false, true]) {
  test(`file upload preserves JSON parameters supplied as ${serialized ? 'strings' : 'objects'}`, async () => {
    const { upload, requests } = captureUpload()
    const source = { author: '上传作者', publisher: 'Journal', original_url: 'https://example.com/article', published_at: '2026-10-11' }
    const config = { chunking_config: { chunk_size: 512, chunk_overlap: 32 } }
    const sourceJSON = JSON.stringify(source, null, 2)
    const configJSON = JSON.stringify(config, null, 2)
    const file = new File(['# Original Markdown'], 'article.md', { type: 'text/markdown' })
    const progress = () => {}
    await upload('kb', { file, fileName: 'folder/article.md', source_info: serialized ? sourceJSON : source,
      process_config: serialized ? configJSON : config, channel: undefined }, progress)
    assert.equal(requests.length, 1)
    const { url, form, progress: forwarded } = requests[0]!
    assert.equal(url, '/api/v1/knowledge-bases/kb/entries/file')
    assert.equal(forwarded, progress)
    for (const [key, expected, text] of [['source_info', source, sourceJSON], ['process_config', config, configJSON]] as const) {
      const value = form.get(key)
      assert.equal(typeof value, 'string')
      assert.deepEqual(JSON.parse(value as string), expected)
      if (serialized) assert.equal(value, text)
    }
    assert.equal(form.get('fileName'), 'folder/article.md')
    assert.equal(form.has('channel'), false)
    const attached = form.get('file') as File
    assert.equal(attached.name, file.name)
    assert.equal(await attached.text(), await file.text())
  })
}
