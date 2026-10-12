import { defineStore } from 'pinia'

export const useUIStore = defineStore('ui', {
  state: () => ({
    showSettingsModal: false,
    settingsInitialSection: null as string | null,
    settingsInitialSubSection: null as string | null,
    manualEditorVisible: false,
    manualEditorMode: 'create' as 'create' | 'edit',
    manualEditorKBId: null as string | null,
    manualEditorEntryId: null as string | null,
    manualEditorInitialTitle: '',
    manualEditorInitialContent: '',
    manualEditorInitialStatus: 'draft' as 'draft' | 'publish',
    manualEditorOnSuccess: null as null | ((payload: { kbId: string; entryId: string; status: 'draft' | 'publish' }) => void),
    sidebarCollapsed: localStorage.getItem('sidebar_collapsed') === 'true'
  }),

  actions: {
    openSettings(section?: string, subSection?: string) {
      this.settingsInitialSection = section || null
      this.settingsInitialSubSection = subSection || null
      this.showSettingsModal = true
    },

    closeSettings() {
      this.showSettingsModal = false
      this.settingsInitialSection = null
      this.settingsInitialSubSection = null
    },

    toggleSettings() {
      this.showSettingsModal = !this.showSettingsModal
    },

    openManualEditor(options: {
      mode?: 'create' | 'edit'
      kbId?: string | null
      entryId?: string | null
      title?: string
      content?: string
      status?: 'draft' | 'publish'
      onSuccess?: (payload: { kbId: string; entryId: string; status: 'draft' | 'publish' }) => void
    } = {}) {
      this.manualEditorMode = options.mode || 'create'
      this.manualEditorKBId = options.kbId ?? null
      this.manualEditorEntryId = options.entryId ?? null
      this.manualEditorInitialTitle = options.title || ''
      this.manualEditorInitialContent = options.content || ''
      this.manualEditorInitialStatus = options.status || 'draft'
      this.manualEditorOnSuccess = options.onSuccess || null
      this.manualEditorVisible = true
    },

    closeManualEditor() {
      this.manualEditorVisible = false
      this.manualEditorEntryId = null
      this.manualEditorInitialContent = ''
      this.manualEditorInitialTitle = ''
      this.manualEditorInitialStatus = 'draft'
      this.manualEditorOnSuccess = null
    },

    notifyManualEditorSuccess(payload: { kbId: string; entryId: string; status: 'draft' | 'publish' }) {
      if (typeof this.manualEditorOnSuccess === 'function') {
        try {
          this.manualEditorOnSuccess(payload)
        } catch (err) {
          console.error('Manual editor success callback error:', err)
        }
      }
      this.manualEditorOnSuccess = null
    },

    toggleSidebar() {
      this.sidebarCollapsed = !this.sidebarCollapsed
      localStorage.setItem('sidebar_collapsed', String(this.sidebarCollapsed))
    },

    collapseSidebar() {
      this.sidebarCollapsed = true
      localStorage.setItem('sidebar_collapsed', 'true')
    },

    expandSidebar() {
      this.sidebarCollapsed = false
      localStorage.setItem('sidebar_collapsed', 'false')
    }
  }
})

