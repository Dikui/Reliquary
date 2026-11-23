export type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type RecordValue = Record<string, unknown>;
type Shape = string | readonly [Shape] | { [key: string]: Shape };

export class QueryError extends Error {
  constructor(public code: string, message: string, public retryable = false, public details?: unknown) {
    super(message);
  }
}

export function record(value: unknown): RecordValue {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new QueryError('invalid_response', '服务返回的数据格式不符合查询契约');
  }
  return value as RecordValue;
}

export function createRedactor(secrets: string[]) {
  return (text: string) => {
    for (const secret of secrets.filter(Boolean).sort((a, b) => b.length - a.length)) text = text.split(secret).join('[已隐藏]');
    return text
      .replace(/\b(Bearer|Basic)\s+[A-Za-z0-9+/_.=~-]+/gi, '$1 [已隐藏]')
      .replace(/((?:authorization|api[_-]?key|access[_-]?token|password)\s*["']?\s*[:=]\s*["']?)[^\s"',;&}]+/gi, '$1[已隐藏]');
  };
}

export function redactValue<T>(value: T, redact: (text: string) => string): T {
  if (typeof value === 'string') return redact(value) as T;
  if (Array.isArray(value)) return value.map(item => redactValue(item, redact)) as T;
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(Object.entries(value).map(([key, item]) => [
    key, /^(authorization|api[_-]?key|access[_-]?token|password)$/i.test(key) ? '[已隐藏]' : redactValue(item, redact),
  ])) as T;
}

function fields(names: string): { [key: string]: Shape } {
  return Object.fromEntries(names.split(' ').map(name => [name, 'scalar']));
}

const certification = {
  ...fields('name owner_id standard year certification_object cell_part_id region agency progress notes start_date planned_date obtained_date issued_date validity number cloud_url'),
  conditions: ['condition'] as const,
};

