import assert from 'node:assert/strict'
import test from 'node:test'
import { marked } from 'marked'
import { TestElement } from '../components/ui/__tests__/dom'
import { focusReadingParagraph, numberReadingParagraphs } from './readingParagraphs'
import type { SourceLocation } from '@/types/search'

test('rendered headings, list items and diagrams retain saved-body paragraph numbers', () => {
  const root = new TestElement('div')
  for (const [tag, text] of [['h1', 'Title'], ['p', 'Opening 😀'], ['ul', ''], ['pre', 'graph TD; A-->B'], ['p', 'After the diagram']]) {
    const node = new TestElement(tag!)
    node.textContent = text!
    root.appendChild(node)
  }
  const list = root.children[2]!
  for (const text of ['One', 'Two']) { const item = new TestElement('li'); item.textContent = text; list.appendChild(item) }
  const body = '# Title\n\nOpening 😀\n\n- One\n- Two\n\n```mermaid\ngraph TD; A-->B\n```\n\nAfter the diagram'
  numberReadingParagraphs(root as unknown as HTMLElement, body)
  assert.deepEqual(root.querySelectorAll('[data-paragraph]').map(node => node.getAttribute('data-paragraph')), ['1', '2', '3', '4', '5', '6'])
  let scrolled = 0
  root.children[4]!.scrollIntoView = () => { scrolled++ }
  focusReadingParagraph(root as unknown as HTMLElement, { paragraph: 6 } as SourceLocation)
  assert.equal(root.children[4]!.getAttribute('data-reference'), 'true')
  assert.equal(scrolled, 1)
  focusReadingParagraph(root as unknown as HTMLElement, { paragraph: 2 } as SourceLocation)
  assert.equal(root.querySelectorAll('[data-reference]').length, 1)
  assert.equal(root.children[1]!.getAttribute('data-reference'), 'true')
  assert.equal(root.children[3]!.textContent, 'graph TD; A-->B', 'decoration does not replace rendered content')
})

test('a missing paragraph clears the previous highlight without selecting unrelated content', () => {
  const root = new TestElement('div'), paragraph = new TestElement('p')
  paragraph.textContent = 'Saved body'; root.appendChild(paragraph)
  numberReadingParagraphs(root as unknown as HTMLElement, 'Saved body')
  focusReadingParagraph(root as unknown as HTMLElement, { paragraph: 1 } as SourceLocation)
  focusReadingParagraph(root as unknown as HTMLElement, { paragraph: 99 } as SourceLocation)
  assert.equal(root.querySelectorAll('[data-reference]').length, 0)
})

test('a paragraph with an image does not shift numbering on the next rendered paragraph', () => {
  const root = new TestElement('div')
  // The existing renderer emits a figure inside a Markdown paragraph. HTML
  // parsing separates the figure and leaves an empty closing paragraph.
  for (const [tag, text] of [['p', 'Before image'], ['figure', 'Image caption'], ['p', ''], ['p', 'After image']]) {
    const node = new TestElement(tag!); node.textContent = text!; root.appendChild(node)
  }
  numberReadingParagraphs(root as unknown as HTMLElement, 'Before image ![Image caption](image.png)\n\nAfter image')
  assert.equal(root.children[3]!.getAttribute('data-paragraph'), '2')
  assert.equal(root.children[1]!.getAttribute('data-paragraph'), null)
})

test('separate quote blocks remain addressable using saved-body numbers', () => {
  const root = new TestElement('div')
  const body = '> First quote\n\n> Second quote'
  assert.equal((String(marked.parse(body)).match(/<blockquote>/g) || []).length, 2)
  for (const text of ['First quote', 'Second quote']) {
    const quote = new TestElement('blockquote'), p = new TestElement('p')
    p.textContent = text; quote.appendChild(p); root.appendChild(quote)
  }
  numberReadingParagraphs(root as unknown as HTMLElement, body)
  assert.deepEqual(root.querySelectorAll('[data-paragraph]').map(node => node.getAttribute('data-paragraph')), ['1', '2'])
  focusReadingParagraph(root as unknown as HTMLElement, { paragraph: 2 } as SourceLocation)
  assert.equal(root.children[1]!.getAttribute('data-reference'), 'true')
})
