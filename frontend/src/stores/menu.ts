import { computed } from 'vue'
import { defineStore } from 'pinia'
import i18n from '@/i18n'

export const useMenuStore = defineStore('menuStore', () => {
  const menuArr = computed(() => [
    { key: 'articles', title: i18n.global.t('menu.materials'), icon: 'file', path: 'libraries', tab: undefined },
    { key: 'search', title: i18n.global.t('menu.searchMaterials'), icon: 'search', path: 'search', tab: undefined },
    { key: 'entities', title: i18n.global.t('menu.entities'), icon: 'usergroup', path: 'entities' },
    { key: 'topics', title: i18n.global.t('menu.topics'), icon: 'folder', path: 'topics', tab: undefined },
    { key: 'follows', title: i18n.global.t('menu.followUpdates'), icon: 'heart', path: 'follows', tab: undefined },
    { key: 'pending', title: i18n.global.t('menu.pending'), icon: 'task', path: 'entries', tab: 'reviews' },
    { key: 'diagnostics', title: i18n.global.t('menu.ingestFailures'), icon: 'tools', path: 'diagnostics', tab: undefined },
    { key: 'contentChecks', title: i18n.global.t('contentCheck.title'), icon: 'task', path: 'content-checks', tab: undefined },
    { key: 'evaluation', title: i18n.global.t('ikb19.evaluationTitle'), icon: 'task', path: 'evaluation', tab: undefined },
    { key: 'modelCalls', title: i18n.global.t('menu.modelCalls'), icon: 'chart-line', path: 'model-calls', tab: undefined },
  ])
  const menuGroups = computed(() => [
    { key: 'materials', title: i18n.global.t('menu.mainNavigation'), items: menuArr.value.filter(item => ['articles', 'search', 'entities', 'topics', 'follows', 'pending'].includes(item.key)) },
    { key: 'diagnostics', title: i18n.global.t('ingestDiagnostics.diagnostics'), items: menuArr.value.filter(item => ['diagnostics', 'contentChecks', 'evaluation', 'modelCalls'].includes(item.key)) },
  ])
  return { menuArr, visibleMenuArr: menuArr, menuGroups }
})
