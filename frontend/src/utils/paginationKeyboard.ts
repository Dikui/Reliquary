// TDesign 1.19 Pagination renders pages and prev/next as li/div without tabindex or role,
// so keyboard users cannot reach them, and its jump input has no name. Patch every rendered pagination in place.
const ITEMS = '.t-pagination__btn, .t-pagination__number'
type Label = (key: 'previous' | 'next' | 'more' | 'jump') => string

export function syncPaginationItems(root: ParentNode, label: Label) {
  root.querySelectorAll<HTMLElement>(ITEMS).forEach((item) => {
    const disabled = item.classList.contains('t-is-disabled')
    item.setAttribute('role', 'button')
    item.setAttribute('tabindex', disabled ? '-1' : '0')
    item.setAttribute('aria-disabled', String(disabled))
    if (item.classList.contains('t-is-current')) item.setAttribute('aria-current', 'page')
    else item.removeAttribute('aria-current')
    const key = item.classList.contains('t-pagination__btn-prev') ? 'previous' : item.classList.contains('t-pagination__btn-next') ? 'next' : item.classList.contains('t-pagination__number--more') ? 'more' : ''
    if (key) item.setAttribute('aria-label', label(key))
  })
  root.querySelectorAll<HTMLElement>('.t-pagination__jump input').forEach(input => input.setAttribute('aria-label', label('jump')))
}

export function activatePaginationItem(event: KeyboardEvent) {
  if (event.key !== 'Enter' && event.key !== ' ') return
  const item = (event.target as HTMLElement | null)?.closest?.<HTMLElement>(ITEMS)
  if (!item || item.classList.contains('t-is-disabled')) return
  event.preventDefault()
  item.click()
}

let installed = false

export function installPaginationKeyboard(label: Label) {
  if (installed || typeof document === 'undefined') return
  installed = true
  syncPaginationItems(document, label)
  new MutationObserver((mutations) => {
    if (mutations.some(m => m.type === 'childList' ? m.addedNodes.length > 0 : (m.target as Element).closest?.('.t-pagination'))) {
      syncPaginationItems(document, label)
    }
  }).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['class'] })
  document.addEventListener('keydown', activatePaginationItem)
}
