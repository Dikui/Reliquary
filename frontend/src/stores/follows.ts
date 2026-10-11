import { ref } from 'vue'
import { defineStore } from 'pinia'
import { followTarget, getFollowState, listFollows, markFollowsViewed, unfollowTarget } from '@/api/follows'
import type { FollowState, FollowTarget, FollowTargetType } from '@/types/follows'
import { compareRFC3339 } from '@/utils/rfc3339'

export const useFollowStore = defineStore('follows', () => {
  const targets = ref<FollowTarget[]>([]), targetsLoaded = ref(false), targetsLoading = ref(false), targetsError = ref(false)
  const newCount = ref(0), lastViewedAt = ref<string | null>(null), stateError = ref(false)
  const pending = ref<Record<string, boolean>>({})
  let targetsRequest: Promise<void> | undefined, stateSequence = 0, mutationEpoch = 0
  const key = (type: FollowTargetType, id: string) => `${type}:${id}`
  const isFollowing = (type: FollowTargetType, id: string) => targets.value.some(target => target.target_type === type && target.target_id === id)
  const isPending = (type: FollowTargetType, id: string) => Boolean(pending.value[key(type, id)])
  function applyState(state: FollowState) {
    if (lastViewedAt.value && (!state.last_viewed_at || compareRFC3339(state.last_viewed_at, lastViewedAt.value) === -1)) return
    newCount.value = state.new_count; lastViewedAt.value = state.last_viewed_at; stateError.value = false
  }
  async function loadTargets(force = false) {
    if (targetsRequest) return targetsRequest
    if (targetsLoaded.value && !force) return
    targetsLoading.value = true
    targetsError.value = false
    const currentEpoch = mutationEpoch
    targetsRequest = (async () => {
      try {
        const response = await listFollows()
        if (!response.success) throw new Error(response.message)
        if (currentEpoch === mutationEpoch) { targets.value = response.data || []; targetsLoaded.value = true }
      } catch (cause) { targetsError.value = true; throw cause }
      finally { targetsLoading.value = false; targetsRequest = undefined }
    })()
    return targetsRequest
  }
  async function refreshState() {
    const current = ++stateSequence
    try {
      const response = await getFollowState()
      if (!response.success) throw new Error(response.message)
      if (current === stateSequence) applyState(response.data)
    } catch { if (current === stateSequence) stateError.value = true }
  }
  async function markViewed(viewedAt?: string) {
    stateSequence++
    const response = await markFollowsViewed(viewedAt)
    if (!response.success) throw new Error(response.message)
    stateSequence++
    applyState(response.data)
    return response.data
  }
  async function setFollowing(type: FollowTargetType, id: string, name: string, following: boolean) {
    if (isPending(type, id)) return
    const targetKey = key(type, id)
    pending.value[targetKey] = true
    try {
      const response = following ? await followTarget(type, id) : await unfollowTarget(type, id)
      if (!response.success) throw new Error(response.message)
      mutationEpoch++
      targets.value = targets.value.filter(target => target.target_type !== type || target.target_id !== id)
      if (following) targets.value.push(response.data || { target_type: type, target_id: id, name, created_at: new Date().toISOString(), recent_article_count: 0 })
      void refreshState()
    } finally { delete pending.value[targetKey] }
  }
  async function refreshAfterTargetChange() {
    mutationEpoch++
    if (targetsRequest) await targetsRequest.catch(() => {})
    await Promise.all([loadTargets(true).catch(() => {}), refreshState()])
  }
  return { targets, targetsLoaded, targetsLoading, targetsError, newCount, lastViewedAt, stateError, isFollowing, isPending, loadTargets, refreshState, markViewed, setFollowing, refreshAfterTargetChange }
})
