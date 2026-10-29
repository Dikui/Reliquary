import { computed } from 'vue'
import { defineStore } from 'pinia'
import i18n from '@/i18n'

export const useMenuStore = defineStore('menuStore', () => {
  const menuArr = computed(() => [
    { key: 'articles', title: i18n.global.t('menu.library'), icon: 'file', path: 'libraries', tab: undefined },
    { key: 'search', title: i18n.global.t('menu.search'), icon: 'search', path: 'search', tab: undefined },
    { key: 'entities', title: i18n.global.t('knowledgeWorkbench.title'), icon: 'usergroup', path: 'entries', tab: 'entities' },
    { key: 'topics', title: i18n.global.t('topicManagement.title'), icon: 'folder', path: 'topics', tab: undefined },
    { key: 'follows', title: i18n.global.t('follows.title'), icon: 'heart', path: 'follows', tab: undefined },
    { key: 'pending', title: i18n.global.t('ingestDiagnostics.pending'), icon: 'task', path: 'entries', tab: 'reviews' },
    { key: 'diagnostics', title: i18n.global.t('ingestDiagnostics.diagnostics'), icon: 'tools', path: 'diagnostics', tab: undefined },
    { key: 'contentChecks', title: i18n.global.t('contentCheck.title'), icon: 'task', path: 'content-checks', tab: undefined },
    { key: 'evaluation', title: i18n.global.t('ikb19.evaluationTitle'), icon: 'task', path: 'evaluation', tab: undefined },
    { key: 'modelCalls', title: i18n.global.t('ikb19.callsTitle'), icon: 'chart-line', path: 'model-calls', tab: undefined },
  ])
  const menuGroups = computed(() => [
    { key: 'materials', title: i18n.global.t('ingestDiagnostics.materialManagement'), items: menuArr.value.filter(item => ['articles', 'search', 'entities', 'topics', 'follows'].includes(item.key)) },
    { key: 'pending', title: i18n.global.t('ingestDiagnostics.pending'), items: menuArr.value.filter(item => item.key === 'pending') },
    { key: 'diagnostics', title: i18n.global.t('ingestDiagnostics.diagnostics'), items: menuArr.value.filter(item => ['diagnostics', 'contentChecks', 'evaluation', 'modelCalls'].includes(item.key)) },
  ])
  return { menuArr, visibleMenuArr: menuArr, menuGroups }
})
