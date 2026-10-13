import { defineStore } from 'pinia'
import type { EntryProcessOverrides } from '@/types/entryProcess'

export interface ReparseConfirmSource {
  entryId: string
  fileName?: string
  fileType?: string
  processOverrides?: EntryProcessOverrides | null
}

export interface ReparseConfirmResult {
  processConfig: EntryProcessOverrides
  reparse: ReparseConfirmSource
}

export const useReparseConfirmStore = defineStore('reparseConfirm', {
  state: () => ({
    visible: false,
    libraryInfo: null as any,
    reparse: null as ReparseConfirmSource | null,
    pendingResolve: null as ((value: ReparseConfirmResult) => void) | null,
    pendingReject: null as (() => void) | null,
  }),
  actions: {
    open(options: { libraryInfo: any; reparse: ReparseConfirmSource }): Promise<ReparseConfirmResult> {
      return new Promise((resolve, reject) => {
        this.visible = true
        this.libraryInfo = options.libraryInfo
        this.reparse = options.reparse
        this.pendingResolve = resolve
        this.pendingReject = reject
      })
    },
    resolveConfirm(payload: ReparseConfirmResult) {
      this.pendingResolve?.(payload)
      this.reset()
    },
    rejectConfirm() {
      this.pendingReject?.()
      this.reset()
    },
    reset() {
      this.visible = false
      this.libraryInfo = null
      this.reparse = null
      this.pendingResolve = null
      this.pendingReject = null
    },
  },
})
