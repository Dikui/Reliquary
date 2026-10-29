import { marked, type Tokens } from 'marked'
import { bodyParagraphs } from './bodyReference'
import type { SourceLocation } from '@/types/search'

// Decorate the existing rendered DOM; never split or re-render Markdown blocks.
// Source offsets retain the backend's numbering, including headings and lists.
export function numberReadingParagraphs(root: HTMLElement, body: string) {
  const paragraphs = bodyParagraphs(body)
  const frontmatter = body.match(/^\s*---\r?\n[\s\S]*?\r?\n---\r?\n/)?.[0].length || 0
  const tokens = marked.lexer(body.slice(frontmatter))
  let offset = frontmatter, cursor = 0
  const children = Array.from(root.children) as HTMLElement[]
  const tagged = new Set<number>()
  const tag = (element: HTMLElement, start: number, end: number) => {
    const index = paragraphs.findIndex(block => block.start <= start && start < block.end)
    if (index < 0 || tagged.has(index + 1)) return
    tagged.add(index + 1)
    element.setAttribute('data-paragraph', String(index + 1))
    element.setAttribute('data-source-start', String(start))
    element.setAttribute('data-source-end', String(end))
  }
  for (const token of tokens) {
    const start = offset; offset += token.raw.length
    if (token.type === 'space' || token.type === 'def') continue
    const matches = (element: HTMLElement) => {
      if (token.type === 'heading') return /^H[1-6]$/.test(element.tagName)
      if (token.type === 'list') return ['OL', 'UL'].includes(element.tagName)
      if (token.type === 'blockquote') return element.tagName === 'BLOCKQUOTE'
      if (token.type === 'table') return element.tagName === 'TABLE'
      if (token.type === 'hr') return element.tagName === 'HR'
      if (token.type === 'code') return element.matches('.code-block-wrapper, .mermaid, pre')
      if (token.type === 'html') {
        const tagName = token.raw.match(/^\s*<([a-z][\w-]*)/i)?.[1]
        return Boolean(tagName && element.tagName === tagName.toUpperCase())
      }
      return Boolean(element.textContent?.trim()) || element.tagName === 'FIGURE' || (element.tagName === 'P' && Boolean(element.querySelector('img')))
    }
    const next = children.findIndex((element, i) => i >= cursor && matches(element))
    if (next < 0) continue
    const element = children[next]!
    cursor = next + 1
    if (token.type === 'list' || token.type === 'blockquote') {
      const blocks = paragraphs.filter(block => block.start >= start && block.start < offset)
      const nodes = Array.from(element.querySelectorAll<HTMLElement>('p, li, pre, h1, h2, h3, h4, h5, h6, table, .code-block-wrapper, .mermaid'))
        .filter(node => !(node.tagName === 'LI' && Array.from(node.children).some(child => child.tagName === 'P')))
        .filter(node => !(node.tagName === 'PRE' && node.parentElement?.classList.contains('code-block-wrapper')))
      if (token.type === 'blockquote' && blocks.length <= 1) tag(element, start, offset)
      else blocks.forEach((block, index) => { tag(nodes[index] || element, block.start, block.end) })
    } else {
      tag(element, start, offset)
      if (token.type === 'paragraph') {
        let images = (token as Tokens.Paragraph).tokens.filter(item => item.type === 'image').length
        if (element.tagName === 'FIGURE') images--
        while (images > 0 && cursor < children.length) {
          const part = children[cursor]!
          if (part.tagName === 'FIGURE') {
            part.setAttribute('data-paragraph-part', element.getAttribute('data-paragraph') || '')
            images--; cursor++
          } else if (part.tagName === 'P' && !part.textContent?.trim()) cursor++
          else break
        }
      }
    }
  }
}

export function focusReadingParagraph(root: HTMLElement, reference?: SourceLocation) {
  root.querySelectorAll('[data-reference]').forEach(node => node.removeAttribute('data-reference'))
  if (!reference) return
  const number = Math.max(1, reference.paragraph)
  const target = root.querySelector<HTMLElement>(`[data-paragraph="${number}"]`)
  root.querySelectorAll(`[data-paragraph="${number}"], [data-paragraph-part="${number}"]`).forEach(node => node.setAttribute('data-reference', 'true'))
  target?.scrollIntoView({ block: 'center', behavior: window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
}

export function bindReadingImages(root: HTMLElement, preview: (image: { src: string; alt: string }) => void, label: string) {
  root.querySelectorAll<HTMLImageElement>('img').forEach(image => {
    if (image.parentElement?.classList.contains('reading-image')) return
    const button = document.createElement('button')
    button.type = 'button'; button.className = 'reading-image'
    button.setAttribute('aria-label', image.alt ? `${label}: ${image.alt}` : label)
    image.replaceWith(button); button.appendChild(image)
    button.addEventListener('click', () => preview({ src: image.currentSrc || image.src, alt: image.alt }))
  })
}
