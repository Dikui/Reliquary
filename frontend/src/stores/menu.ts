import { computed } from 'vue'
import { defineStore } from 'pinia'
import i18n from '@/i18n'

export const useMenuStore = defineStore('menuStore', () => {
  const menuArr = computed(() => [
    { key: 'articles', title: i18n.global.t('menu.library'), icon: 'file', path: 'entries', tab: undefined },
    { key: 'stories', title: i18n.global.t('stories.title'), icon: 'time', path: 'stories', tab: undefined },
    { key: 'search', title: i18n.global.t('menu.search'), icon: 'search', path: 'search', tab: undefined },
    { key: 'entities', title: i18n.global.t('knowledgeWorkbench.title'), icon: 'usergroup', path: 'entries', tab: 'entities' },
    { key: 'domains', title: i18n.global.t('domains.title'), icon: 'tag', path: 'entries', tab: 'domains' },
    { key: 'follows', title: i18n.global.t('follows.title'), icon: 'heart', path: 'follows', tab: undefined },
    { key: 'pending', title: i18n.global.t('ingestDiagnostics.pending'), icon: 'task', path: 'entries', tab: 'reviews' },
    { key: 'evaluation', title: i18n.global.t('ikb19.evaluationTitle'), icon: 'task', path: 'evaluation', tab: undefined },
    { key: 'modelCalls', title: i18n.global.t('ikb19.callsTitle'), icon: 'chart-line', path: 'model-calls', tab: undefined },
    { key: 'settings', title: i18n.global.t('menu.settings'), icon: 'setting', path: 'settings', tab: undefined },
  ])
  const menuGroups = computed(() => [
    { key: 'materials', title: i18n.global.t('ingestDiagnostics.materialManagement'), items: menuArr.value.filter(item => ['articles', 'stories', 'search', 'entities', 'domains', 'follows'].includes(item.key)) },
    { key: 'pending', title: i18n.global.t('ingestDiagnostics.pending'), items: menuArr.value.filter(item => item.key === 'pending') },
    { key: 'diagnostics', title: i18n.global.t('ingestDiagnostics.diagnostics'), items: menuArr.value.filter(item => ['evaluation', 'modelCalls'].includes(item.key)) },
    { key: 'configuration', title: i18n.global.t('ingestDiagnostics.configuration'), items: menuArr.value.filter(item => item.key === 'settings') },
  ])
  return { menuArr, visibleMenuArr: menuArr, menuGroups }
})