// These shapes describe business output only. Unknown fields never pass through.
const shapes: Record<string, Shape> = {
  location: { ...fields('paragraph start end body_hash mapping'), heading_path: ['scalar'] },
  evidence: { locations: ['location'], speaker: fields('entity_id name selection') },
  source: { ...fields('type import_plan_key plan_revision file_name file_sha256 worksheet agent_name import_batch_id file_date'), cells: ['scalar'] },
  snapshot: { ...fields('snapshot_id operation_log_id changeset_id effective_at recorded_at'), source: 'source' },
  condition: { module_type: 'scalar', part_ids: ['scalar'] },
  option: { key: 'scalar', name: 'scalar', aliases: ['scalar'] },
  kind: { key: 'scalar', name: 'scalar', parent_kinds: ['scalar'] },
  field: { ...fields('key name type unit archived'), options: ['scalar'], kinds: ['scalar'], product_line_ids: ['scalar'] },
  summary: { overview: 'scalar', points: ['scalar'], key_terms: ['scalar'] },
  entity: {
    ...fields('id library_id canonical_name name entity_type kind kind_label code description revision archived_at created_at updated_at'),
    aliases: ['scalar'], attributes: 'attributes',
  },
  requirement: {
    ...fields('id name list_name name_zh name_en file_category stage_label due_text draft_due_stage final_due_stage owner priority source_list source_version'),
    markets: 'scalarMap',
  },
  certification,
  certificationFact: { ...certification, status: 'scalar' },
  sale: { ...fields('region status planned_eom notes raw'), conditions: ['condition'] },
  parameter: { value: 'parameterValue', conditions: ['condition'] },
  relation: {
    ...fields('id library_id subject_id object_id relation_type version valid_from valid_to created_at updated_at'),
    attributes: fields('module_type'),
  },
  changeOperation: {
    ...fields('id type entity_id revision alias relation_id valid_to effective_at candidate_entity_id'),
    entity: { ...fields('name kind code description effective_at'), aliases: ['scalar'], attributes: 'attributes',
      name_confirmations: [fields('other_entity_id other_kind name')] },
    patch: { ...fields('revision name kind code description archived effective_at'), aliases: ['scalar'], attributes: 'attributes',
      name_confirmations: [fields('other_entity_id other_kind name')] },
    relation: 'relation', sale_statuses: ['sale'], parameter_values: 'parameters', certification: 'certification',
    issues: [fields('type message')],
  },
  changeDecision: { ...fields('item_id action target_entity_id target_revision'),
    name_confirmations: [fields('other_entity_id other_kind name')], replacement: 'changeOperation' },
  changeConflict: { ...fields('type message'), candidates: [fields('other_entity_id other_kind name')], manual_before: 'attributes' },
  fact: {
    ...fields('entity_id relation_id field partial_options origin origin_label region_name'),
    value: 'factValue', conditions: ['condition'], evidence: 'snapshot',
  },
  document: {
    ...fields('id library_id file_id owner_id requirement_id certificate_number attachment_id cloud_url entry_id ordinal version_label stage is_current origin occurred_at created_at changeset_id parse_status'),
    requirement_ids: ['scalar'], conditions: ['condition'], source: 'source',
  },
  checklist: {
    ...fields('document_name ambiguous status status_label has_file owner_id origin origin_label partial_options'),
    requirement: 'entity', document: 'document', conditions: ['condition'],
  },
  shelf: {
    ...fields('current_stage stage_owner_id'),
    composition: [{ module_type: 'scalar', optional: 'scalar', parts: ['entity'] }],
    sale_statuses: ['sale'], parameters: 'parameters',
    certifications: [{ entity: 'entity', certification: 'certification', status: 'scalar', standard_name: 'scalar', region_name: 'scalar', origin: 'scalar' }],
    checklist: ['checklist'], checklist_summary: {
      ...fields('model_count models_with_gaps'), counts: 'scalarMap',
      models: [{ ...fields('model_id current_stage has_gaps'), counts: 'scalarMap' }],
    },
    documents: [{ entity: 'entity', requirement: 'entity', requirements: ['entity'], document: 'document' }],
    other_documents: [{ entity: 'entity', owner: 'entity', document: 'document', origin: 'scalar', conditions: ['condition'] }],
  },
  node: {
    ...fields('as_of recorded current_stage partial_options'), entity: 'entity', shelf: 'shelf', facts: ['fact'], matching_conditions: [['condition']],
  },
  snippet: {
    ...fields('context chunk_type chunk_id content chunk_index score match_type'),
    locations: ['location'], context_locations: ['location'],
  },
  entry: {
    ...fields('id entry_id library_id library_name library_directory title source source_ref identifier url original_url author publisher published_at collected_at content_type acquisition cannot_be_fact source_unverified has_update latest_entry_id historical_match'),
    summary: 'summary', entities: ['entity'], topics: [fields('id name')], snippets: ['snippet'],
    shelf_document: fields('file_id version_id version_label ordinal is_current'),
    similar_reports: ['entry'], other_version_matches: ['entry'],
  },
  library: {
    ...fields('id name directory description entity_mode shelf_enabled updated_at'),
    content_types: [fields('name description enabled')],
    relation_types: [{ ...fields('name description hierarchical enabled'), subject_kinds: ['scalar'], object_kinds: ['scalar'] }],
    entity_schema: { kinds: ['kind'], fields: ['field'] },
    shelf_config: { regions: ['option'], markets: ['option'], stages: ['option'], module_types: ['option'], standards: ['option'] },
  },
  gap: {
    ...fields('model_id model_name current_stage current_stage_unset status status_label has_file document_name ambiguous owner_id origin origin_label partial_options'),
    requirement: 'requirement', document: 'document', conditions: ['condition'],
  },
  guide: {
    ...fields('library_id library_updated_at as_of rules'),
    values: {
      kinds: ['kind'], fields: ['field'], product_lines: ['entity'], regions: ['option'], markets: ['option'],
      stages: ['option'], module_types: ['option'], standards: ['option'], sale_statuses: ['scalar'],
      requirement_lists: [{ name: 'scalar', total: 'scalar', product_lines: [fields('id name')], platforms: [fields('id name')] }],
      requirements: { items: ['requirement'], ...fields('total truncated') },
    },
    gaps: {
      ...fields('node_id kind model_count current_stage_unset'),
      documents: { items: ['gap'], ...fields('total truncated') },
      no_file_documents: { items: ['gap'], ...fields('total truncated') },
      sale_statuses: { items: [fields('model_id model_name region region_name')], ...fields('total truncated') },
      pending_changesets: { items: [{ ...fields('id revision updated_at'), source: 'source' }], ...fields('total truncated') },
    },
  },
};

