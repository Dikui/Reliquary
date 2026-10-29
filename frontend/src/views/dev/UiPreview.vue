<script setup lang="ts">
import { computed, ref, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme, type ThemeMode } from '@/composables/useTheme'
import {
  Button, Field, Input, Textarea, Select, Checkbox, Switch, Radio, Dialog, Drawer, Tabs,
  DropdownMenu, Combobox, Tooltip, Tag, Chip, LibMark, StatusSquare, EmptyState, ErrorState, Skeleton, DataTable, toast, confirm,
} from '@/components/ui'

const { t, locale } = useI18n()
const { currentTheme, setTheme } = useTheme()
const text = ref(''), notes = ref(''), choice = ref('research'), checked = ref(false), enabled = ref(true), radio = ref('a'), chip = ref(false)
const dialog = ref(false), drawer = ref(false), tab = ref('list'), selected = ref<string[]>([]), focused = ref('a')
const options = computed(() => [{ value: 'research', label: t('ui.research') }, { value: 'reports', label: t('ui.reports') }])
const tabs = computed(() => [{ value: 'list', label: t('ui.tabList') }, { value: 'table', label: t('ui.tabTable') }])
const columns = computed(() => [{ key: 'title', label: t('ui.columnTitle') }, { key: 'status', label: t('ui.columnStatus') }])
const rows = computed(() => [{ id: 'a', title: t('ui.rowA'), status: t('ui.success') }, { id: 'b', title: t('ui.rowB'), status: t('ui.warning') }, { id: 'c', title: t('ui.rowC'), status: t('ui.error') }])
const notify = () => { toast.success(t('ui.toastMessage')) }
const confirmExample = () => confirm({ header: t('ui.confirmTitle'), body: t('ui.confirmBody'), confirmBtn: t('ui.confirmButton'), onConfirm: notify })
watchEffect(() => { document.title = `${t('ui.previewTitle')} · Reliquary` })
</script>

<template>
  <main class="rq-ui ui-preview">
    <header>
      <h1>{{ t('ui.previewTitle') }}</h1><p>{{ t('ui.previewHint') }}</p>
      <div class="ui-preview__row">
        <Field :label="t('ui.theme')"><Select :model-value="currentTheme" @update:model-value="setTheme($event as ThemeMode)"><option value="light">{{ t('ui.light') }}</option><option value="dark">{{ t('ui.dark') }}</option><option value="system">{{ t('ui.system') }}</option></Select></Field>
        <Field :label="t('ui.locale')"><Select v-model="locale"><option value="zh-CN">中文</option><option value="en-US">English</option></Select></Field>
      </div>
    </header>
    <section><h2>{{ t('ui.buttons') }}</h2><div class="ui-preview__row">
      <Button variant="primary" @click="notify">{{ t('ui.primary') }}</Button><Button @click="notify">{{ t('ui.default') }}</Button><Button variant="ghost" @click="notify">{{ t('ui.ghost') }}</Button><Button variant="danger" @click="confirmExample">{{ t('ui.danger') }}</Button><Button loading disabled>{{ t('ui.busy') }}</Button><Button disabled>{{ t('ui.disabled') }}</Button>
    </div></section>
    <section><h2>{{ t('ui.forms') }}</h2><div class="ui-preview__grid">
      <Field :label="t('ui.fieldLabel')" :hint="t('ui.fieldHint')" :error="text ? undefined : t('ui.fieldError')"><Input v-model="text" /></Field>
      <Field :label="t('ui.notes')"><Textarea v-model="notes" /></Field>
      <Field :label="t('ui.library')"><Select v-model="choice"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></Select></Field>
      <Field :label="t('ui.showOptions')"><Combobox v-model="choice" :options="options" /></Field>
      <label class="ui-preview__row"><Checkbox v-model="checked" />{{ t('ui.check') }}</label>
      <label class="ui-preview__row"><Switch v-model="enabled" />{{ t('ui.switch') }}</label>
      <div class="ui-preview__row"><label><Radio v-model="radio" name="preview-radio" value="a" /> {{ t('ui.radioA') }}</label><label><Radio v-model="radio" name="preview-radio" value="b" /> {{ t('ui.radioB') }}</label></div>
    </div></section>
    <section><h2>{{ t('ui.overlays') }}</h2><div class="ui-preview__row">
      <Dialog v-model="dialog" :title="t('ui.dialogTitle')" :description="t('ui.overlayHint')"><template #trigger><Button>{{ t('ui.openDialog') }}</Button></template><Field :label="t('ui.fieldLabel')"><Input v-model="text" /></Field><template #footer><Button @click="dialog = false">{{ t('common.close') }}</Button></template></Dialog>
      <Drawer v-model="drawer" :title="t('ui.drawerTitle')" :description="t('ui.overlayHint')"><template #trigger><Button>{{ t('ui.openDrawer') }}</Button></template><Field :label="t('ui.notes')"><Textarea v-model="notes" /></Field><template #footer><Button @click="drawer = false">{{ t('common.close') }}</Button></template></Drawer>
      <DropdownMenu :items="[{ value: 'notify', label: t('ui.toastAction') }, { value: 'disabled', label: t('ui.disabled'), disabled: true }]" @select="notify"><Button>{{ t('ui.menu') }}</Button></DropdownMenu>
      <Tooltip :content="t('ui.tooltip')"><Button>{{ t('ui.tooltip') }}</Button></Tooltip>
      <Button @click="notify">{{ t('ui.toastAction') }}</Button><Button @click="confirmExample">{{ t('ui.confirmAction') }}</Button>
    </div></section>
    <section><h2>{{ t('ui.dataStates') }}</h2><div class="ui-preview__row">
      <Tag variant="accent">{{ t('ui.sampleText') }}</Tag><Tag>{{ t('ui.sampleText') }}</Tag><Chip v-model="chip">{{ t('ui.sampleText') }}</Chip>
      <LibMark v-for="color in ([1, 2, 3, 4, 5, 6, 7, 8] as const)" :key="color" :color="color" :label="`${t('ui.library')} ${color}`" />
      <StatusSquare status="success" :label="t('ui.success')" /><StatusSquare status="warning" :label="t('ui.warning')" /><StatusSquare status="danger" :label="t('ui.error')" />
    </div>
      <Tabs v-model="tab" :label="t('ui.dataStates')" :items="tabs"><template #list>{{ t('ui.tabContent') }}</template><template #table>{{ t('ui.tabTable') }}</template></Tabs>
      <p>{{ t('ui.selection', { count: selected.length }) }}</p>
      <DataTable v-model:selected="selected" v-model:focused="focused" :rows="rows" :columns="columns" :label="t('ui.tableTitle')" selectable />
      <div class="ui-preview__grid"><EmptyState :title="t('ui.emptyTitle')" :description="t('ui.emptyHint')" /><ErrorState :title="t('ui.errorTitle')" :description="t('ui.errorHint')"><Button @click="notify">{{ t('ui.retry') }}</Button></ErrorState></div>
      <Skeleton :label="t('ui.loading')" />
    </section>
  </main>
</template>

<style scoped>
.ui-preview { min-height: 100%; padding: var(--space-6); background: var(--bg); color: var(--text); }
.ui-preview h1 { font-size: var(--text-2xl); margin: 0; }
.ui-preview h2 { font-size: var(--text-lg); margin: 0 0 var(--space-4); }
.ui-preview section { margin-top: var(--space-6); padding-top: var(--space-4); border-top: 2px solid var(--rule); }
.ui-preview__row { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); }
.ui-preview__grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 288px), 1fr)); gap: var(--space-5); }
.ui-preview .rq-skeleton { height: 64px; }
</style>
