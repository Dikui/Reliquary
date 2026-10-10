import { defineComponent, h, resolveComponent } from 'vue'

// Keep existing tests' TDesign stubs and event handlers when the owning form
// imports the shared wrapper. Its real lifecycle has separate mounting tests.
export const safeTextareaTestModule = {
  __esModule: true,
  default: defineComponent({
    inheritAttrs: false,
    setup: (_props, { attrs, slots }) => () => h(resolveComponent('t-textarea'), attrs, slots),
  }),
}
