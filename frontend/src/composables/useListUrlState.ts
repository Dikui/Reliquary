// Team lists preserve their view and search query in the URL.
import { ref, watch, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

export type ListScope = 'all' | 'favorites' | 'recents'
export interface ListUrlState { scope: Ref<string>; query: Ref<string> }
export interface ListUrlStateOptions { defaultScope: ListScope }

export function useListUrlState(opts: ListUrlStateOptions): ListUrlState {
  const route = useRoute()
  const router = useRouter()
  const parseScope = (value: unknown): ListScope => value === 'favorites' || value === 'recents' ? value : opts.defaultScope
  const scope = ref<string>(parseScope(route.query.scope))
  const query = ref(typeof route.query.q === 'string' ? route.query.q : '')
  const sync = () => {
    const next: Record<string, string> = { ...(route.query as Record<string, string>) }
    if (scope.value !== opts.defaultScope) next.scope = scope.value
    else delete next.scope
    delete next.creator
    if (query.value) next.q = query.value
    else delete next.q
    if (next.scope === route.query.scope && next.q === route.query.q && route.query.creator === undefined) return
    void router.replace({ path: route.path, query: next })
  }
  watch([scope, query], sync)
  watch(() => route.query, (next) => {
    scope.value = parseScope(next.scope)
    query.value = typeof next.q === 'string' ? next.q : ''
  })
  return { scope, query }
}
