import { computed } from 'vue'
import { defineStore } from 'pinia'
import i18n from '@/i18n'

export const useMenuStore = defineStore('menuStore', () => {
  const menuArr = computed(() => [
    { title: i18n.global.t('menu.knowledgeBase'), icon: 'folder', path: 'knowledge-bases' },
    { title: i18n.global.t('menu.settings'), icon: 'setting', path: 'settings' },
  ])
  return { menuArr, visibleMenuArr: menuArr }
})
