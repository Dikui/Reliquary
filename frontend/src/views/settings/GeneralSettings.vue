<template>
  <div class="general-settings settings-form">
    <div class="section-header">
      <p class="section-description">{{ $t('general.description') }}</p>
    </div>

    <div class="settings-group">
      <!-- 语言选择 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('language.language') }}</label>
          <p class="desc">{{ $t('language.languageDescription') }}</p>
        </div>
        <div class="setting-control">
          <Select
            v-model="localLanguage" :aria-label="$t('language.language')"
            :placeholder="$t('language.selectLanguage')"
            @update:model-value="handleLanguageChange"
            class="setting-select"
          >
            <option value="zh-CN" :label="$t('language.zhCN')">{{ $t('language.zhCN') }}</option>
            <option value="en-US" :label="$t('language.enUS')">{{ $t('language.enUS') }}</option>
          </Select>
        </div>
      </div>

      <!-- 主题设置 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('theme.theme') }}</label>
          <p class="desc">{{ $t('theme.themeDescription') }}</p>
        </div>
        <div class="setting-control">
          <Select
            v-model="localTheme" :aria-label="$t('theme.theme')"
            class="setting-select"
            :placeholder="$t('theme.selectTheme')"
            @update:model-value="value => handleThemeChange(value as ThemeMode)"
          >
            <option value="light" :label="$t('theme.light')">{{ $t('theme.light') }}</option>
            <option value="dark" :label="$t('theme.dark')">{{ $t('theme.dark') }}</option>
            <option value="system" :label="$t('theme.system')">{{ $t('theme.system') }}</option>
          </Select>
        </div>
      </div>

      <!-- 界面字体 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('font.uiFont') }}</label>
          <p class="desc">{{ $t('font.uiFontDescription') }}</p>
        </div>
        <div class="setting-control setting-control--stacked">
          <Select
            v-model="localSansFont" :aria-label="$t('font.uiFont')"
            class="setting-select"
            :placeholder="$t('font.selectFont')"
            @update:model-value="value => handleSansFontChange(value as FontKey)"
          >
            <option
              v-for="opt in sansFontOptions"
              :key="opt.value"
              :value="opt.value"
              :label="opt.label" :style="{ fontFamily: opt.preview }"
            >
              {{ opt.label }}
            </option>
          </Select>
          <div class="font-preview" :style="{ fontFamily: currentSansStack }">
            {{ $t('font.sansPreview') }}
          </div>
        </div>
      </div>

      <!-- 代码字体 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('font.monoFont') }}</label>
          <p class="desc">{{ $t('font.monoFontDescription') }}</p>
        </div>
        <div class="setting-control setting-control--stacked">
          <Select
            v-model="localMonoFont" :aria-label="$t('font.monoFont')"
            class="setting-select"
            :placeholder="$t('font.selectFont')"
            @update:model-value="value => handleMonoFontChange(value as MonoFontKey)"
          >
            <option
              v-for="opt in monoFontOptions"
              :key="opt.value"
              :value="opt.value"
              :label="opt.label" :style="{ fontFamily: opt.preview }"
            >
              {{ opt.label }}
            </option>
          </Select>
          <div class="font-preview font-preview--mono" :style="{ fontFamily: currentMonoStack }">
            {{ $t('font.monoPreview') }}
          </div>
        </div>
      </div>

      <!-- 字体大小 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('font.fontSize') }}</label>
          <p class="desc">{{ $t('font.fontSizeDescription') }}</p>
        </div>
        <div class="setting-control">
          <Select v-model="localFontSize" :aria-label="$t('font.fontSize')" @update:model-value="value => handleFontSizeChange(value as FontSizeKey)">
            <option value="small">{{ $t('font.size.small') }}</option>
            <option value="normal">{{ $t('font.size.normal') }}</option>
            <option value="large">{{ $t('font.size.large') }}</option>
          </Select>
        </div>
      </div>

    </div>
  </div>
