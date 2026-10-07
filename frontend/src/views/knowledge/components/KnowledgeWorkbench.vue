<template>
  <section class="knowledge-workbench">
    <header class="workbench-header">
      <div>
        <h2>{{ t('knowledgeWorkbench.title') }}</h2>
        <p>{{ t('knowledgeWorkbench.subtitle') }}</p>
      </div>
      <div class="workbench-header__actions">
        <t-button variant="outline" :loading="loading" @click="refreshAll">
          <template #icon><t-icon name="refresh" /></template>
          {{ t('knowledgeWorkbench.refresh') }}
        </t-button>
        <t-button v-if="canManage" variant="outline" :loading="rebuilding" @click="rebuild">
          <template #icon><t-icon name="refresh" /></template>
          {{ t('knowledgeWorkbench.rebuild') }}
        </t-button>
      </div>
    </header>

    <div class="workbench-scope" v-if="!['workflows', 'reports'].includes(activeSection)">
      <label><span>{{ t('knowledgeWorkbench.scope.label') }}</span><t-select v-model="queryScope" :options="scopeOptions" :aria-label="t('knowledgeWorkbench.scope.label')" /></label>
      <label v-if="queryScope === 'domain'"><span>{{ t('knowledgeDomains.title') }}</span><t-select v-model="queryDomainId" filterable :loading="domainsLoading" :options="domainOptions" :placeholder="t('knowledgeWorkbench.scope.selectDomain')" /></label>
    </div>
    <p v-if="domainLoadError" class="run-error" role="alert">{{ domainLoadError }} <t-button variant="text" :loading="domainsLoading" @click="loadDomains">{{ t('knowledgeDomains.retry') }}</t-button></p>
    <p v-if="['workflows', 'reports'].includes(activeSection)" class="muted">{{ t('knowledgeWorkbench.scope.workflowHint') }}</p>

    <nav class="workbench-nav" :aria-label="t('knowledgeWorkbench.sections')">
      <button v-for="section in sections" :key="section.id" type="button"
        :class="['workbench-nav__item', { active: activeSection === section.id }]"
        @click="selectSection(section.id)">
        <span>{{ t(`knowledgeWorkbench.nav.${section.id}`) }}</span>
        <span v-if="section.id === 'events' && events.length" class="workbench-nav__count">{{ events.length }}</span>
        <span v-if="section.id === 'workflows' && workflows.length" class="workbench-nav__count">{{ workflows.length }}</span>
      </button>
    </nav>

    <t-alert v-if="loadError" theme="error" :message="loadError" />

    <section v-if="activeSection === 'query'" class="workbench-panel">
      <div class="panel-heading">
        <div>
          <h3>{{ t('knowledgeWorkbench.query.title') }}</h3>
          <p>{{ t('knowledgeWorkbench.query.description') }}</p>
        </div>
      </div>
      <div class="query-form">
        <label>
          <span>{{ t('knowledgeWorkbench.query.entity') }}</span>
          <t-input v-model.trim="queryForm.entity" clearable :placeholder="t('knowledgeWorkbench.query.entityPlaceholder')" @enter="onQueryEnter" />
        </label>
        <label>
          <span>{{ t('knowledgeWorkbench.query.predicate') }}</span>
          <t-input v-model.trim="queryForm.predicate" clearable :placeholder="t('knowledgeWorkbench.query.predicatePlaceholder')" @enter="onQueryEnter" />
        </label>
        <label>
          <span>{{ t('knowledgeWorkbench.query.relation') }}</span>
          <t-input v-model.trim="queryForm.relation" clearable :placeholder="t('knowledgeWorkbench.query.relationPlaceholder')" @enter="onQueryEnter" />
        </label>
        <label>
          <span>{{ t('knowledgeWorkbench.query.since') }}</span>
          <input v-model="queryForm.since" class="native-input" type="date" />
        </label>
        <label>
          <span>{{ t('knowledgeWorkbench.query.until') }}</span>
          <input v-model="queryForm.until" class="native-input" type="date" />
        </label>
        <label class="history-toggle">
          <t-checkbox v-model="queryForm.include_history">{{ t('knowledgeWorkbench.query.history') }}</t-checkbox>
        </label>
        <label>
          <span>{{ t('knowledgeWorkbench.fusion.minValue') }}</span>
          <input v-model="queryForm.min_value" class="native-input" type="number" step="any" />
        </label>
        <label>
          <span>{{ t('knowledgeWorkbench.fusion.maxValue') }}</span>
          <input v-model="queryForm.max_value" class="native-input" type="number" step="any" />
        </label>
        <label>
          <span>{{ t('knowledgeWorkbench.fusion.unit') }}</span>
          <t-input v-model.trim="queryForm.unit" clearable :placeholder="t('knowledgeWorkbench.fusion.unit')" />
        </label>
        <p class="span-two">{{ t('knowledgeWorkbench.fusion.queryHint') }}</p>
        <div class="query-form__actions">
          <t-button theme="primary" :loading="queryLoading" :disabled="queryScope === 'domain' && !queryDomainId" @click="runQuery">
            <template #icon><t-icon name="search" /></template>
            {{ t('knowledgeWorkbench.query.submit') }}
          </t-button>
          <t-button variant="text" @click="clearQuery">{{ t('knowledgeWorkbench.query.clear') }}</t-button>
        </div>
      </div>

      <div v-if="queryResult" class="query-results">
        <div class="result-summary">
          <span>{{ t('knowledgeWorkbench.query.resultCounts', {
            entities: queryResult.entities.length,
            facts: queryResult.facts.length,
            events: queryResult.events.length,
            evidence: queryResult.evidence.length,
          }) }}</span>
          <span v-if="queryResult.facts.some(fact => fact.status === 'superseded')" class="history-note">
            {{ t('knowledgeWorkbench.query.historyIncluded') }}
          </span>
        </div>
        <t-empty v-if="!queryResult.facts.length && !queryResult.events.length && !queryResult.evidence.length"
          :description="t('knowledgeWorkbench.query.empty')" />
        <div v-if="queryResult.facts.length" class="result-group">
          <h4>{{ t('knowledgeWorkbench.facts') }}</h4>
          <FactCard v-for="fact in queryResult.facts" :key="fact.id" :fact="fact"
            :facts="queryResult.facts" :entities="queryResult.entities" :evidence="queryResult.evidence" :can-manage="canManage"
            @correct="startCorrection" @open-evidence="openEvidence" @open-source="emit('open-source', $event)" />
        </div>
        <div v-if="queryResult.events.length" class="result-group">
          <h4>{{ t('knowledgeWorkbench.events.title') }}</h4>
          <EventCard v-for="event in queryResult.events" :key="event.id" :event="event"
            :attention="queryResult.attention.filter(item => item.event_id === event.id)"
            :evidence="queryResult.evidence" @open-evidence="openEvidence" />
        </div>
        <div v-if="queryResult.evidence.length" class="result-group">
          <h4>{{ t('knowledgeWorkbench.evidence.title') }}</h4>
          <EvidenceCard v-for="item in queryResult.evidence" :key="item.id" :evidence="item"
            :expanded="expandedEvidenceId === item.id" :loading="evidenceLoadingId === item.id"
            :snapshot="evidenceSnapshots[item.id]" @toggle="openEvidence(item.id)"
            @open-source="emit('open-source', $event)" />
        </div>
      </div>
    </section>

    <section v-else-if="activeSection === 'entities'" class="workbench-panel">
      <div class="panel-heading">
        <div>
          <h3>{{ t('knowledgeWorkbench.entities.title') }}</h3>
          <p>{{ t('knowledgeWorkbench.entities.description') }}</p>
        </div>
        <t-input v-model.trim="entityFilter" class="panel-search" clearable
          :placeholder="t('knowledgeWorkbench.entities.filter')" />
      </div>
      <t-loading :loading="entitiesLoading" show-overlay>
        <div class="entity-layout">
          <div class="entity-list">
            <t-empty v-if="!filteredEntities.length" :description="t('knowledgeWorkbench.entities.empty')" />
            <button v-for="entity in filteredEntities" :key="entity.id" type="button"
              :class="['entity-list__item', { selected: selectedEntity?.id === entity.id }]"
              @click="selectEntity(entity)">
              <span class="entity-list__avatar">{{ entity.canonical_name.slice(0, 1) }}</span>
              <span class="entity-list__copy">
                <strong>{{ entity.canonical_name }}</strong>
                <small>{{ entity.entity_type || t('knowledgeWorkbench.entities.unknownType') }} · {{ entity.aliases?.length || 0 }} {{ t('knowledgeWorkbench.entities.aliases') }}</small>
              </span>
            </button>
            <t-button v-if="hasMoreEntities" block variant="text"
              :loading="entitiesLoading" @click="loadEntities(true)">{{ t('knowledgeWorkbench.loadMore') }}</t-button>
          </div>
          <div class="entity-detail">
            <t-empty v-if="!selectedEntity" :description="t('knowledgeWorkbench.entities.choose')" />
            <template v-else>
              <div class="entity-detail__header">
                <div>
                  <span class="eyebrow">{{ selectedEntity.entity_type || t('knowledgeWorkbench.entities.unknownType') }}</span>
                  <h3>{{ selectedEntity.canonical_name }}</h3>
                  <p v-if="selectedEntity.description">{{ selectedEntity.description }}</p>
                  <div v-if="selectedEntity.aliases?.length" class="chips">
                    <span v-for="alias in selectedEntity.aliases" :key="alias" class="chip">{{ alias }}</span>
                  </div>
                </div>
                <t-button v-if="canManage" variant="outline" @click="openMerge">
                  {{ t('knowledgeWorkbench.entities.merge') }}
                </t-button>
              </div>
              <p class="muted">{{ t('knowledgeWorkbench.scope.detailHint') }}</p>
              <t-loading :loading="entityContextLoading" show-overlay>
                <div class="result-group">
                  <div class="group-heading">
                    <h4>{{ t('knowledgeWorkbench.facts') }}</h4>
                    <t-checkbox v-model="showEntityHistory" @change="loadEntityContext">
                      {{ t('knowledgeWorkbench.entities.includeHistory') }}
                    </t-checkbox>
                  </div>
                  <t-empty v-if="!entityContext?.facts.length" :description="t('knowledgeWorkbench.entities.noFacts')" />
                  <FactCard v-for="fact in entityContext?.facts || []" :key="fact.id" :fact="fact"
                    :facts="entityContext?.facts || []" :entities="entityContext?.entities || [selectedEntity]" :evidence="entityContext?.evidence || []"
                    :can-manage="canManage" @correct="startCorrection" @open-evidence="openEvidence"
                    @open-source="emit('open-source', $event)" />
                </div>
                <div class="result-group" v-if="entityContext?.events.length">
                  <h4>{{ t('knowledgeWorkbench.events.title') }}</h4>
                  <EventCard v-for="event in entityContext.events" :key="event.id" :event="event"
                    :attention="entityContext.attention.filter(item => item.event_id === event.id)"
                    :evidence="entityContext.evidence.filter(item => event.evidence_ids?.includes(item.id))" @open-evidence="openEvidence" />
                </div>
              </t-loading>
            </template>
          </div>
        </div>
      </t-loading>
    </section>

    <section v-else-if="activeSection === 'events'" class="workbench-panel">
      <div class="panel-heading">
        <div>
          <h3>{{ t('knowledgeWorkbench.events.title') }}</h3>
          <p>{{ t('knowledgeWorkbench.events.description') }}</p>
        </div>
      </div>
      <t-loading :loading="eventsLoading" show-overlay>
        <t-empty v-if="!events.length" :description="t('knowledgeWorkbench.events.empty')" />
        <article v-for="event in events" :key="event.id" class="event-row">
          <div class="event-row__main">
            <div class="event-row__title">
              <div>
                <span class="eyebrow">{{ event.event_type || t('knowledgeWorkbench.events.event') }}</span>
                <h4>{{ event.title }}</h4>
              </div>
              <div class="event-heat">
                <strong>{{ formatHeat(event.heat) }}</strong>
                <small>{{ t('knowledgeWorkbench.events.heat') }}</small>
              </div>
            </div>
            <p v-if="event.description" class="event-row__description">{{ event.description }}</p>
            <p v-for="relation in (event.relations || []).filter(item => item.relation === 'same_story')" :key="`${relation.related_event_id}:${relation.evidence_id}`" class="event-row__description">
              {{ t('knowledgeWorkbench.fusion.sameStory') }}：{{ relation.related_title || relation.related_event_id }}
            </p>
            <div class="event-row__meta">
              <span>{{ t('knowledgeWorkbench.events.sourceCount', { count: event.source_count }) }}</span>
              <span>{{ formatDate(event.occurred_at) }}</span>
              <span>{{ t('knowledgeWorkbench.scope.sourceKb') }}: {{ (event.sources || []).map(source => source.knowledge_base_name || source.knowledge_base_id).join('、') || event.knowledge_base_name || event.knowledge_base_id }}</span>
            </div>
            <div v-if="event.entity_ids?.length" class="chips">
              <span v-for="id in event.entity_ids" :key="id" class="chip">{{ entityName(id) }}</span>
            </div>
            <t-button size="small" variant="text" @click="toggleEvent(event.id)">
              {{ expandedEventId === event.id ? t('knowledgeWorkbench.events.hideDetails') : t('knowledgeWorkbench.events.showDetails') }}
            </t-button>
          </div>
          <div v-if="expandedEventId === event.id" class="event-row__details">
            <t-loading :loading="eventLoadingId === event.id" size="small">
              <div class="result-group">
                <h5>{{ t('knowledgeWorkbench.events.whyHot') }}</h5>
                <p v-if="!eventContexts[event.id]?.attention.length" class="muted">{{ t('knowledgeWorkbench.events.noReason') }}</p>
                <div v-for="reason in eventContexts[event.id]?.attention || []" :key="reason.id" class="attention-reason">
                  <strong>{{ reason.reason || t('knowledgeWorkbench.events.scored') }}</strong>
                  <span>{{ t('knowledgeWorkbench.events.attentionScore', { score: formatHeat(reason.score) }) }}</span>
                </div>
              </div>
              <div class="result-group">
                <h5>{{ t('knowledgeWorkbench.evidence.title') }}</h5>
                <EvidenceCard v-for="item in eventContexts[event.id]?.evidence || []" :key="item.id"
                  :evidence="item" :expanded="expandedEvidenceId === item.id"
                  :loading="evidenceLoadingId === item.id" :snapshot="evidenceSnapshots[item.id]"
                  @toggle="openEvidence(item.id)" @open-source="emit('open-source', $event)" />
                <p v-if="!eventContexts[event.id]?.evidence.length" class="muted">{{ t('knowledgeWorkbench.events.noSources') }}</p>
              </div>
            </t-loading>
          </div>
        </article>
        <t-button v-if="hasMoreEvents" block variant="text"
          :loading="eventsLoading" @click="loadEvents(true)">{{ t('knowledgeWorkbench.loadMore') }}</t-button>
      </t-loading>
    </section>

    <section v-else-if="activeSection === 'evidence'" class="workbench-panel">
      <div class="panel-heading">
        <div>
          <h3>{{ t('knowledgeWorkbench.evidence.title') }}</h3>
          <p>{{ t('knowledgeWorkbench.evidence.description') }}</p>
        </div>
        <t-input v-model.trim="evidenceFilter" class="panel-search" clearable
          :placeholder="t('knowledgeWorkbench.evidence.filter')" />
      </div>
      <t-loading :loading="evidenceLoading" show-overlay>
        <t-empty v-if="!filteredEvidence.length" :description="t('knowledgeWorkbench.evidence.empty')" />
        <EvidenceCard v-for="item in filteredEvidence" :key="item.id" :evidence="item"
          :expanded="expandedEvidenceId === item.id" :loading="evidenceLoadingId === item.id"
          :snapshot="evidenceSnapshots[item.id]" @toggle="openEvidence(item.id)"
          @open-source="emit('open-source', $event)" />
        <t-button v-if="hasMoreEvidence" block variant="text"
          :loading="evidenceLoading" @click="loadEvidence(true)">{{ t('knowledgeWorkbench.loadMore') }}</t-button>
      </t-loading>
    </section>

    <section v-else-if="activeSection === 'workflows'" class="workbench-panel">
      <div class="panel-heading">
        <div>
          <h3>{{ t('knowledgeWorkbench.workflows.title') }}</h3>
          <p>{{ t('knowledgeWorkbench.workflows.description') }}</p>
        </div>
        <t-button v-if="canManage" theme="primary" @click="startNewWorkflow">
          <template #icon><t-icon name="add" /></template>
          {{ t('knowledgeWorkbench.workflows.create') }}
        </t-button>
      </div>
      <div v-if="workflowDraft" class="workflow-editor">
        <div class="workflow-editor__header">
          <h4>{{ workflowDraftId ? t('knowledgeWorkbench.workflows.edit') : t('knowledgeWorkbench.workflows.new') }}</h4>
          <t-button variant="text" @click="cancelWorkflowEdit">{{ t('knowledgeWorkbench.workflows.cancel') }}</t-button>
        </div>
        <div v-if="!workflowDraftId" class="template-grid">
          <button v-for="template in workflowTemplates" :key="template.id" type="button"
            :class="['template-card', { selected: selectedTemplate === template.id }]"
            @click="applyTemplate(template.id)">
            <strong>{{ t(`knowledgeWorkbench.workflows.templates.${template.id}.title`) }}</strong>
            <span>{{ t(`knowledgeWorkbench.workflows.templates.${template.id}.description`) }}</span>
          </button>
        </div>
        <div class="workflow-form-grid">
          <label>
            <span>{{ t('knowledgeWorkbench.workflows.name') }}</span>
            <t-input v-model.trim="workflowDraft.name" :placeholder="t('knowledgeWorkbench.workflows.namePlaceholder')" />
          </label>
          <label class="span-two">
            <span>{{ t('knowledgeWorkbench.workflows.descriptionField') }}</span>
            <t-input v-model.trim="workflowDraft.description" />
          </label>
          <label>
            <span>{{ t('knowledgeWorkbench.workflows.cron') }}</span>
            <t-input v-model.trim="workflowDraft.schedule.cron" :placeholder="t('knowledgeWorkbench.workflows.manualOnly')" />
          </label>
          <label>
            <span>{{ t('knowledgeWorkbench.workflows.timezone') }}</span>
            <t-input v-model.trim="workflowDraft.schedule.timezone" placeholder="Asia/Shanghai" />
          </label>
          <label class="enable-toggle">
            <t-checkbox v-model="workflowDraft.enabled">{{ t('knowledgeWorkbench.workflows.enabled') }}</t-checkbox>
          </label>
        </div>
        <div class="workflow-steps-heading">
          <div>
            <h5>{{ t('knowledgeWorkbench.workflows.steps') }}</h5>
            <p>{{ t('knowledgeWorkbench.workflows.stepsHint') }}</p>
          </div>
          <t-select v-model="stepTypeToAdd" :options="stepTypeOptions" class="add-step-select" />
          <t-button variant="outline" @click="addWorkflowStep">{{ t('knowledgeWorkbench.workflows.addStep') }}</t-button>
        </div>
        <div class="workflow-step-list">
          <article v-for="(step, index) in workflowDraft.steps" :key="step.id" class="workflow-step">
            <div class="workflow-step__top">
              <span class="step-number">{{ index + 1 }}</span>
              <t-input v-model.trim="step.id" :placeholder="t('knowledgeWorkbench.workflows.stepId')" />
              <t-select v-model="step.type" :options="stepTypeOptions" />
              <div class="step-order">
                <t-button size="small" variant="text" :disabled="index === 0" @click="moveWorkflowStep(index, -1)">↑</t-button>
                <t-button size="small" variant="text" :disabled="index === workflowDraft.steps.length - 1" @click="moveWorkflowStep(index, 1)">↓</t-button>
                <t-button size="small" variant="text" theme="danger" @click="removeWorkflowStep(index)">{{ t('knowledgeWorkbench.workflows.remove') }}</t-button>
              </div>
            </div>
            <label class="workflow-step__depends">
              <span>{{ t('knowledgeWorkbench.workflows.dependsOn') }}</span>
              <t-select v-model="step.depends_on" multiple clearable :options="dependencyOptions(step)" />
            </label>
            <div class="step-config">
              <template v-if="step.type === 'search'">
                <label class="span-two"><span>{{ t('knowledgeWorkbench.workflows.config.query') }}</span><t-input v-model="step.config.query" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.limit') }}</span><t-input-number v-model="step.config.limit" :min="1" :max="100" /></label>
              </template>
              <template v-else-if="step.type === 'fact_query'">
                <label><span>{{ t('knowledgeWorkbench.scope.label') }}</span><t-select v-model="step.config.scope" :options="scopeOptions" /></label>
                <label v-if="step.config.scope === 'domain'"><span>{{ t('knowledgeDomains.title') }}</span><t-select v-model="step.config.domain_id" filterable :options="domainOptions" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.entity') }}</span><t-input v-model="step.config.entity" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.predicate') }}</span><t-input v-model="step.config.predicate" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.relation') }}</span><t-input v-model="step.config.relation" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.objectEntityId') }}</span><t-input v-model="step.config.object_entity_id" /></label>
                <label><span>{{ t('knowledgeWorkbench.fusion.minValue') }}</span><t-input-number v-model="step.config.min_value" /></label>
                <label><span>{{ t('knowledgeWorkbench.fusion.maxValue') }}</span><t-input-number v-model="step.config.max_value" /></label>
                <label><span>{{ t('knowledgeWorkbench.fusion.unit') }}</span><t-input v-model="step.config.unit" placeholder="tokens" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.since') }}</span><input v-model="step.config.since" class="native-input" type="date" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.until') }}</span><input v-model="step.config.until" class="native-input" type="date" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.limit') }}</span><t-input-number v-model="step.config.limit" :min="1" :max="100" /></label>
                <label class="enable-toggle"><t-checkbox v-model="step.config.include_history">{{ t('knowledgeWorkbench.workflows.config.includeHistory') }}</t-checkbox></label>
              </template>
              <template v-else-if="step.type === 'web_research'">
                <label class="span-two"><span>{{ t('knowledgeWorkbench.workflows.config.query') }}</span><t-input v-model="step.config.query" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.maxResults') }}</span><t-input-number v-model="step.config.max_results" :min="1" :max="50" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.provider') }}</span><t-input v-model="step.config.provider_id" /></label>
                <label class="enable-toggle"><t-checkbox :checked="true" disabled>{{ t('knowledgeWorkbench.workflows.config.ingest') }}</t-checkbox></label>
              </template>
              <template v-else-if="step.type === 'report'">
                <label><span>{{ t('knowledgeWorkbench.workflows.config.reportTitle') }}</span><t-input v-model="step.config.title" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.timeWindow') }}</span><t-select :value="step.config.window" clearable :options="reportWindowOptions" @change="setReportWindow(step, $event)" /></label>
                <label v-if="step.config.window === 'rolling_days'"><span>{{ t('knowledgeWorkbench.workflows.config.days') }}</span><t-input-number v-model="step.config.days" :min="1" :max="365" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.timezone') }}</span><t-input v-model="step.config.timezone" placeholder="Asia/Shanghai" /></label>
                <label v-if="!step.config.window"><span>{{ t('knowledgeWorkbench.workflows.config.timeFrom') }}</span><input v-model="step.config.time_from" class="native-input" type="date" /></label>
                <label v-if="!step.config.window"><span>{{ t('knowledgeWorkbench.workflows.config.timeTo') }}</span><input v-model="step.config.time_to" class="native-input" type="date" /></label>
                <label class="span-two"><span>{{ t('knowledgeWorkbench.workflows.config.prompt') }}</span><t-textarea v-model="step.config.prompt" :autosize="{ minRows: 2, maxRows: 5 }" /></label>
              </template>
              <template v-else-if="step.type === 'wiki_update'">
                <label><span>{{ t('knowledgeWorkbench.workflows.config.slug') }}</span><t-input v-model="step.config.slug" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.pageTitle') }}</span><t-input v-model="step.config.title" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.pageType') }}</span><t-input v-model="step.config.page_type" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.sourceStep') }}</span><t-select v-model="step.config.source_step" clearable :options="dependencyOptions(step)" /></label>
                <label class="span-two"><span>{{ t('knowledgeWorkbench.workflows.config.summary') }}</span><t-textarea v-model="step.config.summary" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
              </template>
              <template v-else-if="step.type === 'skill'">
                <label><span>{{ t('knowledgeWorkbench.workflows.config.agentId') }}</span><t-input v-model="step.config.agent_id" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.skillName') }}</span><t-input v-model="step.config.skill_name" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.scriptPath') }}</span><t-input v-model="step.config.script_path" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.inputStep') }}</span><t-select v-model="step.config.input_step" clearable :options="dependencyOptions(step)" /></label>
                <label class="span-two"><span>{{ t('knowledgeWorkbench.workflows.config.input') }}</span><t-textarea v-model="step.config.input" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
                <label class="span-two"><span>{{ t('knowledgeWorkbench.workflows.config.args') }}</span><t-textarea v-model="step.config.args_text" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
              </template>
              <template v-else-if="step.type === 'condition'">
                <label><span>{{ t('knowledgeWorkbench.workflows.config.conditionStep') }}</span><t-select v-model="step.config.step_id" :options="dependencyOptions(step)" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.path') }}</span><t-input v-model="step.config.path" /></label>
                <label><span>{{ t('knowledgeWorkbench.workflows.config.operator') }}</span><t-select v-model="step.config.operator" :options="conditionOperatorOptions" /></label>
                <label>
                  <span>{{ t('knowledgeWorkbench.workflows.config.value') }}</span>
                  <t-input-number v-if="step.config.operator === 'gt' || step.config.operator === 'gte'" v-model="step.config.value" />
                  <t-input v-else v-model="step.config.value" />
                </label>
              </template>
              <template v-else-if="step.type === 'human_review'">
                <label class="span-two"><span>{{ t('knowledgeWorkbench.workflows.config.reviewMessage') }}</span><t-textarea v-model="step.config.message" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
              </template>
              <label class="step-condition span-two">
                <t-checkbox :checked="!!step.when" @change="toggleStepCondition(step)">{{ t('knowledgeWorkbench.workflows.onlyWhen') }}</t-checkbox>
                <template v-if="step.when">
                  <t-select :value="step.when.step_id" :options="conditionOptions(step)" @change="setConditionStep(step, String($event))" />
                  <t-select :value="String(step.when.equals)" :options="booleanOptions" @change="setConditionValue(step, $event)" />
                </template>
              </label>
            </div>
          </article>
        </div>
        <div v-if="workflowEditorError" class="form-error">{{ workflowEditorError }}</div>
        <div class="workflow-editor__actions">
          <t-button variant="outline" @click="cancelWorkflowEdit">{{ t('knowledgeWorkbench.workflows.cancel') }}</t-button>
          <t-button theme="primary" :loading="workflowSaving" @click="saveWorkflow">{{ t('knowledgeWorkbench.workflows.save') }}</t-button>
        </div>
      </div>

      <t-loading :loading="workflowsLoading" show-overlay>
        <t-empty v-if="!workflows.length && !workflowDraft" :description="t('knowledgeWorkbench.workflows.empty')" />
        <div class="workflow-layout">
          <div class="workflow-list">
            <article v-for="workflow in workflows" :key="workflow.id"
              :class="['workflow-list__item', { selected: selectedWorkflow?.id === workflow.id }]"
              @click="selectWorkflow(workflow)">
              <div class="workflow-list__heading">
                <strong>{{ workflow.name }}</strong>
                <t-tag :theme="workflow.enabled ? 'success' : 'default'" size="small">
                  {{ workflow.enabled ? t('knowledgeWorkbench.workflows.enabled') : t('knowledgeWorkbench.workflows.disabled') }}
                </t-tag>
              </div>
              <p>{{ workflow.description || t('knowledgeWorkbench.workflows.noDescription') }}</p>
              <small>{{ workflow.schedule.cron || t('knowledgeWorkbench.workflows.manualOnly') }} · {{ workflow.schedule.timezone || 'UTC' }}</small>
              <div class="workflow-list__actions">
                <t-button v-if="canManage" size="small" theme="primary" variant="outline" :loading="runningWorkflowId === workflow.id"
                  @click.stop="runWorkflow(workflow)">{{ t('knowledgeWorkbench.workflows.run') }}</t-button>
                <t-button v-if="canManage" size="small" variant="text" @click.stop="editWorkflow(workflow)">{{ t('knowledgeWorkbench.workflows.edit') }}</t-button>
                <t-button v-if="canManage" size="small" variant="text" @click.stop="toggleWorkflow(workflow)">
                  {{ workflow.enabled ? t('knowledgeWorkbench.workflows.disable') : t('knowledgeWorkbench.workflows.enable') }}
                </t-button>
              </div>
            </article>
          </div>
          <div class="workflow-runs">
            <div class="group-heading">
              <div>
                <h4>{{ t('knowledgeWorkbench.workflows.runs') }}</h4>
                <p v-if="selectedWorkflow">{{ selectedWorkflow.name }}</p>
              </div>
              <t-button size="small" variant="text" :disabled="!selectedWorkflow" :loading="runsLoading"
                @click="loadRuns">{{ t('knowledgeWorkbench.refresh') }}</t-button>
            </div>
            <t-empty v-if="!selectedWorkflow" :description="t('knowledgeWorkbench.workflows.selectWorkflow')" />
            <t-empty v-else-if="!workflowRuns.length && !runsLoading" :description="t('knowledgeWorkbench.workflows.noRuns')" />
            <button v-for="run in workflowRuns" :key="run.id" type="button"
              :class="['run-list__item', { selected: selectedRun?.id === run.id }]"
              @click="selectRun(run)">
              <span class="status-dot" :class="`status-${run.status}`"></span>
              <span class="run-list__copy">
                <strong>{{ t(`knowledgeWorkbench.workflows.runStatus.${run.status}`) }}</strong>
                <small>{{ formatDate(run.created_at) }} · {{ run.current_step_id || run.trigger }}</small>
              </span>
            </button>
            <div v-if="selectedRun" class="run-detail">
              <div class="run-detail__heading">
                <h5>{{ t('knowledgeWorkbench.workflows.runDetail') }}</h5>
                <t-button v-if="canRetryRun" size="small" variant="outline"
                  :loading="retryingRun" @click="retryRun">
                  {{ t('knowledgeWorkbench.workflows.retry') }}
                </t-button>
                <t-button v-if="canResumeRun" size="small" variant="outline"
                  :loading="resumingRun" @click="resumeRun">
                  {{ t('knowledgeWorkbench.workflows.resume') }}
                </t-button>
              </div>
              <p v-if="selectedRun.error" class="run-error">{{ selectedRun.error }}</p>
              <div v-for="stepRun in selectedRun.steps || []" :key="stepRun.id" class="run-step">
                <div class="run-step__header">
                  <strong>{{ workflowStepName(stepRun.step_id) || stepRun.step_id }}</strong>
                  <t-tag :theme="stepStatusTheme(stepRun.status)" size="small">{{ t(`knowledgeWorkbench.workflows.stepStatus.${stepRun.status}`) }}</t-tag>
                  <small v-if="stepRun.attempt">{{ t('knowledgeWorkbench.workflows.attempt', { count: stepRun.attempt }) }}</small>
                </div>
                <p v-if="stepRun.error" class="run-error">{{ stepRun.error }}</p>
                <pre v-if="stepRun.output !== undefined" class="run-output">{{ formatOutput(stepRun.output) }}</pre>
                <div v-if="stepRun.step_type === 'skill' && stepArtifacts(stepRun).length" class="run-artifacts">
                  <t-button v-for="artifact in stepArtifacts(stepRun)" :key="artifact.key" size="small" variant="outline"
                    :loading="downloadingArtifactKey === artifact.key" @click="downloadStepArtifact(artifact)">
                    <template #icon><t-icon name="download" /></template>
                    {{ artifact.file_name }}
                  </t-button>
                </div>
              </div>
              <div v-if="selectedRun.status === 'paused' && canManage" class="review-actions">
                <span>{{ t('knowledgeWorkbench.workflows.reviewPending') }}</span>
                <div>
                  <t-button size="small" theme="primary" :loading="decisionLoading" @click="decideRun('approve')">{{ t('knowledgeWorkbench.workflows.approve') }}</t-button>
                  <t-button size="small" theme="danger" variant="outline" :loading="decisionLoading" @click="decideRun('reject')">{{ t('knowledgeWorkbench.workflows.reject') }}</t-button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </t-loading>
    </section>

    <section v-else-if="activeSection === 'reports'" class="workbench-panel">
      <div class="panel-heading">
        <div>
          <h3>{{ t('knowledgeWorkbench.reports.title') }}</h3>
          <p>{{ t('knowledgeWorkbench.reports.description') }}</p>
        </div>
        <t-button variant="outline" :loading="reportsLoading" @click="loadReports">{{ t('knowledgeWorkbench.refresh') }}</t-button>
      </div>
      <t-loading :loading="reportsLoading" show-overlay>
        <t-empty v-if="!reports.length" :description="t('knowledgeWorkbench.reports.empty')" />
        <div class="report-layout">
          <div class="report-list">
            <button v-for="report in reports" :key="report.id" type="button"
              :class="['report-list__item', { selected: selectedReport?.id === report.id }]"
              @click="selectReport(report)">
              <strong>{{ report.title }}</strong>
              <small>{{ formatDate(report.created_at) }}</small>
            </button>
          </div>
          <article v-if="selectedReport" class="report-view">
            <header>
              <div>
                <h4>{{ selectedReport.title }}</h4>
                <p>{{ selectedReport.time_from ? formatDate(selectedReport.time_from) : '' }}
                  <template v-if="selectedReport.time_to">— {{ formatDate(selectedReport.time_to) }}</template>
                  <span v-if="!selectedReport.time_from && !selectedReport.time_to">{{ formatDate(selectedReport.created_at) }}</span>
                </p>
              </div>
              <t-button variant="outline" :loading="reportDownloading" @click="downloadReport(selectedReport)">
                <template #icon><t-icon name="download" /></template>
                {{ t('knowledgeWorkbench.reports.download') }}
              </t-button>
            </header>
            <div class="report-markdown" v-html="safeMarkdownToHTML(selectedReport.markdown)" />
            <div class="report-citations">
              <h5>{{ t('knowledgeWorkbench.reports.citations') }}</h5>
              <t-empty v-if="!selectedReport.citations?.length" :description="t('knowledgeWorkbench.reports.noCitations')" />
              <article v-for="citation in selectedReport.citations || []" :key="citation.id" class="citation-card">
                <strong>{{ citation.title || citation.kind }}</strong>
                <small v-if="citation.role">{{ t(`knowledgeWorkbench.fusion.${citation.role}`) }}</small>
                <p v-if="citation.quote">“{{ citation.quote }}”</p>
                <small>{{ citation.published_at ? formatDate(citation.published_at) : '' }}</small>
                <div>
                  <t-button v-if="citation.evidence_id" size="small" variant="text" @click="openEvidence(citation.evidence_id, citation.quote)">
                    {{ t('knowledgeWorkbench.evidence.openSnapshot') }}
                  </t-button>
                  <a v-if="safeExternalUrl(citation.url)" :href="safeExternalUrl(citation.url)" target="_blank" rel="noopener noreferrer">{{ t('knowledgeWorkbench.evidence.openSource') }}</a>
                </div>
              </article>
            </div>
          </article>
        </div>
      </t-loading>
    </section>

    <t-drawer v-model:visible="snapshotVisible" :header="t('knowledgeWorkbench.fusion.snapshotTitle')" size="min(680px, 95vw)" :footer="false">
      <t-loading :loading="snapshotLoading" show-overlay>
        <p v-if="snapshotError" class="run-error" role="alert">{{ snapshotError }}</p>
        <div v-if="selectedSnapshot">
          <h3>{{ selectedSnapshot.title }}</h3>
          <p>v{{ selectedSnapshot.version }} · {{ t(`knowledgeWorkbench.evidence.status.${selectedSnapshot.status}`) }}</p>
          <p class="snapshot-hash">{{ t('knowledgeWorkbench.evidence.hash') }}: {{ selectedSnapshot.body_hash }}</p>
          <blockquote v-if="selectedQuote">{{ selectedQuote }}</blockquote>
          <p v-if="selectedQuote && !snapshotSegments.some(segment => segment.match)" role="status">
            {{ t('knowledgeWorkbench.fusion.quoteMissing') }}
          </p>
          <pre ref="snapshotBody" class="evidence-snapshot__body"><template v-for="(segment, index) in snapshotSegments" :key="index"><mark v-if="segment.match">{{ segment.text }}</mark><template v-else>{{ segment.text }}</template></template></pre>
        </div>
      </t-loading>
    </t-drawer>

    <t-drawer v-model:visible="correctionVisible" :header="t('knowledgeWorkbench.correctionTitle')" size="520px" :footer="false">
      <div v-if="editingFact" class="drawer-form">
        <p>{{ t('knowledgeWorkbench.correctionIntro', { predicate: editingFact.fact.predicate }) }}</p>
        <label><span>{{ t('knowledgeWorkbench.correctionValue') }}</span><t-textarea v-model="editingFact.value" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
        <label><span>{{ t('knowledgeWorkbench.correctionValueType') }}</span><t-select v-model="editingFact.valueType" :options="valueTypeOptions" /></label>
        <label><span>{{ t('knowledgeWorkbench.correctionUnit') }}</span><t-input v-model.trim="editingFact.unit" /></label>
        <label><span>{{ t('knowledgeWorkbench.correctionQuote') }}</span><t-textarea v-model="editingFact.quote" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
        <label><span>{{ t('knowledgeWorkbench.correctionNote') }}</span><t-textarea v-model="editingFact.correctionNote" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
        <label><span>{{ t('knowledgeWorkbench.correctionFrom') }}</span><input v-model="editingFact.effectiveFrom" class="native-input" type="date" /></label>
        <label><span>{{ t('knowledgeWorkbench.correctionTo') }}</span><input v-model="editingFact.effectiveTo" class="native-input" type="date" /></label>
        <div class="drawer-form__actions">
          <t-button variant="outline" @click="closeCorrection">{{ t('knowledgeWorkbench.cancel') }}</t-button>
          <t-button theme="primary" :loading="correcting" :disabled="!editingFact.value.trim() || !editingFact.quote.trim() || !editingFact.correctionNote.trim()" @click="saveCorrection">{{ t('knowledgeWorkbench.correct') }}</t-button>
        </div>
      </div>
    </t-drawer>
    <t-drawer v-model:visible="mergeVisible" :header="t('knowledgeWorkbench.entities.mergeTitle')" size="420px" :footer="false">
      <div class="drawer-form">
        <p>{{ t('knowledgeWorkbench.entities.mergeHint') }}</p>
        <label><span>{{ t('knowledgeWorkbench.entities.mergeSource') }}</span><t-input :value="selectedEntity?.canonical_name || ''" disabled /></label>
        <label><span>{{ t('knowledgeWorkbench.entities.mergeTarget') }}</span>
          <t-select v-model="mergeTargetId" filterable :options="mergeTargetOptions" :placeholder="t('knowledgeWorkbench.entities.mergeTargetPlaceholder')" />
        </label>
        <t-alert theme="warning" :message="t('knowledgeWorkbench.entities.mergeWarning')" />
        <div class="drawer-form__actions">
          <t-button variant="outline" @click="mergeVisible = false">{{ t('knowledgeWorkbench.cancel') }}</t-button>
          <t-button theme="primary" :disabled="!mergeTargetId" :loading="merging" @click="mergeEntities">{{ t('knowledgeWorkbench.entities.merge') }}</t-button>
        </div>
      </div>
    </t-drawer>
  </section>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { listKnowledgeDomains, type KnowledgeDomain } from '@/api/knowledge-domains'
