import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import { parse as parseTemplate, NodeTypes, type ElementNode, type RootNode, type TemplateChildNode } from '@vue/compiler-dom'

const filename = new URL('./ReparseConfirmDialog.vue', import.meta.url)
const { descriptor, errors } = parse(readFileSync(filename, 'utf8'), { filename: filename.pathname })
assert.deepEqual(errors, [])

async function layoutAt(width: number) {
  const style = descriptor.styles.find(block => block.scoped)!
  const compiled = await compileStyleAsync({
    filename: filename.pathname, id: 'data-v-reparse-layout', source: style.content,
    scoped: true, preprocessLang: 'less',
  })
  assert.deepEqual(compiled.errors, [])
  const declarations: Record<string, Record<string, string>> = {}
  compiled.rawResult!.root.walkRules(rule => {
    if (rule.parent?.type === 'atrule') {
      const media = 'params' in rule.parent && rule.parent.params.match(/^\(max-width:\s*(\d+)px\)$/)
      assert.ok(media, `Unexpected layout media condition: ${rule.parent.toString()}`)
      if (width > Number(media[1])) return
    }
    for (const selector of rule.selectors) {
      const name = selector.replace(/\[data-v-reparse-layout\]/g, '')
      const values = declarations[name] ||= {}
      rule.walkDecls(declaration => { values[declaration.prop] = declaration.value })
    }
  })
  return declarations
}

for (const width of [1440, 720]) {
  test(`reparse dialog allocates a bounded content scroller and preserves footer space at ${width}px`, async () => {
    const css = await layoutAt(width)
    const modal = css['.reparse-confirm-modal']
    assert.equal(modal.display, 'flex')
    assert.equal(modal['flex-direction'], 'column')
    assert.equal(modal.height, '85vh')
    assert.equal(modal['max-height'], '750px')
    assert.equal(modal.overflow, 'hidden')

    const container = css['.reparse-confirm-container']
    assert.equal(container.display, 'flex')
    assert.equal(container.flex, '1')
    assert.equal(container['min-height'], '0')
    assert.equal(container.overflow, 'hidden')
    if (width <= 800) assert.equal(container['flex-direction'], 'column')

    const panel = css['.config-panel']
    assert.ok(panel, 'The configuration panel must allocate height to its content and footer')
    assert.equal(panel.display, 'flex')
    assert.equal(panel['flex-direction'], 'column')
    assert.equal(panel.flex, '1')
    assert.equal(panel['min-width'], '0')
    assert.equal(panel['min-height'], '0', 'The panel must shrink within the mobile vertical flex chain')
    assert.equal(panel.overflow, 'hidden')

    const content = css['.content-wrapper']
    assert.equal(content.flex, '1')
    assert.equal(content['min-height'], '0', 'Long settings must shrink into their assigned scroll viewport')
    assert.equal(content['overflow-y'], 'auto')
    const footer = css['.modal-footer']
    assert.equal(footer['flex-shrink'], '0', 'Long settings must not reduce the action buttons to zero height')
    assert.equal(footer.position, undefined, 'The footer must reserve normal-flow space below the scroller')
  })
}

test('parser and chunking settings share the scroller while actions stay outside it', () => {
  const elements: ElementNode[] = []
  const parents = new Map<ElementNode, ElementNode | undefined>()
  function visit(node: RootNode | TemplateChildNode, parent?: ElementNode) {
    if (node.type === NodeTypes.ELEMENT) {
      elements.push(node)
      parents.set(node, parent)
      node.children.forEach(child => visit(child, node))
    } else if (node.type === NodeTypes.ROOT) node.children.forEach(child => visit(child))
  }
  visit(parseTemplate(descriptor.template!.content))
  const hasClass = (node: ElementNode, name: string) => node.props.some(prop =>
    prop.type === NodeTypes.ATTRIBUTE && prop.name === 'class' && prop.value?.content.split(/\s+/).includes(name))
  const panel = elements.find(node => hasClass(node, 'config-panel'))!
  const content = elements.find(node => hasClass(node, 'content-wrapper'))!
  const footer = elements.find(node => hasClass(node, 'modal-footer'))!
  assert.ok(panel && content && footer)
  assert.equal(parents.get(content), panel)
  assert.equal(parents.get(footer), panel, 'Footer buttons must not be inside the scrolling settings')
  assert.equal(footer.tag, 'footer')
  const children = panel.children.filter((node): node is ElementNode => node.type === NodeTypes.ELEMENT)
  assert.deepEqual(children, [content, footer])
  const sections = elements.filter(node => node.props.some(prop => prop.type === NodeTypes.ATTRIBUTE && prop.name === 'data-section'))
  assert.equal(sections.length, 2)
  for (const section of sections) assert.equal(parents.get(section), content)
  const actions = footer.children.filter((node): node is ElementNode => node.type === NodeTypes.ELEMENT)
  assert.deepEqual(actions.map(node => node.tag), ['t-button', 't-button'])
})
