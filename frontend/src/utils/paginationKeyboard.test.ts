import assert from 'node:assert/strict'
import test from 'node:test'
import { activatePaginationItem, syncPaginationItems } from './paginationKeyboard'

function item(...classes: string[]) {
  const attrs: Record<string, string> = {}
  let clicks = 0
  const node = {
    attrs, get clicks() { return clicks },
    classList: { contains: (name: string) => classes.includes(name) },
    setAttribute: (name: string, value: string) => { attrs[name] = value },
    removeAttribute: (name: string) => { delete attrs[name] },
    closest: () => node,
    click: () => { clicks++ },
  }
  return node
}

test('pagination items become focusable buttons that follow disabled and current state', () => {
  const prev = item('t-pagination__btn', 't-pagination__btn-prev', 't-is-disabled')
  const current = item('t-pagination__number', 't-is-current')
  const next = item('t-pagination__btn', 't-pagination__btn-next')
  const more = item('t-pagination__number', 't-pagination__number--more')
  const jump = item()
  const root = { querySelectorAll: (selector: string) => selector.includes('jump') ? [jump] : [prev, current, next, more] } as unknown as ParentNode
  syncPaginationItems(root, key => ({ previous: 'Previous', next: 'Next', more: 'More pages', jump: 'Go to page' })[key])
  assert.deepEqual(prev.attrs, { role: 'button', tabindex: '-1', 'aria-disabled': 'true', 'aria-label': 'Previous' })
  assert.deepEqual(current.attrs, { role: 'button', tabindex: '0', 'aria-disabled': 'false', 'aria-current': 'page' })
  assert.deepEqual(next.attrs, { role: 'button', tabindex: '0', 'aria-disabled': 'false', 'aria-label': 'Next' })
  assert.equal(more.attrs['aria-label'], 'More pages')
  assert.deepEqual(jump.attrs, { 'aria-label': 'Go to page' })
})

test('Enter and Space click an enabled item; disabled items and other keys do nothing', () => {
  const press = (key: string, target: ReturnType<typeof item>) => {
    let prevented = false
    activatePaginationItem({ key, target, preventDefault: () => { prevented = true } } as unknown as KeyboardEvent)
    return prevented
  }
  const page = item('t-pagination__number'), disabled = item('t-pagination__btn', 't-is-disabled')
  assert.equal(press('Enter', page), true)
  assert.equal(press(' ', page), true)
  assert.equal(press('a', page), false)
  assert.equal(press('Enter', disabled), false)
  assert.equal(page.clicks, 2)
  assert.equal(disabled.clicks, 0)
})