import { knowledgeContextScopeParams } from '@/api/knowledge-context-scope'
import { safeMarkdownToHTML } from '@/utils/security'
import { downloadArtifact, type ArtifactMeta } from '@/api/chat/index'
import {
  correctGlobalKnowledgeFact,
  getGlobalKnowledgeEvidence,
  getGlobalKnowledgeEntityContext,
  mergeGlobalKnowledgeEntities,
  rebuildKnowledgeContext,
  listKnowledgeContextEntities,
  listKnowledgeContextEvents,
  listKnowledgeContextEvidence,
  queryKnowledgeContext,
  type KnowledgeAttention,
  type KnowledgeContextQuery,
  type KnowledgeContextScope,
  type KnowledgeContextResult,
  type KnowledgeEntity,
  type KnowledgeEvidence,
  type KnowledgeEvent,
  type KnowledgeFact,
} from '@/api/knowledge-context'
import {
  createKnowledgeWorkflow,
  decideKnowledgeWorkflowRun,
  downloadKnowledgeWorkflowReport,
  getKnowledgeWorkflowReport,
  getKnowledgeWorkflowRun,
  listKnowledgeWorkflowReports,
  listKnowledgeWorkflowRuns,
  listKnowledgeWorkflows,
  retryKnowledgeWorkflowRun,
  resumeKnowledgeWorkflowRun,
  runKnowledgeWorkflow,
  updateKnowledgeWorkflow,
  type KnowledgeWorkflow,
  type KnowledgeWorkflowInput,
  type KnowledgeWorkflowReport,
  type KnowledgeWorkflowRun,
  type KnowledgeWorkflowStep,
  type KnowledgeWorkflowStepRun,
  type KnowledgeWorkflowStepType,
} from '@/api/knowledge-workflows'

