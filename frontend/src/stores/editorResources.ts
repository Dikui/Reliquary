import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  getPromptTemplates,
  getParserEngines,
  getSystemInfo,
  type PromptTemplatesConfig,
  type ParserEngineInfo,
  type SystemInfo,
} from '@/api/system'
import { listMCPServices, type MCPService } from '@/api/mcp-service'
import { listSkillCatalog, listSkills, type SkillCatalogItem, type SkillInfo } from '@/api/skill'
import { getAgentTypePresets, getPlaceholders, type AgentTypePreset, type PlaceholdersResponse } from '@/api/agent'
import { getTenantRetrievalConfig } from '@/api/retrieval'

const CACHE_TTL_MS = 60_000

type EditorResourceKey =
  | 'mcpServices'
  | 'skills'
  | 'skillCatalog'
  | 'agentTypePresets'
  | 'promptTemplates'
  | 'placeholders'
  | 'tenantRetrievalConfig'
  | 'parserEngines'
  | 'systemInfo'

export const useEditorResourcesStore = defineStore('editorResources', () => {
  const mcpServices = ref<MCPService[]>([])
  const skills = ref<SkillInfo[]>([])
  const skillsAvailable = ref(false)
  const skillsConfigId = ref('')
  const skillCatalog = ref<SkillCatalogItem[]>([])
  const agentTypePresets = ref<AgentTypePreset[]>([])
  const promptTemplates = ref<PromptTemplatesConfig | null>(null)
  const placeholders = ref<PlaceholdersResponse | null>(null)
  const tenantRetrievalConfig = ref<Record<string, unknown> | null>(null)
  const parserEngines = ref<ParserEngineInfo[]>([])
  const systemInfo = ref<SystemInfo | null>(null)

  const loadedAt = ref<Partial<Record<EditorResourceKey, number>>>({})
  const inflight = new Map<EditorResourceKey, Promise<void>>()

  function isFresh(key: EditorResourceKey): boolean {
    const at = loadedAt.value[key]
    return !!at && Date.now() - at < CACHE_TTL_MS
  }

  async function runOnce(key: EditorResourceKey, force: boolean, loader: () => Promise<void>): Promise<void> {
    if (!force && isFresh(key)) return
    const existing = inflight.get(key)
    if (existing) return existing
    const p = loader().finally(() => inflight.delete(key))
    inflight.set(key, p)
    return p
  }

  async function ensureMcpServices(force = false): Promise<void> {
    return runOnce('mcpServices', force, async () => {
      const list = await listMCPServices()
      mcpServices.value = Array.isArray(list) ? list : []
      loadedAt.value.mcpServices = Date.now()
    })
  }

  async function ensureSkills(sandboxConfigId?: string, force = false): Promise<void> {
    const configId = sandboxConfigId?.trim() || ''
    if (configId !== skillsConfigId.value) {
      force = true
    }
    return runOnce('skills', force, async () => {
      skillsConfigId.value = configId
      if (!configId) {
        skillsAvailable.value = false
        skills.value = []
        loadedAt.value.skills = Date.now()
        return
      }
      try {
        const skillsRes = await listSkills(configId)
        skillsAvailable.value = skillsRes.skills_available !== false
        skills.value = skillsRes.data && skillsRes.data.length > 0 ? skillsRes.data : []
      } catch {
        skillsAvailable.value = false
        skills.value = []
      }
      loadedAt.value.skills = Date.now()
    })
  }

  async function ensureSkillCatalog(force = false): Promise<void> {
    return runOnce('skillCatalog', force, async () => {
      const res = await listSkillCatalog()
      skillCatalog.value = Array.isArray(res?.data) ? res.data : []
      loadedAt.value.skillCatalog = Date.now()
    })
  }

  async function ensureAgentTypePresets(force = false): Promise<void> {
    return runOnce('agentTypePresets', force, async () => {
      const presetsRes: any = await getAgentTypePresets()
      agentTypePresets.value = presetsRes?.data && Array.isArray(presetsRes.data) ? presetsRes.data : []
      loadedAt.value.agentTypePresets = Date.now()
    })
  }

  async function ensurePromptTemplates(force = false): Promise<void> {
    return runOnce('promptTemplates', force, async () => {
      const tmplRes = await getPromptTemplates()
      promptTemplates.value = tmplRes?.data ?? null
      loadedAt.value.promptTemplates = Date.now()
    })
  }

  async function ensurePlaceholders(force = false): Promise<void> {
    return runOnce('placeholders', force, async () => {
      const placeholdersRes = await getPlaceholders()
      placeholders.value = placeholdersRes?.data ?? null
      loadedAt.value.placeholders = Date.now()
    })
  }

  async function ensureTenantRetrievalConfig(force = false): Promise<void> {
    return runOnce('tenantRetrievalConfig', force, async () => {
      const retrievalRes: any = await getTenantRetrievalConfig()
      tenantRetrievalConfig.value = retrievalRes?.data ?? null
      loadedAt.value.tenantRetrievalConfig = Date.now()
    })
  }

  async function ensureParserEngines(force = false): Promise<void> {
    return runOnce('parserEngines', force, async () => {
      const resp = await getParserEngines()
      parserEngines.value = resp?.data && Array.isArray(resp.data) ? resp.data : []
      loadedAt.value.parserEngines = Date.now()
    })
  }

  async function ensureSystemInfo(force = false): Promise<void> {
    return runOnce('systemInfo', force, async () => {
      const response = await getSystemInfo()
      systemInfo.value = response?.data ?? null
      loadedAt.value.systemInfo = Date.now()
    })
  }

  async function prefetchAgentEditorDeps(force = false): Promise<void> {
    await Promise.all([
      ensureMcpServices(force),
      ensureAgentTypePresets(force),
      ensurePromptTemplates(force),
      ensurePlaceholders(force),
      ensureTenantRetrievalConfig(force),
    ])
  }

  function invalidate(...keys: EditorResourceKey[]) {
    if (keys.length === 0) {
      loadedAt.value = {}
      mcpServices.value = []
      skills.value = []
      skillsAvailable.value = false
      skillsConfigId.value = ''
      skillCatalog.value = []
      agentTypePresets.value = []
      promptTemplates.value = null
      placeholders.value = null
      tenantRetrievalConfig.value = null
      parserEngines.value = []
      systemInfo.value = null
      inflight.clear()
      return
    }
    keys.forEach((k) => {
      delete loadedAt.value[k]
      inflight.delete(k)
    })
  }

  return {
    mcpServices,
    skills,
    skillsAvailable,
    skillCatalog,
    agentTypePresets,
    promptTemplates,
    placeholders,
    tenantRetrievalConfig,
    parserEngines,
    systemInfo,
    ensureMcpServices,
    ensureSkills,
    ensureSkillCatalog,
    ensureAgentTypePresets,
    ensurePromptTemplates,
    ensurePlaceholders,
    ensureTenantRetrievalConfig,
    ensureParserEngines,
    ensureSystemInfo,
    prefetchAgentEditorDeps,
    invalidate,
  }
})
