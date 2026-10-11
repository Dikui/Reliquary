import { computed } from 'vue'
import { defineStore } from 'pinia'
import i18n from '@/i18n'

export const useMenuStore = defineStore('menuStore', () => {
  const menuArr = computed(() => [
    { key: 'articles', title: i18n.global.t('menu.knowledgeBase'), icon: 'file', path: 'knowledge', tab: undefined },
    { key: 'search', title: i18n.global.t('menu.search'), icon: 'search', path: 'knowledge-search', tab: undefined },
    { key: 'entities', title: i18n.global.t('knowledgeWorkbench.title'), icon: 'usergroup', path: 'knowledge', tab: 'entities' },
    { key: 'topics', title: i18n.global.t('topicManagement.title'), icon: 'folder', path: 'topics', tab: undefined },
    { key: 'follows', title: i18n.global.t('follows.title'), icon: 'heart', path: 'follows', tab: undefined },
    { key: 'settings', title: i18n.global.t('menu.settings'), icon: 'setting', path: 'settings', tab: undefined },
  ])
  return { menuArr, visibleMenuArr: menuArr }
})