const props = defineProps<{ kbId: string; domainId?: string; canManage: boolean }>()
const emit = defineEmits<{ 'open-source': [knowledgeId: string] }>()
const { t } = useI18n()
const pageSize = 50
const queryScope = ref<KnowledgeContextScope>('knowledge_base')
const queryDomainId = ref(props.domainId || '')
const domains = ref<KnowledgeDomain[]>([])
const domainsLoading = ref(false)
const domainLoadError = ref('')
const scopeOptions = computed(() => ['knowledge_base', 'domain', 'team'].map(value => ({ value, label: t(`knowledgeWorkbench.scope.${value}`) })))
const domainOptions = computed(() => domains.value.map(domain => ({ value: domain.id, label: domain.name })))
const scopeParams = computed(() => knowledgeContextScopeParams(queryScope.value, props.kbId, queryDomainId.value))
let contextGeneration = 0
async function loadDomains() {
  domainsLoading.value = true
  domainLoadError.value = ''
  try { const response = await listKnowledgeDomains(); if (!response.success) throw new Error(response.error || response.message || t('knowledgeDomains.loadFailed')); domains.value = response.data || [] }
  catch (error) { domainLoadError.value = getErrorMessage(error) }
  finally { domainsLoading.value = false }
}

type SectionId = 'query' | 'entities' | 'events' | 'evidence' | 'workflows' | 'reports'
const sections: Array<{ id: SectionId }> = [
  { id: 'query' }, { id: 'entities' }, { id: 'events' }, { id: 'evidence' }, { id: 'workflows' }, { id: 'reports' },
]
const activeSection = ref<SectionId>('query')
const loading = ref(false)
const loadError = ref('')
const entitiesLoading = ref(false)
const eventsLoading = ref(false)
const evidenceLoading = ref(false)
const entities = ref<KnowledgeEntity[]>([])
const events = ref<KnowledgeEvent[]>([])
const evidence = ref<KnowledgeEvidence[]>([])
const entityOffset = ref(0)
const eventOffset = ref(0)
const evidenceOffset = ref(0)
const hasMoreEntities = ref(false)
const hasMoreEvents = ref(false)
const hasMoreEvidence = ref(false)
const entityFilter = ref('')
const evidenceFilter = ref('')
const selectedEntity = ref<KnowledgeEntity | null>(null)
const entityContext = ref<KnowledgeContextResult | null>(null)
const entityContextLoading = ref(false)
const showEntityHistory = ref(true)
const queryLoading = ref(false)
const queryResult = ref<KnowledgeContextResult | null>(null)
const queryForm = reactive({ entity: '', predicate: '', relation: '', since: '', until: '', include_history: true, min_value: '', max_value: '', unit: '' })
const expandedEventId = ref('')
const eventLoadingId = ref('')
const eventContexts = ref<Record<string, KnowledgeContextResult>>({})
const expandedEvidenceId = ref('')
const evidenceLoadingId = ref('')
const evidenceSnapshots = reactive<Record<string, KnowledgeEvidence>>({})
const snapshotVisible = ref(false)
const snapshotLoading = ref(false)
const snapshotError = ref('')
const selectedSnapshot = ref<KnowledgeEvidence | null>(null)
const selectedQuote = ref('')
const snapshotBody = ref<HTMLElement | null>(null)
let snapshotRequest = 0
const snapshotSegments = computed(() => {
  const body = selectedSnapshot.value?.body || ''
  const quoted = selectedQuote.value
  // Report excerpts carry a trailing ellipsis. Match their exact prefix only;
  // never use fuzzy matching to guess a source location.
  const quote = body.includes(quoted) ? quoted : quoted.endsWith('…') ? quoted.slice(0, -1) : quoted
  const segments: Array<{ text: string; match: boolean }> = []
  let offset = 0
  let start = quote ? body.indexOf(quote) : -1
  while (start >= 0 && segments.length < 100) {
    if (start > offset) segments.push({ text: body.slice(offset, start), match: false })
    segments.push({ text: quote, match: true })
    offset = start + quote.length
    start = body.indexOf(quote, offset)
  }
  if (offset < body.length) segments.push({ text: body.slice(offset), match: false })
  return segments
})
const mergeVisible = ref(false)
const mergeTargetId = ref('')
const merging = ref(false)
const correctionVisible = ref(false)
const correcting = ref(false)
const rebuilding = ref(false)

