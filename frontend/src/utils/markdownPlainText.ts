import { Lexer, type Token, type Tokens } from 'marked'

const namedEntities: Record<string, string> = {
  amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: ' ',
}

function decodeEntities(text: string): string {
  return text.replace(/&(#x[\da-f]+|#\d+|amp|lt|gt|quot|apos|nbsp);/gi, (entity, name: string) => {
    if (!name.startsWith('#')) return namedEntities[name.toLowerCase()] || entity
    const point = name[1]?.toLowerCase() === 'x' ? Number.parseInt(name.slice(2), 16) : Number.parseInt(name.slice(1), 10)
    return point > 0 && point <= 0x10ffff && !(point >= 0xd800 && point <= 0xdfff) ? String.fromCodePoint(point) : '\ufffd'
  })
}

function htmlText(text: string, imageLabel: string): string {
  return decodeEntities(text.replace(/<!--[\s\S]*?-->|<\/?[a-z][^>"']*(?:(?:"[^"]*"|'[^']*')[^>"']*)*>/gi, tag => {
    if (/^<img\b/i.test(tag)) return imageLabel
    if (/^<\/?(?:br|p|div|li|h[1-6]|blockquote|pre|tr)\b/i.test(tag)) return '\n'
    return ''
  }))
}

/** Convert a Markdown excerpt to text before escaping or highlighting it. */
export function markdownToPlainText(markdown: string | null | undefined, imageLabel = '[Image]'): string {
  if (!markdown) return ''
  // A local lexer keeps document rendering extensions out of excerpt conversion.
  const lexer = new Lexer({ gfm: true })
  const render = (tokens: Token[]): string => tokens.map(token => {
    switch (token.type) {
      case 'image': return imageLabel
      case 'html': return htmlText((token as Tokens.HTML).text, imageLabel)
      case 'code': return `${(token as Tokens.Code).text}\n`
      case 'codespan': return (token as Tokens.Codespan).text
      case 'escape': return decodeEntities((token as Tokens.Escape).text)
      case 'list': return '\n' + (token as Tokens.List).items.map(item => render(item.tokens).trim()).join('\n') + '\n'
      case 'table': {
        const table = token as Tokens.Table
        return [table.header, ...table.rows].map(row => row.map(cell => render(cell.tokens)).join('\t')).join('\n') + '\n'
      }
      case 'def': case 'checkbox': return ''
      case 'space': case 'hr': case 'br': return '\n'
      case 'heading': return render((token as Tokens.Heading).tokens) + '\n\n'
      case 'paragraph': case 'blockquote': return render((token as Tokens.Paragraph).tokens) + '\n'
      default: return 'tokens' in token && Array.isArray(token.tokens)
        ? render(token.tokens)
        : decodeEntities((token as Tokens.Text).text || '')
    }
  }).join('')
  return render(lexer.lex(markdown)).replace(/[ \t]+\n/g, '\n').replace(/\n{3,}/g, '\n\n').trim()
}
