export type TextareaAutosize = boolean | { minRows?: number; maxRows?: number }

const sizingProperties = [
  'padding-top', 'padding-bottom', 'padding-left', 'padding-right',
  'font-family', 'font-weight', 'font-size', 'font-variant', 'text-rendering',
  'text-transform', 'width', 'text-indent', 'border-width', 'box-sizing',
  'line-height', 'letter-spacing',
]

// Keep TDesign's row and box-sizing calculation, but give each measurement
// its own temporary element so closing a form cannot leave shared DOM behind.
export function calculateTextareaHeight(textarea: HTMLTextAreaElement, autosize: Exclude<TextareaAutosize, false>) {
  const minRows = typeof autosize === 'object' ? autosize.minRows ?? 1 : 1
  const maxRows = typeof autosize === 'object' ? autosize.maxRows : undefined
  const style = window.getComputedStyle(textarea)
  const padding = parseFloat(style.getPropertyValue('padding-top')) + parseFloat(style.getPropertyValue('padding-bottom'))
  const border = parseFloat(style.getPropertyValue('border-top-width')) + parseFloat(style.getPropertyValue('border-bottom-width'))
  const boxSizing = style.getPropertyValue('box-sizing')
  const measurement = document.createElement('textarea')
  measurement.setAttribute('aria-hidden', 'true')
  measurement.tabIndex = -1
  measurement.setAttribute('style', [
    ...sizingProperties.map(property => `${property}:${style.getPropertyValue(property)}`),
    'min-height:0!important', 'max-height:none!important', 'height:0!important',
    'visibility:hidden!important', 'overflow:hidden!important', 'position:absolute!important',
    'z-index:-1000!important', 'top:0!important', 'right:0!important',
  ].join(';'))
  document.body.appendChild(measurement)
  try {
    measurement.value = textarea.value || textarea.placeholder || ''
    let height = measurement.scrollHeight
    if (boxSizing === 'border-box') height += border
    else if (boxSizing === 'content-box') height -= padding
    measurement.value = ''
    const rowHeight = measurement.scrollHeight - padding
    const forRows = (rows: number) => rows * rowHeight + (boxSizing === 'border-box' ? padding + border : 0)
    const minHeight = forRows(minRows)
    height = Math.max(minHeight, height)
    if (maxRows != null) height = Math.min(forRows(maxRows), height)
    return { minHeight: `${minHeight}px`, height: `${height}px` }
  } finally {
    measurement.parentNode?.removeChild(measurement)
  }
}