const workflows = ref<KnowledgeWorkflow[]>([])
const workflowsLoading = ref(false)
const selectedWorkflow = ref<KnowledgeWorkflow | null>(null)
const workflowRuns = ref<KnowledgeWorkflowRun[]>([])
const runsLoading = ref(false)
const selectedRun = ref<KnowledgeWorkflowRun | null>(null)
const runningWorkflowId = ref('')
const decisionLoading = ref(false)
const resumingRun = ref(false)
const retryingRun = ref(false)
const downloadingArtifactKey = ref('')
const reports = ref<KnowledgeWorkflowReport[]>([])
const reportsLoading = ref(false)
const selectedReport = ref<KnowledgeWorkflowReport | null>(null)
const reportDownloading = ref(false)
const workflowDraftId = ref('')
const workflowDraft = ref<KnowledgeWorkflowInput | null>(null)
const workflowSaving = ref(false)
const workflowEditorError = ref('')
const selectedTemplate = ref('daily')
const stepTypeToAdd = ref<KnowledgeWorkflowStepType>('search')
let runPollTimer: ReturnType<typeof setInterval> | null = null

const workflowTemplates = [
  { id: 'daily' }, { id: 'weekly' }, { id: 'research' }, { id: 'custom' },
]
const stepTypes: KnowledgeWorkflowStepType[] = [
  'search', 'fact_query', 'web_research', 'report', 'wiki_update', 'skill', 'condition', 'human_review',
]
const stepTypeOptions = computed(() => stepTypes.map(value => ({
  value,
  label: t(`knowledgeWorkbench.workflows.stepTypes.${value}`),
})))
const reportWindowOptions = computed(() => [
  { value: 'previous_day', label: t('knowledgeWorkbench.workflows.config.previousDay') },
  { value: 'previous_week', label: t('knowledgeWorkbench.workflows.config.previousWeek') },
  { value: 'rolling_days', label: t('knowledgeWorkbench.workflows.config.rollingDays') },
])
const conditionOperatorOptions = ['exists', 'equals', 'not_equals', 'contains', 'gt', 'gte']
  .map(value => ({ value, label: value }))
const canRetryRun = computed(() => Boolean(
  props.canManage && selectedRun.value?.status === 'failed' && selectedWorkflow.value?.enabled,
))
const canResumeRun = computed(() => Boolean(
  props.canManage && selectedRun.value?.status === 'paused' && selectedWorkflow.value?.enabled
  && !selectedRun.value.steps?.some(step => step.status === 'waiting_review'),
))
const booleanOptions = computed(() => [
  { value: 'true', label: t('common.yes') },
  { value: 'false', label: t('common.no') },
])
const valueTypeOptions = ['string', 'number', 'boolean', 'date', 'datetime', 'json', 'entity']
  .map(value => ({ value, label: value }))
const filteredEntities = computed(() => {
  const q = entityFilter.value.trim().toLocaleLowerCase()
  if (!q) return entities.value
  return entities.value.filter(entity => [entity.canonical_name, entity.entity_type, entity.description, ...(entity.aliases || [])]
    .some(value => value?.toLocaleLowerCase().includes(q)))
})
const filteredEvidence = computed(() => {
  const q = evidenceFilter.value.trim().toLocaleLowerCase()
  if (!q) return evidence.value
  return evidence.value.filter(item => [item.title, item.author, item.publisher, item.original_url]
    .some(value => value?.toLocaleLowerCase().includes(q)))
})
const mergeTargetOptions = computed(() => entities.value
  .filter(entity => entity.id !== selectedEntity.value?.id && !entity.merged_into_id)
  .map(entity => ({ value: entity.id, label: `${entity.canonical_name} · ${entity.entity_type || t('knowledgeWorkbench.entities.unknownType')}` })))

function envelopeData<T>(response: { success: boolean; data: T; error?: string }): T {
  if (!response?.success) throw new Error(response?.error || t('knowledgeWorkbench.requestFailed'))
  return response.data
}

function getErrorMessage(error: unknown): string {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') return error.message
  return t('knowledgeWorkbench.requestFailed')
}

async function refreshAll() {
  if (!props.kbId || (queryScope.value === 'domain' && !queryDomainId.value)) return
  const generation = contextGeneration
  loading.value = true
  loadError.value = ''
  try {
    const [entityResponse, eventResponse, evidenceResponse] = await Promise.all([
      listKnowledgeContextEntities(props.kbId, { ...scopeParams.value, limit: pageSize, offset: 0 }),
      listKnowledgeContextEvents(props.kbId, { ...scopeParams.value, limit: pageSize, offset: 0 }),
      listKnowledgeContextEvidence(props.kbId, { ...scopeParams.value, limit: pageSize, offset: 0 }),
    ])
    if (generation !== contextGeneration) return
    entities.value = envelopeData(entityResponse) || []
    events.value = envelopeData(eventResponse) || []
    evidence.value = envelopeData(evidenceResponse) || []
    entityOffset.value = entities.value.length
    eventOffset.value = events.value.length
    evidenceOffset.value = evidence.value.length
    hasMoreEntities.value = entities.value.length === pageSize
    hasMoreEvents.value = events.value.length === pageSize
    hasMoreEvidence.value = evidence.value.length === pageSize
    if (selectedEntity.value) {
      const current = entities.value.find(item => item.id === selectedEntity.value?.id)
      selectedEntity.value = current || null
      if (current) await loadEntityContext()
    }
  } catch (error) {
    if (generation === contextGeneration) loadError.value = getErrorMessage(error)
  } finally {
    if (generation === contextGeneration) loading.value = false
  }
}

async function loadEntities(loadMore = false) {
  if (queryScope.value === 'domain' && !queryDomainId.value) return
  const generation = contextGeneration
  entitiesLoading.value = true
  try {
    const offset = loadMore ? entityOffset.value : 0
    const items = envelopeData(await listKnowledgeContextEntities(props.kbId, { ...scopeParams.value, limit: pageSize, offset })) || []
    if (generation !== contextGeneration) return
    entities.value = loadMore ? [...entities.value, ...items] : items
    entityOffset.value = offset + items.length
    hasMoreEntities.value = items.length === pageSize
  } catch (error) {
    if (generation === contextGeneration) loadError.value = getErrorMessage(error)
  } finally { if (generation === contextGeneration) entitiesLoading.value = false }
}

async function loadEvents(loadMore = false) {
  if (queryScope.value === 'domain' && !queryDomainId.value) return
  const generation = contextGeneration
  eventsLoading.value = true
  try {
    const offset = loadMore ? eventOffset.value : 0
    const items = envelopeData(await listKnowledgeContextEvents(props.kbId, { ...scopeParams.value, limit: pageSize, offset })) || []
    if (generation !== contextGeneration) return
    events.value = loadMore ? [...events.value, ...items] : items
    eventOffset.value = offset + items.length
    hasMoreEvents.value = items.length === pageSize
  }
  catch (error) { if (generation === contextGeneration) loadError.value = getErrorMessage(error) }
  finally { if (generation === contextGeneration) eventsLoading.value = false }
}

async function loadEvidence(loadMore = false) {
  if (queryScope.value === 'domain' && !queryDomainId.value) return
  const generation = contextGeneration
  evidenceLoading.value = true
  try {
    const offset = loadMore ? evidenceOffset.value : 0
    const items = envelopeData(await listKnowledgeContextEvidence(props.kbId, { ...scopeParams.value, limit: pageSize, offset })) || []
    if (generation !== contextGeneration) return
    evidence.value = loadMore ? [...evidence.value, ...items] : items
    evidenceOffset.value = offset + items.length
    hasMoreEvidence.value = items.length === pageSize
  }
  catch (error) { if (generation === contextGeneration) loadError.value = getErrorMessage(error) }
  finally { if (generation === contextGeneration) evidenceLoading.value = false }
}

async function selectSection(section: SectionId) {
  activeSection.value = section
  loadError.value = ''
  if (section === 'entities' && !entities.value.length) await loadEntities()
  if (section === 'events' && !events.value.length) await loadEvents()
  if (section === 'evidence' && !evidence.value.length) await loadEvidence()
  if (section === 'workflows' && !workflows.value.length) await loadWorkflows()
  if (section === 'reports' && !reports.value.length) await loadReports()
}

function makeQuery(extra: Partial<KnowledgeContextQuery> = {}): KnowledgeContextQuery {
  const query: KnowledgeContextQuery = {
    ...scopeParams.value,
    include_history: true,
    limit: 100,
    ...extra,
  }
  return query
}

function onQueryEnter(_value: unknown, context: { e: KeyboardEvent }) {
  if (!context.e.isComposing) void runQuery()
}

async function runQuery() {
  if (queryLoading.value || (queryScope.value === 'domain' && !queryDomainId.value)) return
  const generation = contextGeneration
  queryLoading.value = true
  loadError.value = ''
  try {
    const response = await queryKnowledgeContext(props.kbId, makeQuery({
      entity: queryForm.entity || undefined,
      predicate: queryForm.predicate || undefined,
      relation: queryForm.relation || undefined,
      since: dateToISO(queryForm.since),
      until: dateToISO(queryForm.until),
      include_history: queryForm.include_history,
      min_value: queryForm.min_value === '' ? undefined : Number(queryForm.min_value),
      max_value: queryForm.max_value === '' ? undefined : Number(queryForm.max_value),
      unit: queryForm.unit || undefined,
    }))
    if (generation === contextGeneration) queryResult.value = envelopeData(response)
  } catch (error) { if (generation === contextGeneration) loadError.value = getErrorMessage(error) }
  finally { if (generation === contextGeneration) queryLoading.value = false }
}

