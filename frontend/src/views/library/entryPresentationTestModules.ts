import { loadComponent } from './componentTestHarness'
import { computed, defineComponent, h, ref } from 'vue'
import { loadUiComponent } from '../../components/ui/uiTestLoader'
import * as processing from '../../utils/entryProcessingStatus'
import * as reading from '../../utils/readingParagraphs'
import * as feedback from '../../components/ui/feedback'

/** Production presentation components for SFC unit loaders; no browser is used. */
export const entryPresentationTestModules: Record<string, unknown> = {
  '@/components/ui': { ...Object.fromEntries(['Button', 'Input', 'Textarea', 'Checkbox', 'Switch', 'Field', 'Select', 'Chip', 'LibMark', 'Skeleton', 'EmptyState', 'ErrorState', 'DataTable', 'Tag', 'StatusSquare'].map(name => [name, loadUiComponent(name)])),
    Dialog: defineComponent({ props: ['modelValue', 'title'], setup: (props, { slots }) => () => props.modelValue ? h('section', { role: 'dialog', 'aria-label': props.title }, [slots.default?.(), slots.footer?.()]) : null }),
  },
  '@/utils/readingParagraphs': reading,
  '@/utils/entryProcessingStatus': processing,
  '@/components/ui/feedback': feedback,
}

/** Exercise the detail presentation with a controlled saved-record boundary. */
export function detailPresentation(modules: Record<string, unknown>) {
  return defineComponent({
    props: ['details'],
    setup(props) {
      const blank = () => ref('')
      const record = computed(() => props.details)
      const state = {
        entry: record, library: ref(), body: computed(() => props.details.md?.map((chunk: any) => chunk.content).join('') || ''),
        bodyHash: ref(''), bodyReady: ref(true), loading: ref(false), error: blank(), bodyError: blank(),
        trace: ref(), traceError: blank(), entities: ref([]), relations: ref([]), relationsLoading: ref(false), relationsError: blank(),
        refresh() {}, load() {}, loadBody() {}, loadRelations() {}, loadTrace() {},
      }
      const View = loadComponent(new URL('./EntryDetail.vue', import.meta.url), {
        ...entryPresentationTestModules, ...modules,
        '@/api/entities': {}, '@/api/relations': {}, '@/api/entry-judgments': {},
        '@/components/DocContent.vue': { __esModule: true, default: loadComponent(new URL('../../components/DocContent.vue', import.meta.url), { ...entryPresentationTestModules, ...modules, 'katex/dist/katex.min.css': {}, 'highlight.js/styles/github.css': {} }) },
        './useEntryDetail': { useEntryDetail: () => state },
        '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
        './components/RepostSources.vue': { __esModule: true, default: loadComponent(new URL('./components/RepostSources.vue', import.meta.url), {}) },
        'vue-router': {
          useRoute: () => ({ params: { id: props.details.id }, query: {} }),
          useRouter: () => ({ push() {}, replace() {} }), onBeforeRouteLeave() {}, onBeforeRouteUpdate() {},
          RouterLink: defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { href: typeof props.to === 'string' ? props.to : props.to.path }, slots.default?.()) }),
        },
        '@/components/ui': {
          ...entryPresentationTestModules['@/components/ui'] as object, ...feedback,
          Tabs: defineComponent({ props: ['modelValue'], setup: (props, { slots }) => () => h('div', slots[props.modelValue]?.()) }),
        },
      })
      View.components = { RouterLink: defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { href: typeof props.to === 'string' ? props.to : props.to.path }, slots.default?.()) }) }
      return () => h(View)
    },
  })
}
