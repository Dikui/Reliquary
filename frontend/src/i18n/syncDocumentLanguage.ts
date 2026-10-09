import { watch, type Ref } from 'vue'

export function syncDocumentLanguage(locale: Ref<string>, target: Pick<Document, 'documentElement'>) {
  return watch(locale, value => {
    target.documentElement.lang = value
  }, { immediate: true, flush: 'sync' })
}