function clearQuery() {
  queryForm.min_value = ''
  queryForm.max_value = ''
  queryForm.unit = ''
  queryForm.entity = ''
  queryForm.predicate = ''
  queryForm.relation = ''
  queryForm.since = ''
  queryForm.until = ''
  queryForm.include_history = true
  queryResult.value = null
}

async function selectEntity(entity: KnowledgeEntity) {
  selectedEntity.value = entity
  showEntityHistory.value = true
  await loadEntityContext()
}

async function loadEntityContext() {
  if (!selectedEntity.value) return
  const generation = contextGeneration
  const entityId = selectedEntity.value.id
  entityContext.value = null
  entityContextLoading.value = true
  try {
    const result = envelopeData(await getGlobalKnowledgeEntityContext(entityId, showEntityHistory.value))
    if (generation === contextGeneration && selectedEntity.value?.id === entityId) entityContext.value = result
  } catch (error) { if (generation === contextGeneration) loadError.value = getErrorMessage(error) }
  finally { if (generation === contextGeneration) entityContextLoading.value = false }
}

async function toggleEvent(eventId: string) {
  const generation = contextGeneration
  if (expandedEventId.value === eventId) {
    expandedEventId.value = ''
    return
  }
  expandedEventId.value = eventId
  if (eventContexts.value[eventId]) return
  eventLoadingId.value = eventId
  try {
    const result = envelopeData(await queryKnowledgeContext(props.kbId, makeQuery({ event_id: eventId })))
    if (generation === contextGeneration) eventContexts.value[eventId] = result
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { if (generation === contextGeneration) eventLoadingId.value = '' }
}

async function openEvidence(evidenceId: string, quote?: string) {
  if (quote !== undefined) {
    const request = ++snapshotRequest
    snapshotVisible.value = true
    snapshotLoading.value = true
    snapshotError.value = ''
    selectedSnapshot.value = null
    selectedQuote.value = quote
    try {
      const snapshot = evidenceSnapshots[evidenceId] || envelopeData(await getGlobalKnowledgeEvidence(evidenceId))
      if (request !== snapshotRequest) return
      selectedSnapshot.value = snapshot
      evidenceSnapshots[evidenceId] = selectedSnapshot.value
      await nextTick()
      snapshotBody.value?.querySelector('mark')?.scrollIntoView({ block: 'center' })
    } catch (error) { if (request === snapshotRequest) snapshotError.value = getErrorMessage(error) }
    finally { if (request === snapshotRequest) snapshotLoading.value = false }
    return
  }
  if (expandedEvidenceId.value === evidenceId) {
    expandedEvidenceId.value = ''
    return
  }
  expandedEvidenceId.value = evidenceId
  if (evidenceSnapshots[evidenceId]) return
  evidenceLoadingId.value = evidenceId
  try { evidenceSnapshots[evidenceId] = envelopeData(await getGlobalKnowledgeEvidence(evidenceId)) }
  catch (error) { loadError.value = getErrorMessage(error) }
  finally { evidenceLoadingId.value = '' }
}

async function showEvidenceInList(evidenceId: string) {
  try {
    const snapshot = evidenceSnapshots[evidenceId] || envelopeData(await getGlobalKnowledgeEvidence(evidenceId))
    evidenceSnapshots[evidenceId] = snapshot
    if (!evidence.value.some(item => item.id === evidenceId)) evidence.value.unshift(snapshot)
    activeSection.value = 'evidence'
    expandedEvidenceId.value = evidenceId
  } catch (error) { loadError.value = getErrorMessage(error) }
}

function safeExternalUrl(value?: string) {
  if (!value) return ''
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : ''
  } catch { return '' }
}

function entityName(entityId?: string) {
  if (!entityId) return t('knowledgeWorkbench.entities.unknownObject')
  const match = entities.value.find(entity => entity.id === entityId)
  if (match) return match.canonical_name
  if (selectedEntity.value?.id === entityId) return selectedEntity.value.canonical_name
  return entityId
}

function formatValue(value: unknown) {
  if (value === undefined || value === null || value === '') return t('knowledgeWorkbench.emptyValue')
  if (typeof value === 'string') return value
  try { return JSON.stringify(value) } catch { return String(value) }
}

function formatHeat(value?: number) {
  return Number.isFinite(value) ? Number(value).toFixed(1) : '0.0'
}

function formatDate(value?: string) {
  if (!value) return t('knowledgeWorkbench.unknownDate')
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function normalizeFactText(value?: string) {
  return (value || '').normalize('NFKC').trim().toLowerCase()
}

function stableFactValue(value: unknown): string {
  const normalize = (item: unknown): unknown => {
    if (Array.isArray(item)) return item.map(normalize)
    if (item && typeof item === 'object') {
      return Object.fromEntries(Object.entries(item).sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0).map(([key, child]) => [key, normalize(child)]))
    }
    return item
  }
  try { return JSON.stringify(normalize(value)) ?? 'undefined' } catch { return String(value) }
}

function factTimeDifference(left: KnowledgeFact, right: KnowledgeFact): 'conflict' | 'potential' | null {
  const date = (value?: string) => {
    if (!value) return undefined
    const time = Date.parse(value)
    return Number.isFinite(time) ? time : undefined
  }
  const leftStart = date(left.effective_from)
  const leftEnd = date(left.effective_to)
  const rightStart = date(right.effective_from)
  const rightEnd = date(right.effective_to)

  if ((leftStart !== undefined && leftEnd !== undefined && leftStart > leftEnd)
    || (rightStart !== undefined && rightEnd !== undefined && rightStart > rightEnd)) return 'potential'
  if ((leftEnd !== undefined && rightStart !== undefined && leftEnd < rightStart)
    || (rightEnd !== undefined && leftStart !== undefined && rightEnd < leftStart)) return null
  if ([leftStart, leftEnd, rightStart, rightEnd].some(value => value === undefined)) return 'potential'
  return 'conflict'
}

function factDifference(fact: KnowledgeFact, facts: KnowledgeFact[]): 'conflict' | 'potential' | null {
  if (fact.kind !== 'attribute' || !['current', 'superseded', 'corrected'].includes(fact.status)) return null
  const predicate = normalizeFactText(fact.predicate)
  if (!fact.subject_entity_id || !predicate) return null
  const valueKey = `${stableFactValue(fact.value)}\u0000${normalizeFactText(fact.unit)}`
  let potential = false
  for (const other of facts) {
    if (other.id === fact.id || other.kind !== 'attribute' || !['current', 'superseded', 'corrected'].includes(other.status)) continue
    if (other.subject_entity_id !== fact.subject_entity_id || normalizeFactText(other.predicate) !== predicate) continue
    if (`${stableFactValue(other.value)}\u0000${normalizeFactText(other.unit)}` === valueKey) continue
    const difference = factTimeDifference(fact, other)
    if (difference === 'conflict') return difference
    if (difference === 'potential') potential = true
  }
  return potential ? 'potential' : null
}