</template>

<script setup lang="ts">
import { Select, toast } from '@/components/ui'
import '@/styles/settings-form.css'
import { ref, onMounted, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { SUPPORTED_LOCALES } from '@/i18n/resolveDefaultLocale'
import { useTheme, type ThemeMode } from '@/composables/useTheme'
import {
  useFont,
  SANS_STACKS,
  MONO_STACKS,
  visibleSansKeys,
  visibleMonoKeys,
  type FontKey,
  type MonoFontKey,
  type FontSizeKey,
} from '@/composables/useFont'

const { t, locale } = useI18n()
const { currentTheme, setTheme } = useTheme()
const {
  currentSans,
  currentMono,
  currentSize,
  setSansFont,
  setMonoFont,
  setFontSize,
} = useFont()

// 本地状态
const localLanguage = ref('zh-CN')
const localTheme = ref<ThemeMode>(currentTheme.value)
const localSansFont = ref<FontKey>(currentSans.value)
const localMonoFont = ref<MonoFontKey>(currentMono.value)
const localFontSize = ref<FontSizeKey>(currentSize.value)

// Keep the form in sync if preferences change externally (e.g. on user switch).
watch(currentTheme, (val) => { localTheme.value = val })
watch(currentSans, (val) => { localSansFont.value = val })
watch(currentMono, (val) => { localMonoFont.value = val })
watch(currentSize, (val) => { localFontSize.value = val })

const sansFontOptions = computed<{ value: FontKey; label: string; preview: string }[]>(() =>
  visibleSansKeys().map((key) => ({
    value: key,
    label: t(`font.sans.${key}`),
    preview: SANS_STACKS[key],
  })),
)

const monoFontOptions = computed<{ value: MonoFontKey; label: string; preview: string }[]>(() =>
  visibleMonoKeys().map((key) => ({
    value: key,
    label: t(`font.mono.${key}`),
    preview: MONO_STACKS[key],
  })),
)

// Live preview stacks, driven by the local form refs so the preview row
// updates immediately on selection — even before handleSansFontChange
// commits the choice to the global store and writes the CSS variable.
const currentSansStack = computed(() => SANS_STACKS[localSansFont.value] ?? SANS_STACKS.system)
const currentMonoStack = computed(() => MONO_STACKS[localMonoFont.value] ?? MONO_STACKS.system)

// 初始化加载
onMounted(() => {
  // 从 localStorage 加载语言设置
  const savedLocale = localStorage.getItem('locale')
  if (savedLocale && (SUPPORTED_LOCALES as readonly string[]).includes(savedLocale)) {
    localLanguage.value = savedLocale
    locale.value = savedLocale
  } else {
    localLanguage.value = locale.value
  }
})

// 处理语言变化
const handleLanguageChange = () => {
  locale.value = localLanguage.value
  localStorage.setItem('locale', localLanguage.value)
  toast.success(t('language.languageSaved'))
    }

// 处理主题变化
const handleThemeChange = (val: ThemeMode) => {
  if (!setTheme(val)) {
    // Setter rejected the value (validation guard); roll the form back to
    // the canonical state so the UI doesn't drift.
    localTheme.value = currentTheme.value
    return
  }
  toast.success(t('common.success'))
}

// 处理字体变化
const handleSansFontChange = (val: FontKey) => {
  if (!setSansFont(val)) {
    localSansFont.value = currentSans.value
    return
  }
  toast.success(t('common.success'))
}

const handleMonoFontChange = (val: MonoFontKey) => {
  if (!setMonoFont(val)) {
    localMonoFont.value = currentMono.value
    return
  }
  toast.success(t('common.success'))
}

const handleFontSizeChange = (val: FontSizeKey) => {
  if (!setFontSize(val)) {
    localFontSize.value = currentSize.value
    return
  }
  toast.success(t('common.success'))
}
</script>
