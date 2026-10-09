import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN.ts'
import enUS from './locales/en-US.ts'
import { BUILT_IN_DEFAULT, SUPPORTED_LOCALES, resolveDefaultLocale } from './resolveDefaultLocale.ts'
import { syncDocumentLanguage } from './syncDocumentLanguage.ts'

const messages = {
  'zh-CN': zhCN,
  'en-US': enUS,
}

// User's explicit past choice wins; otherwise use the deployment default.
const userLocale = localStorage.getItem('locale') || ''
const savedLocale = (SUPPORTED_LOCALES as readonly string[]).includes(userLocale)
  ? userLocale
  : resolveDefaultLocale(
      window.__RUNTIME_CONFIG__?.DEFAULT_LOCALE,
      import.meta.env.VITE_DEFAULT_LOCALE,
    )

const i18n = createI18n({
  legacy: false,
  locale: savedLocale,
  fallbackLocale: BUILT_IN_DEFAULT,
  globalInjection: true,
  // Some translations intentionally embed `<strong>` markup (e.g. agent step summaries).
  // We render them via v-html with our own sanitization, so silence vue-i18n's HTML warning
  // to avoid flooding the console and slowing renders during history loads.
  warnHtmlMessage: false,
  messages
})

syncDocumentLanguage(i18n.global.locale, document)

export default i18n