function dateToISO(value: string) {
  if (!value) return undefined
  const date = new Date(`${value}T00:00:00`)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

function startCorrection(fact: KnowledgeFact) {
  if (!props.canManage || fact.kind !== 'attribute' || fact.status !== 'current') return
  editingFact.value = {
    fact,
    value: formatValue(fact.value),
    valueType: fact.value_type || 'string',
    unit: fact.unit || '',
    quote: fact.quote || '',
    correctionNote: '',
    effectiveFrom: dateInputValue(fact.effective_from),
    effectiveTo: dateInputValue(fact.effective_to),
  }
  correctionVisible.value = true
}

const editingFact = ref<{
  fact: KnowledgeFact
  value: string
  valueType: string
  unit: string
  quote: string
  correctionNote: string
  effectiveFrom: string
  effectiveTo: string
} | null>(null)

function dateInputValue(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString().slice(0, 10)
}

function closeCorrection() {
  correctionVisible.value = false
  editingFact.value = null
}

async function saveCorrection() {
  if (!editingFact.value) return
  const edit = editingFact.value
  if (!edit.value.trim() || !edit.quote.trim() || !edit.correctionNote.trim()) return
  let value: unknown = edit.value
  try {
    if (edit.valueType === 'number' || edit.valueType === 'boolean' || edit.valueType === 'json') value = JSON.parse(edit.value)
  } catch {
    loadError.value = t('knowledgeWorkbench.correctionValueInvalid')
    return
  }
  correcting.value = true
  try {
    envelopeData(await correctGlobalKnowledgeFact(edit.fact.id, {
      value,
      value_type: edit.valueType || undefined,
      unit: edit.unit || undefined,
      quote: edit.quote.trim(),
      correction_note: edit.correctionNote.trim(),
      effective_from: dateToISO(edit.effectiveFrom),
      effective_to: dateToISO(edit.effectiveTo),
    }))
    closeCorrection()
    MessagePlugin.success(t('knowledgeWorkbench.correctionSaved'))
    await refreshAll()
    if (selectedEntity.value) await loadEntityContext()
    if (queryResult.value) await runQuery()
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { correcting.value = false }
}

function openMerge() {
  mergeTargetId.value = ''
  mergeVisible.value = true
}

async function mergeEntities() {
  if (!selectedEntity.value || !mergeTargetId.value) return
  merging.value = true
  try {
    const result = await mergeGlobalKnowledgeEntities({
      keep_entity_id: mergeTargetId.value,
      merge_entity_id: selectedEntity.value.id,
    })
    if (!result.success) throw new Error(result.error || t('knowledgeWorkbench.requestFailed'))
    mergeVisible.value = false
    selectedEntity.value = null
    entityContext.value = null
    MessagePlugin.success(t('knowledgeWorkbench.entities.mergeSaved'))
    await refreshAll()
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { merging.value = false }
}

async function rebuild() {
  rebuilding.value = true
  try {
    const result = envelopeData(await rebuildKnowledgeContext(props.kbId))
    MessagePlugin.success(t('knowledgeWorkbench.rebuildQueued', { count: result.queued }))
    await refreshAll()
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { rebuilding.value = false }
}

async function loadWorkflows() {
  workflowsLoading.value = true
  try {
    workflows.value = envelopeData(await listKnowledgeWorkflows(props.kbId)) || []
    if (selectedWorkflow.value) {
      selectedWorkflow.value = workflows.value.find(item => item.id === selectedWorkflow.value?.id) || null
      if (selectedWorkflow.value) await loadRuns()
    }
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { workflowsLoading.value = false }
}

async function loadRuns() {
  if (!selectedWorkflow.value) return
  runsLoading.value = true
  try {
    workflowRuns.value = envelopeData(await listKnowledgeWorkflowRuns(props.kbId, selectedWorkflow.value.id)) || []
    const current = selectedRun.value && workflowRuns.value.find(run => run.id === selectedRun.value?.id)
    if (current) await selectRun(current)
    else if (workflowRuns.value.length) await selectRun(workflowRuns.value[0])
    else selectedRun.value = null
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { runsLoading.value = false }
}

async function selectWorkflow(workflow: KnowledgeWorkflow) {
  selectedWorkflow.value = workflow
  selectedRun.value = null
  workflowRuns.value = []
  await loadRuns()
}

async function selectRun(run: KnowledgeWorkflowRun) {
  selectedRun.value = run
  try {
    selectedRun.value = envelopeData(await getKnowledgeWorkflowRun(props.kbId, run.id))
    updateRunPolling()
  } catch (error) { loadError.value = getErrorMessage(error) }
}

function clearRunPolling() {
  if (runPollTimer) clearInterval(runPollTimer)
  runPollTimer = null
}

function updateRunPolling() {
  clearRunPolling()
  if (selectedRun.value && ['queued', 'running', 'waiting_ingestion'].includes(selectedRun.value.status)) {
    runPollTimer = setInterval(() => {
      if (selectedRun.value) void selectRun(selectedRun.value)
    }, 3000)
  }
}

async function runWorkflow(workflow: KnowledgeWorkflow) {
  runningWorkflowId.value = workflow.id
  try {
    const run = envelopeData(await runKnowledgeWorkflow(props.kbId, workflow.id))
    selectedWorkflow.value = workflow
    await loadRuns()
    await selectRun(run)
    MessagePlugin.success(t('knowledgeWorkbench.workflows.runStarted'))
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { runningWorkflowId.value = '' }
}

async function decideRun(decision: 'approve' | 'reject') {
  if (!selectedRun.value) return
  decisionLoading.value = true
  try {
    selectedRun.value = envelopeData(await decideKnowledgeWorkflowRun(props.kbId, selectedRun.value.id, decision))
    await loadRuns()
    MessagePlugin.success(t(`knowledgeWorkbench.workflows.decision.${decision}`))
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { decisionLoading.value = false }
}

async function resumeRun() {
  if (!selectedRun.value) return
  resumingRun.value = true
  try {
    selectedRun.value = envelopeData(await resumeKnowledgeWorkflowRun(props.kbId, selectedRun.value.id))
    await loadRuns()
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { resumingRun.value = false }
}

async function retryRun() {
  if (!selectedRun.value) return
  retryingRun.value = true
  try {
    selectedRun.value = envelopeData(await retryKnowledgeWorkflowRun(props.kbId, selectedRun.value.id))
    await loadRuns()
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { retryingRun.value = false }
}

function workflowStepName(stepId: string) {
  return selectedWorkflow.value?.steps.find(step => step.id === stepId)?.id || ''
}

function stepStatusTheme(status: string): 'success' | 'danger' | 'warning' | 'default' {
  if (status === 'succeeded') return 'success'
  if (status === 'failed' || status === 'rejected') return 'danger'
  if (status === 'waiting_review' || status === 'running') return 'warning'
  return 'default'
}

function formatOutput(value: unknown) {
  if (typeof value === 'string') return value
  const redactArtifactMetadata = (item: unknown): unknown => {
    if (Array.isArray(item)) return item.map(redactArtifactMetadata)
    if (!item || typeof item !== 'object') return item
    return Object.fromEntries(Object.entries(item).filter(([key]) =>
      !['source_path', 'storage_path', 'session_id', 'message_id', 'artifacts', 'provider_url'].includes(key),
    ).map(([key, child]) => [key, redactArtifactMetadata(child)]))
  }
  try { return JSON.stringify(redactArtifactMetadata(value), null, 2) } catch { return String(value) }
}

type WorkflowStepArtifact = ArtifactMeta & { sessionId: string; messageId: string; key: string }

function stepArtifacts(stepRun: KnowledgeWorkflowStepRun): WorkflowStepArtifact[] {
  const output = stepRun.output && typeof stepRun.output === 'object' ? stepRun.output as Record<string, unknown> : null
  const data = output?.data && typeof output.data === 'object' ? output.data as Record<string, unknown> : null
  const sessionId = typeof data?.session_id === 'string' ? data.session_id : ''
  const messageId = typeof data?.message_id === 'string' ? data.message_id : ''
  if (!sessionId || !messageId || !Array.isArray(data?.artifacts)) return []
  return data.artifacts.flatMap((value, index) => {
    if (!value || typeof value !== 'object') return []
    const artifact = value as Partial<ArtifactMeta>
    if (!artifact.file_name) return []
    const key = `${sessionId}:${messageId}:${index}`
    return [{ ...artifact, index, sessionId, messageId, key } as WorkflowStepArtifact]
  })
}

async function downloadStepArtifact(artifact: WorkflowStepArtifact) {
  downloadingArtifactKey.value = artifact.key
  try {
    const file = await downloadArtifact(artifact.sessionId, artifact.messageId, artifact.index)
    const url = URL.createObjectURL(file)
    const link = document.createElement('a')
    link.href = url
    link.download = safeFilename(artifact.file_name)
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(url)
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { downloadingArtifactKey.value = '' }
}

function makeStep(type: KnowledgeWorkflowStepType, index: number): KnowledgeWorkflowStep {
  const config: Record<string, unknown> = {
    search: { query: '', limit: 20 },
    fact_query: { scope: 'knowledge_base', entity: '', predicate: '', relation: '', object_entity_id: '', since: '', until: '', include_history: true, limit: 50 },
    web_research: { query: '', max_results: 10, provider_id: '', ingest: true },
    report: { title: '', prompt: '', time_from: '', time_to: '', window: '', days: 7, timezone: 'Asia/Shanghai' },
    wiki_update: { slug: '', title: '', page_type: 'topic', summary: '', source_step: '' },
    skill: { agent_id: '', skill_name: '', script_path: '', args: [], args_text: '[]', input: '', input_step: '' },
    condition: { step_id: '', path: '', operator: 'equals', value: '' },
    human_review: { message: '' },
  }[type]
  return { id: `step_${index + 1}`, type, config: { ...config } }
}

function blankWorkflow(): KnowledgeWorkflowInput {
  return {
    name: '', description: '', enabled: false,
    schedule: { cron: '', timezone: 'Asia/Shanghai' },
    steps: [makeStep('search', 0), makeStep('report', 1)],
  }
}

function startNewWorkflow() {
  workflowDraftId.value = ''
  selectedTemplate.value = 'custom'
  workflowDraft.value = blankWorkflow()
  workflowEditorError.value = ''
}

function applyTemplate(templateId: string) {
  selectedTemplate.value = templateId
  if (!workflowDraft.value) workflowDraft.value = blankWorkflow()
  const draft = workflowDraft.value
  if (templateId === 'daily') {
    draft.name = t('knowledgeWorkbench.workflows.templates.daily.title')
    draft.description = t('knowledgeWorkbench.workflows.templates.daily.description')
    draft.enabled = true
    draft.schedule = { cron: '0 8 * * *', timezone: 'Asia/Shanghai' }
    draft.steps = [makeStep('search', 0), makeStep('report', 1)]
    draft.steps[0].config.query = t('knowledgeWorkbench.workflows.templates.daily.searchQuery')
    draft.steps[1].depends_on = [draft.steps[0].id]
    draft.steps[1].config.title = t('knowledgeWorkbench.workflows.templates.daily.reportTitle')
    draft.steps[1].config.prompt = t('knowledgeWorkbench.workflows.templates.daily.reportPrompt')
    draft.steps[1].config.window = 'previous_day'
    draft.steps[1].config.timezone = 'Asia/Shanghai'
  } else if (templateId === 'weekly') {
    draft.name = t('knowledgeWorkbench.workflows.templates.weekly.title')
    draft.description = t('knowledgeWorkbench.workflows.templates.weekly.description')
    draft.enabled = true
    draft.schedule = { cron: '0 9 * * 1', timezone: 'Asia/Shanghai' }
    draft.steps = [makeStep('fact_query', 0), makeStep('report', 1)]
    draft.steps[0].config.include_history = true
    draft.steps[1].depends_on = [draft.steps[0].id]
    draft.steps[1].config.title = t('knowledgeWorkbench.workflows.templates.weekly.reportTitle')
    draft.steps[1].config.prompt = t('knowledgeWorkbench.workflows.templates.weekly.reportPrompt')
    draft.steps[1].config.window = 'previous_week'
    draft.steps[1].config.timezone = 'Asia/Shanghai'
  } else if (templateId === 'research') {
    draft.name = t('knowledgeWorkbench.workflows.templates.research.title')
    draft.description = t('knowledgeWorkbench.workflows.templates.research.description')
    draft.enabled = true
    draft.schedule = { cron: '0 9 * * *', timezone: 'Asia/Shanghai' }
    draft.steps = [makeStep('web_research', 0), makeStep('fact_query', 1), makeStep('report', 2)]
    draft.steps[0].config.ingest = true
    draft.steps[1].depends_on = [draft.steps[0].id]
    draft.steps[1].config.include_history = true
    draft.steps[2].depends_on = [draft.steps[1].id]
    draft.steps[2].config.title = t('knowledgeWorkbench.workflows.templates.research.reportTitle')
    draft.steps[2].config.prompt = t('knowledgeWorkbench.workflows.templates.research.reportPrompt')
    draft.steps[2].config.window = 'rolling_days'
    draft.steps[2].config.days = 7
    draft.steps[2].config.timezone = 'Asia/Shanghai'
  } else {
    draft.name = ''
    draft.description = ''
    draft.enabled = false
    draft.schedule = { cron: '', timezone: 'Asia/Shanghai' }
    draft.steps = [makeStep('search', 0), makeStep('report', 1)]
    draft.steps[1].depends_on = [draft.steps[0].id]
  }
  workflowEditorError.value = ''
}

function editWorkflow(workflow: KnowledgeWorkflow) {
  workflowDraftId.value = workflow.id
  workflowDraft.value = {
    name: workflow.name,
    description: workflow.description,
    enabled: workflow.enabled,
    schedule: { ...workflow.schedule },
    steps: workflow.steps.map(step => {
      const config = { ...step.config }
      if (step.type === 'skill') config.args_text = JSON.stringify(config.args || [])
      return {
        ...step,
        depends_on: [...(step.depends_on || [])],
        config,
        when: step.when ? { ...step.when } : undefined,
      }
    }),
  }
  workflowEditorError.value = ''
}

function cancelWorkflowEdit() {
  workflowDraftId.value = ''
  workflowDraft.value = null
  workflowEditorError.value = ''
}

function defaultConfig(type: KnowledgeWorkflowStepType): Record<string, unknown> {
  return makeStep(type, workflowDraft.value?.steps.length || 0).config
}

function addWorkflowStep() {
  if (!workflowDraft.value) return
  const next = makeStep(stepTypeToAdd.value, workflowDraft.value.steps.length)
  while (workflowDraft.value.steps.some(step => step.id === next.id)) next.id = `${next.id}_${workflowDraft.value.steps.length + 1}`
  workflowDraft.value.steps.push(next)
}

function removeWorkflowStep(index: number) {
  if (!workflowDraft.value) return
  const [removed] = workflowDraft.value.steps.splice(index, 1)
  for (const step of workflowDraft.value.steps) {
    step.depends_on = (step.depends_on || []).filter(id => id !== removed.id)
    if (step.when?.step_id === removed.id) step.when = undefined
  }
}

function moveWorkflowStep(index: number, offset: number) {
  if (!workflowDraft.value) return
  const target = index + offset
  const [step] = workflowDraft.value.steps.splice(index, 1)
  workflowDraft.value.steps.splice(target, 0, step)
}

function dependencyOptions(step: KnowledgeWorkflowStep) {
  const steps = workflowDraft.value?.steps || []
  const stepIndex = steps.findIndex(other => other.id === step.id)
  return steps.slice(0, stepIndex).filter(other => other.id !== step.id)
    .map(other => ({ value: other.id, label: other.id }))
}

function conditionOptions(step: KnowledgeWorkflowStep) {
  const steps = workflowDraft.value?.steps || []
  const stepIndex = steps.findIndex(other => other.id === step.id)
  return steps.slice(0, stepIndex).filter(other => other.type === 'condition')
    .map(other => ({ value: other.id, label: other.id }))
}

function toggleStepCondition(step: KnowledgeWorkflowStep, checked?: boolean) {
  const enable = checked ?? !step.when
  step.when = enable ? (step.when || { step_id: '', equals: true }) : undefined
}

function setConditionStep(step: KnowledgeWorkflowStep, stepId: string) {
  if (step.when) step.when.step_id = stepId
}

function setConditionValue(step: KnowledgeWorkflowStep, value: unknown) {
  if (step.when) step.when.equals = String(value) === 'true'
}

function setReportWindow(step: KnowledgeWorkflowStep, value: unknown) {
  const window = typeof value === 'string' ? value : ''
  step.config.window = window
  if (window) {
    delete step.config.time_from
    delete step.config.time_to
  }
  if (window !== 'rolling_days') delete step.config.days
}

function normalizeWorkflowInput(draft: KnowledgeWorkflowInput): KnowledgeWorkflowInput {
  return {
    ...draft,
    name: draft.name.trim(),
    description: draft.description?.trim() || '',
    schedule: { cron: draft.schedule.cron?.trim() || '', timezone: draft.schedule.timezone?.trim() || 'Asia/Shanghai' },
    steps: draft.steps.map(step => {
      const config = { ...step.config }
      if (step.type === 'web_research') config.ingest = true
      if (step.type === 'fact_query' && config.scope === 'domain' && !config.domain_id) throw new Error(t('knowledgeDomains.domainRequired'))
      if (step.type === 'condition' && (config.operator === 'gt' || config.operator === 'gte')) {
        const value = typeof config.value === 'string' && config.value.trim() !== '' ? Number(config.value) : config.value
        if (typeof value !== 'number' || !Number.isFinite(value)) {
          throw new Error(t('knowledgeWorkbench.workflows.invalidConditionValue', { step: step.id }))
        }
        config.value = value
      }
      if (step.type === 'skill' && typeof config.args_text === 'string') {
        try {
          const args = config.args_text.trim() ? JSON.parse(config.args_text) : []
          if (!Array.isArray(args) || !args.every(arg => typeof arg === 'string')) throw new Error('args must be a string array')
          config.args = args
        }
        catch { throw new Error(t('knowledgeWorkbench.workflows.invalidArgs', { step: step.id })) }
        delete config.args_text
      }
      return {
        id: step.id.trim(), type: step.type,
        depends_on: (step.depends_on || []).filter(Boolean),
        when: step.when?.step_id ? { ...step.when } : undefined,
        config,
      }
    }),
  }
}

async function saveWorkflow() {
  if (!workflowDraft.value) return
  workflowEditorError.value = ''
  let input: KnowledgeWorkflowInput
  try { input = normalizeWorkflowInput(workflowDraft.value) }
  catch (error) { workflowEditorError.value = getErrorMessage(error); return }
  if (!input.name || !input.steps.length || input.steps.some(step => !step.id)) {
    workflowEditorError.value = t('knowledgeWorkbench.workflows.validation')
    return
  }
  workflowSaving.value = true
  try {
    const saved = workflowDraftId.value
      ? envelopeData(await updateKnowledgeWorkflow(props.kbId, workflowDraftId.value, input))
      : envelopeData(await createKnowledgeWorkflow(props.kbId, input))
    cancelWorkflowEdit()
    workflows.value = envelopeData(await listKnowledgeWorkflows(props.kbId)) || []
    const current = workflows.value.find(item => item.id === saved.id)
    if (current) await selectWorkflow(current)
    MessagePlugin.success(t('knowledgeWorkbench.workflows.saved'))
  } catch (error) { workflowEditorError.value = getErrorMessage(error) }
  finally { workflowSaving.value = false }
}

async function toggleWorkflow(workflow: KnowledgeWorkflow) {
  try {
    const input: KnowledgeWorkflowInput = {
      name: workflow.name,
      description: workflow.description || '',
      enabled: !workflow.enabled,
      schedule: { ...workflow.schedule },
      steps: workflow.steps,
    }
    envelopeData(await updateKnowledgeWorkflow(props.kbId, workflow.id, input))
    await loadWorkflows()
  } catch (error) { loadError.value = getErrorMessage(error) }
}

async function loadReports() {
  reportsLoading.value = true
  try {
    reports.value = envelopeData(await listKnowledgeWorkflowReports(props.kbId)) || []
    if (selectedReport.value) {
      selectedReport.value = reports.value.find(report => report.id === selectedReport.value?.id) || null
    }
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { reportsLoading.value = false }
}

async function selectReport(report: KnowledgeWorkflowReport) {
  try { selectedReport.value = envelopeData(await getKnowledgeWorkflowReport(props.kbId, report.id)) }
  catch (error) { loadError.value = getErrorMessage(error) }
}

async function downloadReport(report: KnowledgeWorkflowReport) {
  reportDownloading.value = true
  try {
    const file = await downloadKnowledgeWorkflowReport(props.kbId, report.id)
    const url = URL.createObjectURL(file)
    const link = document.createElement('a')
    link.href = url
    link.download = `${safeFilename(report.title)}.md`
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(url)
  } catch (error) { loadError.value = getErrorMessage(error) }
  finally { reportDownloading.value = false }
}

function safeFilename(value: string) {
  return (value || 'report').replace(/[\\/:*?"<>|\u0000-\u001f]/g, '_').trim() || 'report'
}

watch(() => props.domainId, id => { if (!queryDomainId.value) queryDomainId.value = id || '' })
watch([() => props.kbId, queryScope, queryDomainId], async () => {
  contextGeneration++
  loading.value = false
  entitiesLoading.value = false
  eventsLoading.value = false
  evidenceLoading.value = false
  queryLoading.value = false
  entityContextLoading.value = false
  expandedEventId.value = ''
  eventContexts.value = {}
  entityOffset.value = 0
  eventOffset.value = 0
  evidenceOffset.value = 0
  hasMoreEntities.value = false
  hasMoreEvents.value = false
  hasMoreEvidence.value = false
  snapshotRequest++
  snapshotVisible.value = false
  snapshotLoading.value = false
  selectedSnapshot.value = null
  selectedQuote.value = ''
  for (const id of Object.keys(evidenceSnapshots)) delete evidenceSnapshots[id]
  entities.value = []
  events.value = []
  evidence.value = []
  selectedEntity.value = null
  entityContext.value = null
  queryResult.value = null
  workflows.value = []
  selectedWorkflow.value = null
  workflowRuns.value = []
  selectedRun.value = null
  reports.value = []
  selectedReport.value = null
  clearRunPolling()
  if (!domains.value.length) await loadDomains()
  await refreshAll()
}, { immediate: true })

watch(selectedRun, updateRunPolling)
onBeforeUnmount(() => { contextGeneration++; snapshotRequest++; clearRunPolling() })

const FactCard = defineComponent({
  name: 'KnowledgeWorkbenchFactCard',
  props: {
    fact: { type: Object as () => KnowledgeFact, required: true },
    facts: { type: Array as () => KnowledgeFact[], required: true },
    entities: { type: Array as () => KnowledgeEntity[], required: true },
    evidence: { type: Array as () => KnowledgeEvidence[], required: true },
    canManage: { type: Boolean, default: false },
  },
  emits: ['correct', 'openEvidence', 'openSource'],
  setup(cardProps, { emit: cardEmit }) {
    const labelValue = (value: unknown) => {
      if (value === null || value === undefined || value === '') return t('knowledgeWorkbench.emptyValue')
      if (typeof value === 'string') return value
      try { return JSON.stringify(value) } catch { return String(value) }
    }
    const objectName = (id?: string) => cardProps.entities.find(entity => entity.id === id)?.canonical_name || id || t('knowledgeWorkbench.entities.unknownObject')
    const sourceTitle = computed(() => cardProps.evidence.find(item => item.id === cardProps.fact.evidence_id)?.title)
    const difference = computed(() => factDifference(cardProps.fact, cardProps.facts))
    return () => h('article', { class: 'fact-card' }, [
      h('div', { class: 'fact-card__heading' }, [
        h('div', { class: 'fact-expression' }, [
          h('strong', objectName(cardProps.fact.subject_entity_id)),
          h('span', cardProps.fact.predicate),
          cardProps.fact.kind === 'relation'
            ? h('strong', objectName(cardProps.fact.object_entity_id))
            : h('strong', `${labelValue(cardProps.fact.value)}${cardProps.fact.unit ? ` ${cardProps.fact.unit}` : ''}`),
        ]),
        h('span', { class: ['fact-status', `is-${cardProps.fact.status}`] }, t(`knowledgeWorkbench.factStatus.${cardProps.fact.status}`)),
      ]),
      difference.value
        ? h('p', { class: ['fact-difference', `is-${difference.value}`], role: 'status' },
          t(`knowledgeWorkbench.factDifference.${difference.value}`))
        : null,
      h('div', { class: 'fact-card__meta' }, [
        h('span', `${t('knowledgeWorkbench.factPeriod')}: ${formatDate(cardProps.fact.effective_from)}${cardProps.fact.effective_to ? ` — ${formatDate(cardProps.fact.effective_to)}` : ''}`),
        h('span', `${t('knowledgeWorkbench.evidence.source')}: ${sourceTitle.value || cardProps.fact.evidence_id.slice(0, 8)}`),
        h('span', `${t('knowledgeWorkbench.scope.sourceKb')}: ${cardProps.fact.knowledge_base_name || cardProps.fact.knowledge_base_id}`),
        cardProps.fact.original_predicate && cardProps.fact.original_predicate !== cardProps.fact.predicate
          ? h('span', `${t('knowledgeWorkbench.fusion.originalField')}: ${cardProps.fact.original_predicate}`) : null,
      ]),
      cardProps.fact.quote ? h('blockquote', cardProps.fact.quote) : null,
      cardProps.fact.correction_note ? h('p', { class: 'fact-card__correction' }, `${t('knowledgeWorkbench.correctionNote')}: ${cardProps.fact.correction_note}`) : null,
      h('div', { class: 'fact-card__actions' }, [
        h('button', { type: 'button', class: 'text-link', onClick: () => cardEmit('openEvidence', cardProps.fact.evidence_id, cardProps.fact.quote) }, t('knowledgeWorkbench.evidence.openSnapshot')),
        cardProps.evidence.find(item => item.id === cardProps.fact.evidence_id)?.knowledge_id
          ? h('button', { type: 'button', class: 'text-link', onClick: () => cardEmit('openSource', cardProps.evidence.find(item => item.id === cardProps.fact.evidence_id)?.knowledge_id) }, t('knowledgeWorkbench.evidence.openKnowledge'))
          : null,
        cardProps.canManage && cardProps.fact.kind === 'attribute' && cardProps.fact.status === 'current'
          ? h('button', { type: 'button', class: 'text-link', onClick: () => cardEmit('correct', cardProps.fact) }, t('knowledgeWorkbench.correct'))
          : null,
      ]),
    ])
  },
})

const EventCard = defineComponent({
  name: 'KnowledgeWorkbenchEventCard',
  props: {
    event: { type: Object as () => KnowledgeEvent, required: true },
    attention: { type: Array as () => KnowledgeAttention[], required: true },
    evidence: { type: Array as () => KnowledgeEvidence[], required: true },
  },
  emits: ['openEvidence'],
  setup(cardProps, { emit: cardEmit }) {
    return () => h('article', { class: 'event-card' }, [
      h('div', { class: 'event-card__heading' }, [
        h('strong', cardProps.event.title),
        h('span', { class: 'event-heat' }, `${t('knowledgeWorkbench.events.heat')} ${formatHeat(cardProps.event.heat)}`),
      ]),
      h('p', cardProps.event.description || t('knowledgeWorkbench.events.noDescription')),
      ...(cardProps.event.relations || []).filter(relation => relation.relation === 'same_story').map(relation => h('p', { class: 'event-card__meta' }, `${t('knowledgeWorkbench.fusion.sameStory')}: ${relation.related_title || relation.related_event_id}`)),
      h('div', { class: 'event-card__meta' }, [
        `${t('knowledgeWorkbench.events.sourceCount', { count: cardProps.event.source_count })} · ${formatDate(cardProps.event.occurred_at)}`,
      ]),
      h('p', { class: 'event-card__meta' }, `${t('knowledgeWorkbench.scope.sourceKb')}: ${(cardProps.event.sources || []).map(source => source.knowledge_base_name || source.knowledge_base_id).join('、') || cardProps.event.knowledge_base_name || cardProps.event.knowledge_base_id}`),
      ...cardProps.attention.map(item => h('p', { class: 'attention-reason', key: item.id }, [
        h('strong', item.reason || t('knowledgeWorkbench.events.scored')),
        h('span', t('knowledgeWorkbench.events.attentionScore', { score: formatHeat(item.score) })),
      ])),
      ...cardProps.evidence.map(item => h('button', {
        key: item.id,
        type: 'button',
        class: 'text-link',
        onClick: () => cardEmit('openEvidence', item.id),
      }, `${t('knowledgeWorkbench.evidence.openSnapshot')}: ${item.title}`)),
    ])
  },
})

const EvidenceCard = defineComponent({
  name: 'KnowledgeWorkbenchEvidenceCard',
  props: {
    evidence: { type: Object as () => KnowledgeEvidence, required: true },
    expanded: { type: Boolean, default: false },
    loading: { type: Boolean, default: false },
    snapshot: { type: Object as () => KnowledgeEvidence | undefined, default: undefined },
  },
  emits: ['toggle', 'openSource'],
  setup(cardProps, { emit: cardEmit }) {
    return () => h('article', { class: 'evidence-card' }, [
      h('div', { class: 'evidence-card__main' }, [
        h('button', { type: 'button', class: 'evidence-card__open', onClick: () => cardEmit('toggle') }, [
          h('strong', cardProps.evidence.title || t('knowledgeWorkbench.evidence.untitled')),
          h('span', { class: 'evidence-card__version' }, `v${cardProps.evidence.version} · ${t(`knowledgeWorkbench.evidence.status.${cardProps.evidence.status}`)}`),
        ]),
        h('div', { class: 'evidence-card__meta' }, [
          `${t('knowledgeWorkbench.scope.sourceKb')}: ${cardProps.evidence.knowledge_base_name || cardProps.evidence.knowledge_base_id}`,
          [cardProps.evidence.publisher, cardProps.evidence.author, cardProps.evidence.published_at ? formatDate(cardProps.evidence.published_at) : ''].filter(Boolean).join(' · '),
        ]),
        cardProps.evidence.source_kind === 'roundup' ? h('p', { class: 'evidence-card__meta' }, t('knowledgeWorkbench.fusion.roundup')) : null,
        safeExternalUrl(cardProps.evidence.original_url) ? h('a', { href: safeExternalUrl(cardProps.evidence.original_url), target: '_blank', rel: 'noopener noreferrer' }, t('knowledgeWorkbench.evidence.openSource')) : null,
        cardProps.evidence.knowledge_id ? h('button', { type: 'button', class: 'text-link', onClick: () => cardEmit('openSource', cardProps.evidence.knowledge_id) }, t('knowledgeWorkbench.evidence.openKnowledge')) : null,
      ]),
      cardProps.expanded ? h('div', { class: 'evidence-snapshot' }, cardProps.loading
        ? t('common.loading')
        : [
          h('div', { class: 'evidence-snapshot__meta' }, [
            `${t('knowledgeWorkbench.evidence.version')}: ${cardProps.snapshot?.version ?? cardProps.evidence.version}`,
            `${t('knowledgeWorkbench.evidence.hash')}: ${(cardProps.snapshot?.body_hash || cardProps.evidence.body_hash).slice(0, 16)}`,
            `${t('knowledgeWorkbench.evidence.extraction')}: ${t(`knowledgeWorkbench.evidence.extractionStatus.${cardProps.snapshot?.extraction_status || cardProps.evidence.extraction_status}`)}`,
          ].map(text => h('span', text))),
          cardProps.snapshot?.extraction_error ? h('p', { class: 'run-error' }, cardProps.snapshot.extraction_error) : null,
          h('pre', { class: 'evidence-snapshot__body' }, cardProps.snapshot?.body || cardProps.evidence.body || t('knowledgeWorkbench.evidence.noBody')),
        ]) : null,
    ])
  },
})
</script>

<style scoped lang="less">
.workbench-scope { display: flex; flex-wrap: wrap; gap: 16px; margin-bottom: 20px; }
.workbench-scope label { display: flex; flex-direction: column; gap: 6px; min-width: min(260px, 100%); }
.knowledge-workbench { color: var(--td-text-color-primary); padding: 20px 22px 34px; }
.snapshot-hash { overflow-wrap: anywhere; color: var(--td-text-color-secondary); font-size: 12px; }
.evidence-snapshot__body mark { background: var(--td-warning-color-2); color: var(--td-text-color-primary); border-radius: 2px; }
.workbench-header, .panel-heading, .workflow-editor__header, .workflow-steps-heading, .group-heading,
.workflow-list__heading, .run-detail__heading, .event-row__title, .report-view > header {
  display: flex; align-items: center; justify-content: space-between; gap: 16px;
}
.workbench-header { margin-bottom: 18px; }
.workbench-header h2, .panel-heading h3, .entity-detail h3, .report-view h4 { margin: 0; }
.workbench-header p, .panel-heading p, .workflow-steps-heading p, .group-heading p { margin: 4px 0 0; color: var(--td-text-color-secondary); font-size: 13px; }
.workbench-header__actions { display: flex; gap: 8px; }
.workbench-nav { display: flex; gap: 6px; overflow-x: auto; border-bottom: 1px solid var(--td-border-level-1-color); margin: 0 -4px 18px; padding: 0 4px; }
.workbench-nav__item { display: inline-flex; align-items: center; gap: 8px; white-space: nowrap; border: 0; border-bottom: 2px solid transparent; background: none; color: var(--td-text-color-secondary); padding: 10px 12px; cursor: pointer; }
.workbench-nav__item:hover, .workbench-nav__item.active { color: var(--td-brand-color); }
.workbench-nav__item.active { border-bottom-color: var(--td-brand-color); font-weight: 600; }
.workbench-nav__count { min-width: 19px; padding: 1px 6px; border-radius: 10px; background: var(--td-bg-color-component); font-size: 11px; text-align: center; }
.workbench-panel { min-height: 360px; }
.panel-heading { margin-bottom: 16px; }
.panel-search { max-width: 260px; }
.query-form, .workflow-form-grid, .step-config { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.query-form { padding: 16px; border: 1px solid var(--td-border-level-1-color); border-radius: 8px; background: var(--td-bg-color-container); }
label { display: flex; flex-direction: column; gap: 6px; color: var(--td-text-color-secondary); font-size: 12px; min-width: 0; }
.history-toggle, .enable-toggle { justify-content: flex-end; align-self: end; min-height: 32px; }
.query-form__actions { grid-column: 1 / -1; display: flex; gap: 8px; align-items: center; }
.native-input { box-sizing: border-box; min-height: 32px; width: 100%; padding: 4px 9px; border: 1px solid var(--td-border-level-2-color); border-radius: 3px; background: var(--td-bg-color-container); color: var(--td-text-color-primary); font: inherit; }
.result-summary { display: flex; gap: 12px; align-items: center; color: var(--td-text-color-secondary); font-size: 12px; padding: 12px 0 4px; }
.history-note, .eyebrow { color: var(--td-brand-color); font-size: 11px; font-weight: 600; letter-spacing: .02em; }
.result-group { margin-top: 20px; }
.result-group h4, .result-group h5 { margin: 0 0 10px; }
.fact-card, .event-card, .evidence-card, .workflow-step, .report-view, .workflow-runs, .entity-list, .entity-detail {
  border: 1px solid var(--td-border-level-1-color); border-radius: 8px; background: var(--td-bg-color-container);
}
.fact-card, .event-card { padding: 14px 16px; margin: 8px 0; }
.fact-card__heading, .fact-card__meta, .fact-card__actions, .event-card__heading, .event-card__meta, .evidence-card__meta,
.event-row__meta, .workflow-list__actions, .review-actions, .drawer-form__actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.fact-card__heading { justify-content: space-between; }
.fact-expression { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
.fact-expression > span { color: var(--td-text-color-secondary); }
.fact-status { border-radius: 10px; padding: 2px 8px; background: var(--td-bg-color-component); font-size: 11px; }
.fact-status.is-current { color: var(--td-success-color); }
.fact-status.is-corrected { color: var(--td-warning-color); }
.fact-difference { margin: 2px 0 0; padding: 6px 9px; border: 1px solid var(--td-warning-color-focus); border-radius: 5px; color: var(--td-warning-color); font-size: 12px; }
.fact-difference.is-conflict { border-color: var(--td-error-color); color: var(--td-error-color); }
.fact-card__meta, .event-card__meta { margin-top: 8px; color: var(--td-text-color-placeholder); font-size: 11px; }
.fact-card blockquote { border-left: 2px solid var(--td-brand-color); color: var(--td-text-color-secondary); margin: 10px 0; padding-left: 10px; }
.fact-card__correction { margin: 6px 0; color: var(--td-warning-color); font-size: 12px; }
.fact-card__actions { margin-top: 8px; }
.text-link { padding: 0; border: 0; background: none; color: var(--td-brand-color); cursor: pointer; font: inherit; font-size: 12px; text-decoration: none; }
.text-link:hover, a:hover { text-decoration: underline; }
.entity-layout { display: grid; grid-template-columns: minmax(230px, 300px) minmax(0, 1fr); gap: 14px; min-height: 360px; }
.entity-list { padding: 6px; max-height: 660px; overflow: auto; }
.entity-list__item { display: flex; width: 100%; align-items: center; gap: 10px; border: 0; border-radius: 6px; background: transparent; text-align: left; color: inherit; padding: 10px 8px; cursor: pointer; }
.entity-list__item:hover, .entity-list__item.selected { background: var(--td-bg-color-container-hover); }
.entity-list__avatar { display: grid; place-items: center; flex: 0 0 32px; width: 32px; height: 32px; border-radius: 50%; color: var(--td-brand-color); background: var(--td-brand-color-light); font-weight: 700; }
.entity-list__copy { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.entity-list__copy strong, .entity-list__copy small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.entity-list__copy small, .muted { color: var(--td-text-color-placeholder); font-size: 11px; }
.entity-detail { padding: 16px; min-width: 0; }
.entity-detail__header { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; border-bottom: 1px solid var(--td-border-level-1-color); padding-bottom: 12px; }
.entity-detail__header p { color: var(--td-text-color-secondary); margin: 8px 0; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.chip { padding: 3px 8px; border-radius: 12px; background: var(--td-bg-color-component); color: var(--td-text-color-secondary); font-size: 11px; }
.group-heading { margin-bottom: 8px; }
.group-heading h4, .group-heading h5 { margin: 0; }
.event-row { border: 1px solid var(--td-border-level-1-color); border-radius: 9px; margin: 10px 0; overflow: hidden; background: var(--td-bg-color-container); }
.event-row__main { padding: 16px; }
.event-row__title h4 { margin: 4px 0 0; }
.event-row__description { color: var(--td-text-color-secondary); margin: 9px 0; }
.event-heat { display: flex; flex-direction: column; align-items: flex-end; padding: 8px 12px; border-radius: 8px; color: var(--td-warning-color); background: var(--td-warning-color-light); }
.event-heat strong { font-size: 18px; }
.event-heat small { color: var(--td-text-color-placeholder); font-size: 10px; }
.event-row__meta { color: var(--td-text-color-placeholder); font-size: 11px; margin-bottom: 8px; }
.event-row__details { border-top: 1px solid var(--td-border-level-1-color); background: var(--td-bg-color-container-hover); padding: 0 16px 14px; }
.attention-reason { display: flex; justify-content: space-between; gap: 12px; padding: 8px 10px; background: var(--td-bg-color-container); border-radius: 6px; margin: 6px 0; font-size: 12px; }
.attention-reason span { color: var(--td-text-color-secondary); white-space: nowrap; }
.evidence-card { margin: 8px 0; overflow: hidden; }
.evidence-card__main { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; padding: 12px 14px; }
.evidence-card__open { display: flex; flex-direction: column; gap: 4px; padding: 0; border: 0; background: none; color: inherit; text-align: left; cursor: pointer; }
.evidence-card__version { color: var(--td-text-color-placeholder); font-size: 11px; }
.evidence-card__meta { justify-content: flex-end; color: var(--td-text-color-placeholder); font-size: 11px; }
.evidence-snapshot { border-top: 1px solid var(--td-border-level-1-color); padding: 12px 14px; background: var(--td-bg-color-container-hover); }
.evidence-snapshot__meta { display: flex; flex-wrap: wrap; gap: 12px; color: var(--td-text-color-placeholder); font-size: 11px; margin-bottom: 10px; }
.evidence-snapshot__body { max-height: 420px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; color: var(--td-text-color-primary); font: inherit; font-size: 12px; line-height: 1.6; }
.workflow-editor { margin-bottom: 20px; padding: 16px; border: 1px solid var(--td-brand-color-light); border-radius: 10px; background: var(--td-bg-color-container); }
.workflow-editor__header { margin-bottom: 14px; }
.workflow-editor__header h4 { margin: 0; }
.template-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; margin-bottom: 14px; }
.template-card { display: flex; flex-direction: column; gap: 6px; min-height: 76px; padding: 10px; border: 1px solid var(--td-border-level-1-color); border-radius: 7px; background: var(--td-bg-color-container); color: inherit; text-align: left; cursor: pointer; }
.template-card.selected, .template-card:hover { border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
.template-card span { color: var(--td-text-color-secondary); font-size: 11px; line-height: 1.4; }
.workflow-form-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); margin-bottom: 20px; }
.span-two { grid-column: span 2; }
.workflow-steps-heading { margin: 16px 0 10px; }
.workflow-steps-heading p { font-size: 11px; }
.add-step-select { width: 180px; }
.workflow-step { padding: 12px; margin: 10px 0; }
.workflow-step__top { display: grid; grid-template-columns: 30px minmax(100px, 1fr) minmax(150px, 1fr) auto; align-items: center; gap: 8px; margin-bottom: 10px; }
.step-number { display: grid; place-items: center; width: 24px; height: 24px; border-radius: 50%; color: var(--td-brand-color); background: var(--td-brand-color-light); font-size: 11px; }
.step-order { display: flex; align-items: center; gap: 2px; }
.workflow-step__depends { margin-bottom: 10px; }
.step-config { grid-template-columns: repeat(3, minmax(0, 1fr)); padding-top: 10px; border-top: 1px solid var(--td-border-level-1-color); }
.step-condition { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-start; gap: 8px; padding-top: 8px; }
.step-condition :deep(.t-select) { min-width: 140px; flex: 1; }
.workflow-editor__actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
.form-error, .run-error { color: var(--td-error-color); font-size: 12px; white-space: pre-wrap; }
.workflow-layout { display: grid; grid-template-columns: minmax(260px, 340px) minmax(0, 1fr); gap: 14px; margin-top: 14px; }
.workflow-list__item { padding: 12px; margin: 8px 0; border: 1px solid var(--td-border-level-1-color); border-radius: 8px; cursor: pointer; }
.workflow-list__item.selected { border-color: var(--td-brand-color); }
.workflow-list__item p { color: var(--td-text-color-secondary); font-size: 12px; margin: 7px 0; }
.workflow-list__item small, .run-list__copy small { color: var(--td-text-color-placeholder); font-size: 10px; }
.workflow-list__actions { margin-top: 9px; }
.workflow-runs { padding: 12px; min-width: 0; }
.run-list__item { display: flex; width: 100%; gap: 10px; align-items: center; padding: 9px 4px; border: 0; border-bottom: 1px solid var(--td-border-level-1-color); background: transparent; color: inherit; text-align: left; cursor: pointer; }
.run-list__item.selected { color: var(--td-brand-color); }
.status-dot { flex: 0 0 8px; width: 8px; height: 8px; border-radius: 50%; background: var(--td-text-color-placeholder); }
.status-dot.status-completed { background: var(--td-success-color); }
.status-dot.status-failed, .status-dot.status-rejected { background: var(--td-error-color); }
.status-dot.status-running, .status-dot.status-queued, .status-dot.status-paused, .status-dot.status-waiting_ingestion { background: var(--td-warning-color); }
.run-list__copy { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.run-detail { margin-top: 14px; border-top: 1px solid var(--td-border-level-1-color); padding-top: 12px; }
.run-detail__heading h5 { margin: 0; }
.run-step { padding: 9px 0; border-bottom: 1px solid var(--td-border-level-1-color); }
.run-step__header { display: flex; align-items: center; gap: 8px; }
.run-step__header small { color: var(--td-text-color-placeholder); margin-left: auto; }
.run-output { max-height: 220px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; padding: 9px; border-radius: 5px; background: var(--td-bg-color-component); font-size: 11px; }
.review-actions { justify-content: space-between; margin-top: 12px; padding: 10px; border: 1px solid var(--td-warning-color); border-radius: 7px; color: var(--td-warning-color); }
.review-actions > div { display: flex; gap: 8px; }
.report-layout { display: grid; grid-template-columns: minmax(220px, 280px) minmax(0, 1fr); gap: 14px; }
.report-list { display: flex; flex-direction: column; gap: 6px; }
.report-list__item { display: flex; flex-direction: column; gap: 5px; text-align: left; color: inherit; border: 1px solid var(--td-border-level-1-color); border-radius: 7px; padding: 10px; background: var(--td-bg-color-container); cursor: pointer; }
.report-list__item.selected { border-color: var(--td-brand-color); }
.report-list__item small { color: var(--td-text-color-placeholder); }
.report-view { padding: 16px; min-width: 0; }
.report-view > header { border-bottom: 1px solid var(--td-border-level-1-color); padding-bottom: 12px; }
.report-view > header p { color: var(--td-text-color-placeholder); font-size: 11px; margin: 5px 0 0; }
.report-markdown { line-height: 1.7; overflow-wrap: anywhere; }
.report-markdown :deep(table) { border-collapse: collapse; width: 100%; }
.report-markdown :deep(th), .report-markdown :deep(td) { border: 1px solid var(--td-border-level-1-color); padding: 6px 8px; }
.report-citations { border-top: 1px solid var(--td-border-level-1-color); padding-top: 12px; }
.report-citations h5 { margin: 0 0 10px; }
.citation-card { border: 1px solid var(--td-border-level-1-color); border-radius: 7px; padding: 10px; margin: 7px 0; font-size: 12px; }
.citation-card p { color: var(--td-text-color-secondary); }
.citation-card small { color: var(--td-text-color-placeholder); }
.citation-card a { color: var(--td-brand-color); margin-left: 8px; }
.drawer-form { display: flex; flex-direction: column; gap: 14px; }
.drawer-form > p { color: var(--td-text-color-secondary); margin-top: 0; }
.drawer-form__actions { justify-content: flex-end; margin-top: 6px; }
@media (max-width: 900px) {
  .entity-layout, .workflow-layout, .report-layout { grid-template-columns: 1fr; }
  .entity-list { max-height: 260px; }
  .template-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .workflow-form-grid, .step-config, .query-form { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 620px) {
  .knowledge-workbench { padding: 14px 10px 24px; }
  .workbench-header, .panel-heading { align-items: flex-start; flex-direction: column; }
  .workbench-header__actions { flex-wrap: wrap; }
  .query-form, .workflow-form-grid, .step-config { grid-template-columns: 1fr; }
  .span-two { grid-column: auto; }
  .workflow-step__top { grid-template-columns: 26px 1fr; }
  .workflow-step__top :deep(.t-select), .step-order { grid-column: 2; }
  .step-order { justify-content: flex-end; }
  .template-grid { grid-template-columns: 1fr; }
  .event-row__title { align-items: flex-start; }
  .report-view > header { align-items: flex-start; flex-direction: column; }
}
</style>
