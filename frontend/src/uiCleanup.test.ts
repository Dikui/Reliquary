import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'

// These two components are registered by app.use(router) in main.ts.
const globalComponents = new Set(['RouterLink', 'RouterView', 'router-link', 'router-view'])

function unresolvedComponents(source: string, filename: string): string[] {
  const { descriptor, errors } = parse(source, { filename })
  assert.deepEqual(errors, [], filename)
  if (!descriptor.template) return []
  const bindings = descriptor.script || descriptor.scriptSetup
    ? compileScript(descriptor, { id: filename }).bindings : {}
  const compiled = compileTemplate({
    source: descriptor.template.content,
    filename,
    id: filename,
    compilerOptions: { bindingMetadata: bindings, expressionPlugins: ['typescript'] },
  })
  assert.deepEqual(compiled.errors, [], filename)
  // Imported bindings and Vue builtins compile directly; runtime resolutions must
  // be registered globals or an explicit recursive reference to the SFC itself.
  return [...compiled.code.matchAll(/_resolveComponent\("([^"]+)"(?:,\s*(true))?\)/g)]
    .filter(([, name, recursive]) => !recursive && !globalComponents.has(name!))
    .map(([, name]) => name!)
}

function sources(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return entry.name === '__tests__' ? [] : sources(path)
    return /\.(vue|ts|css|less)$/.test(path) && !entry.name.endsWith('.test.ts') ? [path] : []
  })
}

test('application sources and dependencies contain no legacy component library references', () => {
  for (const file of [...sources(new URL('.', import.meta.url).pathname), 'index.html', 'package.json', 'package-lock.json']) {
    assert.doesNotMatch(readFileSync(file, 'utf8'), /tdesign|--td-|<\/?t-[a-z]/i, file)
  }
})

test('the document baseline uses the shared tokens and always honors hidden elements', () => {
  const css = readFileSync(new URL('./styles/global.css', import.meta.url), 'utf8')
  assert.match(css, /font-size: var\(--text-sm\)/)
  assert.match(css, /font-family: var\(--font\)/)
  assert.match(css, /background: var\(--bg\)/)
  assert.match(css, /\[hidden\]\s*\{\s*display:\s*none !important/)
})

test('all application templates resolve their component references', () => {
  const root = new URL('.', import.meta.url).pathname
  const failures = sources(root).filter(file => file.endsWith('.vue')).flatMap(file =>
    unresolvedComponents(readFileSync(file, 'utf8'), file).map(name => `${relative(root, file)}: ${name}`))
  assert.deepEqual(failures, [], `Unregistered template components:\n${failures.join('\n')}`)
})

test('component audit rejects missing icons and accepts imports, builtins and router globals', () => {
  assert.deepEqual(unresolvedComponents('<template><button><SettingIcon /></button><missing-widget /></template>', 'Fixture.vue'), ['SettingIcon', 'missing-widget'])
  assert.deepEqual(unresolvedComponents(`
    <script setup lang="ts">
    import SettingIcon from './SettingIcon.vue'
    import { Widget as LocalWidget } from './widgets'
    </script>
    <template><SettingIcon /><local-widget /><Transition><RouterView /></Transition><router-link to="/" /><Fixture /></template>
  `, 'Fixture.vue'), [])
})
