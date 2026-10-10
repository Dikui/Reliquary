import { computed } from 'vue'
import { defineStore } from 'pinia'
import i18n from '@/i18n'

export const useMenuStore = defineStore('menuStore', () => {
  const menuArr = computed(() => [
    { title: i18n.global.t('menu.knowledgeBase'), icon: 'file', path: 'knowledge' },
    { title: i18n.global.t('topicManagement.title'), icon: 'folder', path: 'topics' },
    { title: i18n.global.t('operationLogs.title'), icon: 'history', path: 'operation-logs' },
    { title: i18n.global.t('menu.settings'), icon: 'setting', path: 'settings' },
  ])
  return { menuArr, visibleMenuArr: menuArr }
})
