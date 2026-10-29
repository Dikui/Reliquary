<template>
  <div class="system-info settings-form">
    <div class="section-header">
      <p class="section-description">{{ $t('system.sectionDescription') }}</p>
    </div>

    <!-- Loading state -->
    <div v-if="loading" class="loading-inline">
      <span class="rq-spinner" aria-hidden="true" />
      <span>{{ $t('system.loadingInfo') }}</span>
    </div>

    <!-- Error state -->
    <div v-else-if="error" class="error-inline">
      <div class="settings-error" role="alert">{{ error }}
        <div class="settings-actions">
          <Button @click="loadInfo">{{ $t('system.retry') }}</Button>
        </div>
      </div>
    </div>

    <!-- Content -->
    <div v-else class="settings-group">
      <!-- System version -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('system.versionLabel') }}</label>
          <p class="desc">{{ $t('system.versionDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">
              {{ systemInfo?.version || $t('system.unknown') }}
              <Tag
                v-if="systemInfo?.edition"
                style="margin-left: 8px;"
              >{{ systemInfo.edition }}</Tag>
              <span v-if="systemInfo?.commit_id" class="commit-info">
                ({{ systemInfo.commit_id }})
              </span>
          </span>
        </div>
      </div>

      <!-- Frontend version -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('system.frontendVersionLabel') }}</label>
          <p class="desc">{{ $t('system.frontendVersionDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">
            {{ frontendVersion }}
            <Tag
              v-if="systemInfo?.version && systemInfo.version !== 'unknown' && frontendVersion !== 'unknown' && systemInfo.version !== frontendVersion"
              style="margin-left: 8px;"
            >{{ $t('system.versionMismatch') }}</Tag>
            <span v-if="frontendCommit && frontendCommit !== 'unknown'" class="commit-info">
              ({{ frontendCommit }})
            </span>
          </span>
        </div>
      </div>

      <!-- Build time -->
      <div v-if="systemInfo?.build_time" class="setting-row">
        <div class="setting-info">
          <label>{{ $t('system.buildTimeLabel') }}</label>
          <p class="desc">{{ $t('system.buildTimeDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ systemInfo.build_time }}</span>
        </div>
      </div>

      <!-- Go version -->
      <div v-if="systemInfo?.go_version" class="setting-row">
        <div class="setting-info">
          <label>{{ $t('system.goVersionLabel') }}</label>
          <p class="desc">{{ $t('system.goVersionDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ systemInfo.go_version }}</span>
        </div>
      </div>

      <!-- Service started at -->
      <div v-if="systemInfo?.started_at" class="setting-row">
        <div class="setting-info">
          <label>{{ $t('system.startedAtLabel') }}</label>
          <p class="desc">{{ $t('system.startedAtDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ formatStartedAt(systemInfo.started_at) }}</span>
        </div>
      </div>

      <!-- Service uptime -->
      <div v-if="displayUptimeSeconds != null" class="setting-row">
        <div class="setting-info">
          <label>{{ $t('system.uptimeLabel') }}</label>
          <p class="desc">{{ $t('system.uptimeDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ formatUptime(displayUptimeSeconds) }}</span>
        </div>
      </div>

      <!-- DB Version -->
      <div v-if="systemInfo?.db_version || systemInfo?.db_migration_error" class="setting-row">
        <div class="setting-info">
          <label>{{ $t('system.dbVersionLabel') }}</label>
          <p class="desc">{{ $t('system.dbVersionDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">
            {{ systemInfo?.db_version || $t('system.unknown') }}
            <Tag
              v-if="systemInfo?.db_migration_error"
              variant="danger"
              style="margin-left: 8px;"
            >{{ $t('system.dbMigrationFailedTag') }}</Tag>
          </span>
        </div>
      </div>

      <!-- DB migration error: full-width banner under the row -->
      <div v-if="systemInfo?.db_migration_error" class="setting-row migration-error-row">
        <div class="settings-error" role="alert"><strong>{{ $t('system.dbMigrationFailedTitle') }}</strong>

            <p class="migration-error-desc">{{ $t('system.dbMigrationFailedDesc') }}</p>
            <pre class="migration-error-detail">{{ systemInfo.db_migration_error }}</pre>
            <div class="migration-error-actions">
              <a
                :href="troubleshootingDocsURL"
                target="_blank"
                rel="noopener noreferrer"
              >{{ $t('system.dbMigrationViewDocs') }}</a>
              <span class="migration-error-actions-sep">·</span>
              <a
                :href="reportIssueURL"
                target="_blank"
                rel="noopener noreferrer"
              >{{ $t('system.dbMigrationReportIssue') }}</a>
            </div>
        </div>
      </div>

      <!-- Keyword Index Engine -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('system.keywordIndexEngineLabel') }}</label>
          <p class="desc">{{ $t('system.keywordIndexEngineDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ systemInfo?.keyword_index_engine || $t('system.unknown') }}</span>
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('system.credentialEncryptionLabel') }}</label>
          <p class="desc">{{ $t(systemInfo?.credential_encryption_enabled ? 'system.credentialEncryptionEnabledDescription' : 'system.credentialEncryptionDisabledDescription') }}</p>
        </div>
        <div class="setting-control">
          <Tag :variant="systemInfo?.credential_encryption_enabled ? 'success' : 'plain'">
            {{ $t(systemInfo?.credential_encryption_enabled ? 'system.credentialEncryptionEnabled' : 'system.credentialEncryptionDisabled') }}
          </Tag>
        </div>
      </div>

    </div>
  </div>
</template>

<script setup lang="ts">
import { Button, Tag } from '@/components/ui'
import '@/styles/settings-form.css'
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { getSystemInfo, type SystemInfo } from '@/api/system'
import { useI18n } from 'vue-i18n'

const { t, locale } = useI18n()

// Reactive state
const systemInfo = ref<SystemInfo | null>(null)
const loading = ref(true)
const error = ref('')
const frontendVersion = __FRONTEND_VERSION__
const frontendCommit = __FRONTEND_COMMIT__

let uptimeTicker: ReturnType<typeof setInterval> | null = null
const uptimeTick = ref(0)

const displayUptimeSeconds = computed(() => {
  void uptimeTick.value
  const info = systemInfo.value
  if (info?.started_at) {
    const boot = new Date(info.started_at).getTime()
    if (!Number.isNaN(boot)) {
      return Math.floor((Date.now() - boot) / 1000)
    }
  }
  if (info?.uptime_seconds != null) return info.uptime_seconds
  return null
})

function formatStartedAt(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(locale.value)
}

function formatUptime(totalSeconds: number): string {
  const sec = Math.max(0, Math.floor(totalSeconds))
  const days = Math.floor(sec / 86400)
  const hours = Math.floor((sec % 86400) / 3600)
  const minutes = Math.floor((sec % 3600) / 60)
  const seconds = sec % 60
  const parts: string[] = []
  if (days > 0) parts.push(t('system.uptimeDays', { n: days }))
  if (hours > 0 || days > 0) parts.push(t('system.uptimeHours', { n: hours }))
  if (minutes > 0 || hours > 0 || days > 0) parts.push(t('system.uptimeMinutes', { n: minutes }))
  if (parts.length === 0) return t('system.uptimeSeconds', { n: seconds })
  if (seconds > 0 && days === 0) parts.push(t('system.uptimeSeconds', { n: seconds }))
  return parts.join(' ')
}

const troubleshootingDocsURL =
  'http://192.168.1.146:3000/dikui/reliquary/src/branch/main/docs/migration-troubleshooting.md'

// Pre-fills a new issue with the current migration error so users don't have to
// paste it manually. Body is intentionally minimal — the bug template will fill
// in the rest. Encode aggressively to survive newlines / quotes.
const reportIssueURL = computed(() => {
  const base = 'http://192.168.1.146:3000/dikui/reliquary/issues/new'
  const params = new URLSearchParams({
    template: 'bug_report.yml',
    title: '[Bug]: Database migration failed at startup',
    labels: 'bug',
  })
  const errMsg = systemInfo.value?.db_migration_error
  if (errMsg) {
    const body = [
      '### Environment',
      `- Reliquary version: ${systemInfo.value?.version || 'unknown'}`,
      `- Commit: ${systemInfo.value?.commit_id || 'unknown'}`,
      `- Frontend version: ${frontendVersion} (${frontendCommit})`,
      `- DB version reported: ${systemInfo.value?.db_version || 'unknown'}`,
      '',
      '### Migration error',
      '```',
      errMsg,
      '```',
    ].join('\n')
    params.set('body', body)
  }
  return `${base}?${params.toString()}`
})

// Methods
const loadInfo = async () => {
  try {
    loading.value = true
    error.value = ''

    const systemResponse = await getSystemInfo()

    if (systemResponse.data) {
      systemInfo.value = systemResponse.data
    } else {
      error.value = t('system.messages.fetchFailed')
    }
  } catch (err: any) {
    error.value = err?.message || t('system.messages.networkError')
  } finally {
    loading.value = false
  }
}

// Lifecycle
onMounted(() => {
  loadInfo()
  uptimeTicker = setInterval(() => {
    uptimeTick.value++
  }, 30_000)
})

onUnmounted(() => {
  if (uptimeTicker) {
    clearInterval(uptimeTicker)
    uptimeTicker = null
  }
})
</script>