const outputs: Record<string, Shape> = {
  list_libraries: { libraries: ['library'] },
  search: { ...fields('vector_unavailable rerank_status excluded_missing_published_at'), results: ['entry'] },
  get_entry: {
    ...fields('library_id library_name source_ref identifier has_update latest_entry_id body original_url parse_status cannot_be_fact source_unverified acquisition offset length total_length next_offset'),
    header: { ...fields('id library_id library title url publisher author published_at collected_at content_type source_unverified acquisition cannot_be_fact'), summary: 'summary', entities: ['scalar'], topics: ['scalar'] },
  },
  list_entities: { entities: ['entity'] },
  get_entity: { entity: 'entity', entries: ['entry'], library_groups: [{ ...fields('library_id library_name count'), entries: ['entry'] }], shelf: 'shelf' },
  graph_search: {
    ...fields('status mode hops expansion_limit truncated'), libraries: ['scalar'], candidates: 'candidates', entities: ['entity'],
    edges: [{ ...fields('id subject_id object_id library_id relation_type version source_count'), attributes: fields('module_type'),
      sources: [{ ...fields('entry_id title library_id library_name library_directory published_at source_unverified acquisition'), evidence: ['evidence'] }] }],
    paths: [{ entity_ids: ['scalar'], relation_ids: ['scalar'] }], documents: ['entry'],
    truncations: [fields('hop entity_id reason')], pending_sync: fields('global involved'),
  },
  shelf_find: { items: ['entity'], ...fields('total truncated') },
  shelf_get: 'node',
  shelf_query: {
    items: ['node'], ...fields('total truncated as_of detail'), parameter_missing: [fields('field name models')],
    facets: {
      sale_statuses: [fields('region region_name status models conditional_models')],
      composition: [fields('module_type part_id part_name models')],
      parameters: [fields('field name models missing min max')],
      certifications: [fields('standard standard_name progress region models')],
    },
  },
  shelf_guide: 'guide',
  shelf_changeset: {
    ...fields('id library_id revision status deferred_changeset_id created_at updated_at confirmed_at'), source: 'source',
    items: [{ ...fields('id entity_id'), operation: 'changeOperation', before: 'changeSnapshot', after: 'changeSnapshot',
      conflicts: ['changeConflict'], decision: 'changeDecision', warnings: [{ ...fields('type message'), model_ids: ['scalar'] }] }],
  },
};

export class OutputProjector {
  private libraryFields = new Map<string, RecordValue[]>();
  constructor(private libraries: string[], readonly redact: (text: string) => string) {}

  assertLibraries(): void {
    if (this.libraries.some(id => !this.libraryFields.has(id))) {
      throw new QueryError('library_unavailable', '配置的目标库不存在或无法读取');
    }
  }

  project(tool: string, value: unknown): { data: Json; warnings: string[] } {
    const warnings = new Set<string>();
    if (tool === 'list_libraries') {
      const rows = record(value).libraries;
      if (!Array.isArray(rows)) throw new QueryError('invalid_response', '库列表格式无效');
      this.libraryFields.clear();
      for (const row of rows) {
        const library = record(row);
        if (this.libraries.includes(String(library.id))) {
          const schema = record(library.entity_schema ?? {});
          this.libraryFields.set(String(library.id), Array.isArray(schema.fields) ? schema.fields.map(record) : []);
        }
      }
      this.assertLibraries();
      value = { libraries: rows.filter(row => this.libraries.includes(String(record(row).id))) };
    }
    const shape = outputs[tool];
    const data = !shape || ['shelf_guide', 'shelf_changeset'].includes(tool)
      ? this.generic(value, warnings) : this.visit(value, shape, '', warnings);
    return { data: data ?? null, warnings: [...warnings] };
  }

  private generic(value: unknown, warnings: Set<string>): Json | undefined {
    if (value === null || typeof value === 'boolean' || typeof value === 'number') return value;
    if (typeof value === 'string') return this.redact(value);
    if (Array.isArray(value)) {
      const output: Json[] = [];
      for (const item of value) {
        const result = this.generic(item, warnings);
        if (result !== undefined) output.push(result);
      }
      return output;
    }
    if (!value || typeof value !== 'object') return undefined;
    const input = record(value);
    if (typeof input.library_id === 'string' && !this.libraries.includes(input.library_id)) {
      warnings.add('已过滤目标库范围以外的记录，结果可能不完整');
      return undefined;
    }
    const output: Record<string, Json> = {};
    for (const [key, item] of Object.entries(input)) {
      if (/authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|password|secret/i.test(key)) continue;
      const result = this.generic(item, warnings);
      if (result !== undefined) output[key] = result;
    }
    return output;
  }

