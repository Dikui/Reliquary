import { ref } from 'vue'
import { defineStore } from 'pinia'
import { followTarget, listFollows, unfollowTarget } from '@/api/follows'
import type { FollowTarget, FollowTargetType } from '@/types/follows'

export const useFollowStore = defineStore('follows', () => {
  const targets = ref<FollowTarget[]>([]), targetsLoaded = ref(false), targetsLoading = ref(false), targetsError = ref(false)
  const pending = ref<Record<string, boolean>>({})
  let targetsRequest: Promise<void> | undefined, mutationEpoch = 0
  const key = (type: FollowTargetType, id: string) => `${type}:${id}`
  const isFollowing = (type: FollowTargetType, id: string) => targets.value.some(target => target.target_type === type && target.target_id === id)
  const isPending = (type: FollowTargetType, id: string) => Boolean(pending.value[key(type, id)])
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
  async function setFollowing(type: FollowTargetType, id: string, name: string, following: boolean) {
    if (isPending(type, id)) return
    const targetKey = key(type, id)
    pending.value[targetKey] = true
    try {
      const response = following ? await followTarget(type, id) : await unfollowTarget(type, id)
      if (!response.success) throw new Error(response.message)
      mutationEpoch++
      targets.value = targets.value.filter(target => target.target_type !== type || target.target_id !== id)
      if (following) {
        const canonical = response.data || { target_type: type, target_id: id, name, created_at: new Date().toISOString() }
        targets.value = targets.value.filter(target => target.target_type !== canonical.target_type || target.target_id !== canonical.target_id)
        targets.value.push(canonical)
      }
    } finally { delete pending.value[targetKey] }
  }
  async function refreshAfterTargetChange() {
    mutationEpoch++
    if (targetsRequest) await targetsRequest.catch(() => {})
    await loadTargets(true).catch(() => {})
  }
  return { targets, targetsLoaded, targetsLoading, targetsError, isFollowing, isPending, loadTargets, setFollowing, refreshAfterTargetChange }
})