  private visit(value: unknown, shape: Shape, library: string, warnings: Set<string>, parent: RecordValue = {}): Json | undefined {
    if (value === undefined) return undefined;
    if (value === null) return null;
    if (Array.isArray(shape)) {
      if (!Array.isArray(value)) throw new QueryError('invalid_response', '服务返回的列表格式无效');
      const output: Json[] = [];
      for (const item of value) {
        if (shape[0] === 'fact') {
          const field = record(item).field;
          const known = ['sale_statuses', 'certification', 'requirement', 'belongs_to', 'composed_of', 'has_certification'];
          if (!known.includes(String(field)) && !(this.libraryFields.get(library) ?? []).some(definition => definition.key === field)) {
            warnings.add('部分字段未包含在本次库配置中，已省略');
            continue;
          }
        }
        const projected = this.visit(item, shape[0], library, warnings);
        if (projected !== undefined) output.push(projected);
      }
      return output;
    }
    if (shape === 'scalar') {
      if (typeof value === 'string') return this.redact(value);
      if (typeof value === 'boolean' || typeof value === 'number') return value;
      throw new QueryError('invalid_response', '服务返回的字段格式无效');
    }
    if (shape === 'parameterValue') {
      if (Array.isArray(value)) return this.visit(value, ['scalar'], library, warnings);
      return this.visit(value, typeof value === 'object' ? fields('min max') : 'scalar', library, warnings);
    }
    if (shape === 'changeSnapshot') {
      return this.visit(value, 'subject_id' in record(value) ? 'relation' : 'entity', library, warnings);
    }
    if (shape === 'factValue') {
      const field = String(parent.field);
      const special: Record<string, Shape> = { sale_statuses: 'sale', certification: 'certificationFact', requirement: 'requirement',
        belongs_to: 'relation', composed_of: 'relation', has_certification: 'relation' };
      return this.visit(value, special[field] ?? 'parameterValue', library, warnings);
    }
    if (shape === 'attributes') {
      const attributes: Record<string, Shape> = { current_stage: 'scalar' };
      for (const field of this.libraryFields.get(library) ?? []) {
        const special: Record<string, Shape> = { shelf_statuses: ['sale'], shelf_certification: 'certification', shelf_requirement: 'requirement', shelf_rules: [['condition']] };
        attributes[String(field.key)] = Array.isArray(field.product_line_ids) && field.product_line_ids.length
          ? ['parameter'] : special[String(field.type)] ?? 'parameterValue';
      }
      return this.visit(value, attributes, library, warnings);
    }
    if (shape === 'scalarMap' || shape === 'parameters' || shape === 'candidates') {
      const child: Shape = shape === 'scalarMap' ? 'scalar' : shape === 'parameters' ? ['parameter'] : ['entity'];
      const result: Record<string, Json> = {};
      for (const [key, item] of Object.entries(record(value))) {
        if (shape === 'parameters' && !(this.libraryFields.get(library) ?? []).some(field => field.key === key)) continue;
        const projected = this.visit(item, child, library, warnings);
        if (projected !== undefined) result[this.redact(key)] = projected;
      }
      return result;
    }
    if (typeof shape === 'string') return this.visit(value, shapes[shape], library, warnings, parent);
    if (!shape) throw new QueryError('invalid_response', '查询输出契约缺失');
    const input = record(value);
    const ownLibrary = input.library_id;
    if (typeof ownLibrary === 'string') {
      if (!this.libraries.includes(ownLibrary)) {
        warnings.add('已过滤目标库范围以外的嵌套记录，结果可能不完整');
        return undefined;
      }
      library = ownLibrary;
    }
    if (input.entity && typeof input.entity === 'object') {
      library = String(record(input.entity).library_id ?? library);
      if (!this.libraries.includes(library)) {
        warnings.add('已过滤目标库范围以外的嵌套记录，结果可能不完整');
        return undefined;
      }
    }
    const output: Record<string, Json> = {};
    for (const [key, child] of Object.entries(shape)) {
      if (/^(authorization|api[_-]?key|access[_-]?token|password)$/i.test(key)) continue;
      const projected = this.visit(input[key], child, library, warnings, input);
      if (projected !== undefined) output[key] = projected;
    }
    return output;
  }
}
