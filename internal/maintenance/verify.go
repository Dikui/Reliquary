package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type TableManifest struct {
	Rows        int64               `json:"rows"`
	SHA256      string              `json:"sha256"`
	Columns     []string            `json:"columns"`
	Regenerated bool                `json:"regenerated"`
	Data        []map[string]any    `json:"data,omitempty"`
	JSONNulls   map[string][]string `json:"json_nulls,omitempty"`
}
type Retrieval struct {
	Query string   `json:"query"`
	IDs   []string `json:"ids"`
}
type Manifest struct {
	FormatVersion int                      `json:"format_version"`
	Tables        map[string]TableManifest `json:"tables"`
	Files         map[string][]File        `json:"files"`
	Retrieval     []Retrieval              `json:"retrieval"`
	Regenerated   []string                 `json:"regenerated"`
	Vectors       []VectorProfile          `json:"vectors"`
	Required      []Requirement            `json:"required_credentials"`
}
type Verification struct {
	OK             bool             `json:"ok"`
	Differences    []string         `json:"differences"`
	Repaired       []string         `json:"repaired"`
	PendingVectors map[string]int64 `json:"pending_vectors,omitempty"`
}

func BuildManifest(ctx context.Context, db *gorm.DB, root, uploads string) (*Manifest, error) {
	snapshot, err := Capture(ctx, db)
	if err != nil {
		return nil, err
	}
	manifest := &Manifest{FormatVersion: 1, Tables: map[string]TableManifest{}, Files: map[string][]File{}, Retrieval: []Retrieval{}, Required: snapshot.Required, Regenerated: []string{"all chunks and vectors, search sequence IDs and ranking scores, live queue leases, excluded authentication secrets, maintenance pause and scheduling checkpoints"}}
	columns, err := Schema(ctx, db)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, column := range columns {
		present[column.Table] = true
	}
	for _, table := range sortedTables() {
		if !present[table] {
			continue
		}
		entry := TableManifest{Columns: []string{}}
		if err := db.WithContext(ctx).Table("public." + quote(table)).Count(&entry.Rows).Error; err != nil {
			return nil, err
		}
		rows, ok := snapshot.Tables[table]
		if !ok {
			entry.Regenerated = true
		} else {
			for column, category := range Rules[table].Columns() {
				if category != Regenerated && !secretColumn(table, column) {
					entry.Columns = append(entry.Columns, column)
				}
			}
			sort.Strings(entry.Columns)
			portable := canonicalRows(table, rows)
			data, _ := json.Marshal(portable)
			entry.SHA256 = sum(data)
			entry.Rows = int64(len(rows))
			entry.JSONNulls = snapshot.JSONNulls[table]
			if table == "models" || table == "data_sources" || table == "chatgpt_accounts" || table == "tenants" || table == "chatgpt_policy" || table == "knowledge_bases" || table == "operation_logs" || table == "embedding_rebuild_runs" || table == "embedding_rebuild_items" || table == "entity_embedding_rebuild_runs" || table == "task_jobs" || table == "tenant_api_keys" || inactiveCredentialTable(table) {
				entry.Data = rows
			}
		}
		manifest.Tables[table] = entry
	}
	extensionTables, err := extensionTableManifests(ctx, db)
	if err != nil {
		return nil, err
	}
	for table, entry := range extensionTables {
		manifest.Tables[table] = entry
	}
	for name, path := range map[string]string{"vault": root, "uploads": uploads} {
		files, err := Files(path)
		if err != nil {
			return nil, err
		}
		manifest.Files[name] = files
	}
	for _, query := range []string{"maintenancearchive", "observations", "reference"} {
		var ids []string
		if err := db.WithContext(ctx).Raw(`SELECT DISTINCT id FROM (
 SELECT k.id FROM public.knowledges k WHERE k.deleted_at IS NULL AND k.parse_status='completed' AND k.enable_status='enabled' AND k.search_seq_id @@@ paradedb.boolean(should => ARRAY[paradedb.match_disjunction('title',?),paradedb.match_disjunction('description',?),paradedb.match_disjunction('search_text',?)])
 UNION ALL SELECT c.knowledge_id AS id FROM public.chunks c JOIN public.knowledges k ON k.id=c.knowledge_id WHERE c.deleted_at IS NULL AND c.is_enabled AND k.deleted_at IS NULL AND k.parse_status='completed' AND k.enable_status='enabled' AND c.seq_id @@@ paradedb.boolean(should => ARRAY[paradedb.match_disjunction('content',?),paradedb.match_disjunction('context_header',?),paradedb.match_disjunction('search_title',?)])
) matched ORDER BY id LIMIT 20`, query, query, query, query, query, query).Scan(&ids).Error; err != nil {
			return nil, err
		}
		if ids == nil {
			ids = []string{}
		}
		manifest.Retrieval = append(manifest.Retrieval, Retrieval{Query: query, IDs: ids})
	}
	manifest.Vectors, err = VectorProfiles(ctx, db)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

func canonicalRows(table string, rows []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		copy := map[string]any{}
		for key, value := range row {
			copy[key] = value
		}
		switch table {
		case "models", "data_sources":
			if table == "models" {
				if parameters, ok := copy["parameters"].(map[string]any); ok {
					cloned := map[string]any{}
					for k, v := range parameters {
						cloned[k] = v
					}
					cloned["extra_config"] = nil
					if extra, ok := parameters["extra_config"].(map[string]any); ok {
						extras := map[string]any{}
						for k, v := range extra {
							if k != "maintenance_required_fields" && k != "maintenance_original_status" && k != "maintenance_original_updated_at" {
								extras[k] = v
							}
						}
						if len(extras) == 0 {
							cloned["extra_config"] = nil
						} else {
							cloned["extra_config"] = extras
						}
					}
					copy["parameters"] = cloned
				}
			}
			if table == "data_sources" {
				if config, ok := copy["config"].(map[string]any); ok {
					cloned := map[string]any{}
					for key, value := range config {
						if key != "_reliquary_maintenance_required_fields" && key != "_reliquary_maintenance_original_status" && key != "_reliquary_maintenance_original_updated_at" {
							cloned[key] = value
						}
					}
					copy["config"] = cloned
				}
			}
		case "task_jobs":
			if (copy["state"] == "active" || copy["state"] == "pending") && copy["completed_at"] == nil {
				for _, key := range []string{"claim_token", "worker_pool", "lease_until", "started_at", "updated_at"} {
					delete(copy, key)
				}
				copy["state"] = "pending"
			}
		case "maintenance_state":
			delete(copy, "backup_directory")
		}
		result = append(result, copy)
	}
	sortRows(result)
	return result
}

// Startup recovery reschedules interrupted work without spending a retry. Its
// new process_at is allowed only when this exact original row was still active;
// existing pending, scheduled and finished jobs retain their original schedule.
func comparableTaskRows(expected, actual []map[string]any) ([]map[string]any, []map[string]any) {
	left, right := canonicalRows("task_jobs", expected), canonicalRows("task_jobs", actual)
	interrupted := map[string]map[string]any{}
	for _, row := range expected {
		if row["state"] == "active" && row["completed_at"] == nil {
			interrupted[fmt.Sprint(row["id"])] = row
		}
	}
	pending := map[string]bool{}
	for _, row := range actual {
		if row["state"] == "pending" && row["completed_at"] == nil {
			pending[fmt.Sprint(row["id"])] = true
		}
	}
	for _, row := range right {
		id := fmt.Sprint(row["id"])
		if original, found := interrupted[id]; found && pending[id] {
			row["process_at"] = original["process_at"]
		}
	}
	sortRows(right)
	return left, right
}

// Credential exclusions permit only the documented pause and the restoration
// of that row's recorded original status. All other business fields stay exact.
func comparableCredentialRows(table string, expected, actual []map[string]any, requirements []Requirement) ([]map[string]any, []map[string]any) {
	if table == "task_jobs" {
		return comparableTaskRows(expected, actual)
	}
	if table != "models" && table != "data_sources" && table != "chatgpt_accounts" && table != "tenants" {
		return canonicalRows(table, expected), canonicalRows(table, actual)
	}
	allowed := map[string]bool{}
	for _, requirement := range requirements {
		if requirement.Kind == table {
			if table != "tenants" {
				allowed[requirement.ID] = true
			} else {
				for _, field := range requirement.Fields {
					if parserCredentialRequirement(field) {
						allowed[requirement.ID] = true
					}
				}
			}
		}
	}
	byID := map[string]map[string]any{}
	for _, row := range expected {
		byID[fmt.Sprint(row["id"])] = row
	}
	left := canonicalRows(table, expected)
	right := canonicalRows(table, actual)
	for _, row := range right {
		id := fmt.Sprint(row["id"])
		wanted, found := byID[id]
		if !found || !allowed[id] {
			continue
		}
		if table == "tenants" {
			row["updated_at"] = wanted["updated_at"]
			continue
		}
		original := wanted["status"]
		if table == "models" {
			if parameters, ok := wanted["parameters"].(map[string]any); ok {
				if extra, ok := parameters["extra_config"].(map[string]any); ok && nonempty(extra["maintenance_original_status"]) {
					original = extra["maintenance_original_status"]
				}
			}
		}
		if table == "data_sources" {
			if config, ok := wanted["config"].(map[string]any); ok && nonempty(config["_reliquary_maintenance_original_status"]) {
				original = config["_reliquary_maintenance_original_status"]
			}
		}
		pause := map[string]string{"models": "needs_configuration", "data_sources": "paused", "chatgpt_accounts": "needs_login"}[table]
		status := fmt.Sprint(row["status"])
		valid := status == fmt.Sprint(wanted["status"]) || status == fmt.Sprint(original) || status == pause
		if table == "chatgpt_accounts" && status == "connected" {
			valid = true // An explicitly repeated login creates a new session.
		}
		if !valid {
			continue
		}
		row["status"] = wanted["status"]
		row["updated_at"] = wanted["updated_at"]
		if table == "chatgpt_accounts" {
			for _, field := range []string{"expires_at", "earliest_refresh_at"} {
				row[field] = wanted[field]
			}
		}
	}
	sortRows(right)
	return left, right
}

func VerifyReplica(ctx context.Context, db *gorm.DB, root, uploads string, repair bool) (*Verification, error) {
	result := &Verification{OK: true, Differences: []string{}, Repaired: []string{}}
	snapshot, err := Capture(ctx, db)
	if err != nil {
		return result, err
	}
	if saved, err := Load(root); err != nil {
		result.Differences = append(result.Differences, "replica manifest or asset integrity: "+err.Error())
	} else {
		before, _ := json.Marshal(saved.JSONNulls)
		after, _ := json.Marshal(snapshot.JSONNulls)
		if !bytes.Equal(before, after) {
			result.Differences = append(result.Differences, "replica JSONB null types differ")
		}
		if uploads != "" && saved.ActiveUploads != nil {
			current, err := Files(uploads)
			if err != nil {
				return result, err
			}
			if !sameUploadInventory(*saved.ActiveUploads, current) {
				result.Differences = append(result.Differences, "active upload inventory differs from current uploads")
			}
		}
	}
	if uploads != "" {
		files, err := Files(uploads)
		if err != nil {
			return result, err
		}
		for _, file := range files {
			mirror, err := SafePath(filepath.Join(root, ".reliquary/resources"), file.Path)
			if err != nil {
				return result, err
			}
			data, err := os.ReadFile(mirror)
			if err != nil || sum(data) != file.SHA256 {
				result.Differences = append(result.Differences, "upload replica differs: "+file.Path)
			}
		}
	}
	expected, err := replicaFiles(snapshot)
	if err != nil {
		return result, err
	}
	for path, data := range expected {
		target, err := SafePath(filepath.Join(root, ".reliquary"), path)
		if err != nil {
			return result, err
		}
		actual, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(actual, data) {
			result.Differences = append(result.Differences, ".reliquary/"+path)
		}
	}
	// The log is never rewritten. A corrupted or altered previous record is a
	// hard failure even when repair is explicitly requested.
	if err := checkLogs(filepath.Join(root, ".reliquary", "operation_logs.jsonl"), snapshot.Tables["operation_logs"]); err != nil {
		result.Differences = append(result.Differences, err.Error())
		if repair {
			path := filepath.Join(root, ".reliquary", "operation_logs.jsonl")
			if _, statErr := os.Stat(path); statErr == nil {
				quarantine := filepath.Join(root, ".reliquary", "quarantine", fmt.Sprintf("operation_logs-%d.corrupt.jsonl", time.Now().UnixNano()))
				if err := os.MkdirAll(filepath.Dir(quarantine), 0700); err != nil {
					return result, err
				}
				if err := os.Rename(path, quarantine); err != nil {
					return result, err
				}
				result.Repaired = append(result.Repaired, "preserved corrupt log at "+quarantine)
			}
		}
	}
	if repair && len(result.Differences) > 0 {
		if err := WriteSnapshot(root, uploads, snapshot); err != nil {
			return result, err
		}
		result.Repaired = append(result.Repaired, result.Differences...)
		result.Differences = []string{}
	}
	sort.Strings(result.Differences)
	result.OK = len(result.Differences) == 0
	return result, nil
}

func checkLogs(path string, expected []map[string]any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rows := []map[string]any{}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		row, err := jsonRow(line)
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	wanted := append([]map[string]any{}, expected...)
	sortRows(wanted)
	actual, _ := json.Marshal(rows)
	canonical, _ := json.Marshal(wanted)
	if !bytes.Equal(actual, canonical) {
		return fmt.Errorf("append-only operation_logs.jsonl differs from database")
	}
	return nil
}

func CompareManifest(expected, actual *Manifest) []string {
	return CompareManifestWithOptions(expected, actual, false, false)
}

type CompareOptions struct {
	AllowModelChange           bool
	AllowOperationLogAppend    bool
	AllowReconfigurationAppend bool
}

// Additional rows in the append-only operation log and explicitly changed
// selected embedding configuration are opt-in exceptions. Original log rows
// and every other persistent table remain exact.
func CompareManifestWithOptions(expected, actual *Manifest, allowModelChange, allowLogAppend bool) []string {
	return CompareManifestWithSettings(expected, actual, CompareOptions{AllowModelChange: allowModelChange, AllowOperationLogAppend: allowLogAppend})
}

func CompareManifestWithSettings(expected, actual *Manifest, options CompareOptions) []string {
	allowModelChange := options.AllowModelChange
	allowLogAppend := options.AllowOperationLogAppend || options.AllowReconfigurationAppend
	diffs := []string{}
	policyAuditValid := false
	if options.AllowReconfigurationAppend {
		policyAuditValid = verifyPolicyProbeChain(expected, actual)
		if !policyAuditValid {
			diffs = append(diffs, "chatgpt policy probe audit chain")
		}
	}
	embeddingIDs := map[string]bool{}
	currentEmbeddingIDs := map[string]bool{}
	if allowModelChange {
		for _, manifest := range []*Manifest{expected, actual} {
			for _, row := range manifest.Tables["knowledge_bases"].Data {
				id := fmt.Sprint(row["embedding_model_id"])
				if tiers, ok := row["model_tiers"].(map[string]any); ok && nonempty(tiers["embedding"]) {
					id = fmt.Sprint(tiers["embedding"])
				}
				if id != "" && id != "<nil>" {
					embeddingIDs[id] = true
					if manifest == actual {
						currentEmbeddingIDs[id] = true
					}
				}
			}
		}
	}
	newRuns := map[string]bool{}
	if allowModelChange {
		oldRuns := map[string]bool{}
		for _, row := range expected.Tables["embedding_rebuild_runs"].Data {
			oldRuns[fmt.Sprint(row["id"])] = true
		}
		for _, row := range actual.Tables["embedding_rebuild_runs"].Data {
			id := fmt.Sprint(row["id"])
			if !oldRuns[id] && currentEmbeddingIDs[fmt.Sprint(row["model_id"])] {
				newRuns[id] = true
			}
		}
	}
	if expected.FormatVersion != actual.FormatVersion {
		diffs = append(diffs, "manifest format version")
	}
	for table, wanted := range expected.Tables {
		current, ok := actual.Tables[table]
		if !ok {
			diffs = append(diffs, "missing table "+table)
			continue
		}
		if wanted.Regenerated {
			continue
		}
		matches := wanted.SHA256 == current.SHA256
		rowsMatch := wanted.Rows == current.Rows
		if (wanted.Data != nil || wanted.Rows == 0) && current.Data != nil {
			left, right := comparableCredentialRows(table, wanted.Data, current.Data, expected.Required)
			if options.AllowReconfigurationAppend && table == "tenant_api_keys" {
				right = reconfiguredAPIKeyRows(left, right, expected.Required)
				rowsMatch = len(left) == len(right)
			}
			if allowModelChange && table == "knowledge_bases" {
				right = normalizeEmbeddingSelection(left, right)
			}
			if allowModelChange && table == "models" {
				right = normalizeEmbeddingModels(left, right, embeddingIDs)
				rowsMatch = len(left) == len(right)
			}
			if allowLogAppend && table == "operation_logs" {
				right = keepExpectedRows(left, right)
				rowsMatch = len(left) == len(right)
			}
			if options.AllowReconfigurationAppend && inactiveCredentialTable(table) {
				if table == "chatgpt_login_attempts" {
					right = keepExpectedMetadataOccurrences(left, right)
				} else {
					right = keepExpectedRows(left, right)
				}
				rowsMatch = len(left) == len(right)
			}
			if options.AllowReconfigurationAppend && table == "chatgpt_policy" && policyAuditValid {
				right = left
				rowsMatch = true
			}
			if allowModelChange && (table == "embedding_rebuild_runs" || table == "embedding_rebuild_items" || table == "entity_embedding_rebuild_runs") {
				right = normalizeRebuildAppend(table, left, right, currentEmbeddingIDs, newRuns)
				rowsMatch = len(left) == len(right)
			}
			before, _ := json.Marshal(left)
			after, _ := json.Marshal(right)
			matches = bytes.Equal(before, after)
		}
		if !rowsMatch || !matches {
			diffs = append(diffs, "table "+table)
		}
		beforeNulls, _ := json.Marshal(wanted.JSONNulls)
		nulls := current.JSONNulls
		if allowLogAppend && table == "operation_logs" {
			nulls = map[string][]string{}
			for id := range wanted.JSONNulls {
				nulls[id] = current.JSONNulls[id]
			}
			// A prior SQL NULL changed to literal JSON null must also fail.
			for _, row := range wanted.Data {
				id := fmt.Sprint(row["id"])
				if current.JSONNulls[id] != nil {
					nulls[id] = current.JSONNulls[id]
				}
			}
			if len(nulls) == 0 {
				nulls = nil
			}
		}
		afterNulls, _ := json.Marshal(nulls)
		if !bytes.Equal(beforeNulls, afterNulls) {
			diffs = append(diffs, "table JSONB null types "+table)
		}
	}
	for table := range actual.Tables {
		if _, ok := expected.Tables[table]; !ok {
			diffs = append(diffs, "unexpected table "+table)
		}
	}
	for group, wanted := range expected.Files {
		current := map[string]File{}
		for _, file := range actual.Files[group] {
			current[file.Path] = file
		}
		for _, file := range wanted {
			if group == "vault" && file.Path == ".reliquary/data/task_jobs.json" {
				if got, present := current[file.Path]; present && matchingTaskReplicaRecovery(expected.Tables["task_jobs"], actual.Tables["task_jobs"], file, got) {
					delete(current, file.Path)
					continue
				}
			}
			// Runtime sidecar config may change while re-entering credentials.
			// Its canonical database rows are verified above.
			if strings.HasPrefix(file.Path, ".reliquary/config/") || strings.HasPrefix(file.Path, ".reliquary/quarantine/") || file.Path == ".reliquary/manifest.json" || (allowLogAppend && file.Path == ".reliquary/operation_logs.jsonl") || (allowModelChange && rebuildReplicaFile(file.Path)) || (options.AllowReconfigurationAppend && authenticationReplicaFile(file.Path)) {
				continue
			}
			got, ok := current[file.Path]
			if !ok || got.SHA256 != file.SHA256 || got.Size != file.Size {
				diffs = append(diffs, group+" file "+file.Path)
			}
			delete(current, file.Path)
		}
		for path := range current {
			if strings.HasPrefix(path, ".reliquary/config/") || strings.HasPrefix(path, ".reliquary/quarantine/") || path == ".reliquary/manifest.json" || (allowLogAppend && path == ".reliquary/operation_logs.jsonl") || (allowModelChange && rebuildReplicaFile(path)) || (options.AllowReconfigurationAppend && authenticationReplicaFile(path)) {
				continue
			}
			diffs = append(diffs, group+" unexpected file "+path)
		}
	}
	left, _ := json.Marshal(expected.Retrieval)
	right, _ := json.Marshal(actual.Retrieval)
	if !bytes.Equal(left, right) {
		diffs = append(diffs, "fixed retrieval article IDs")
	}
	sort.Strings(diffs)
	return diffs
}

// Bind each task replica file to the exact raw rows saved by its own manifest.
// A paired recovery exception must never hide a stale or altered sidecar file.
func matchingTaskReplicaRecovery(expected, actual TableManifest, before, after File) bool {
	if expected.Data == nil || actual.Data == nil {
		return false // Older manifests contain no original active provenance.
	}
	for _, side := range []struct {
		rows []map[string]any
		file File
	}{{expected.Data, before}, {actual.Data, after}} {
		data, err := json.MarshalIndent(side.rows, "", "  ")
		if err != nil {
			return false
		}
		data = append(data, '\n')
		if sum(data) != side.file.SHA256 || int64(len(data)) != side.file.Size {
			return false
		}
	}
	left, right := comparableTaskRows(expected.Data, actual.Data)
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func rebuildReplicaFile(path string) bool {
	return path == ".reliquary/data/embedding_rebuild_runs.json" || path == ".reliquary/data/embedding_rebuild_items.json" || path == ".reliquary/data/entity_embedding_rebuild_runs.json"
}

func authenticationReplicaFile(path string) bool {
	return path == ".reliquary/data/auth_tokens.json" || path == ".reliquary/data/resource_access_grants.json" || path == ".reliquary/data/chatgpt_login_attempts.json"
}

func keepExpectedMetadataOccurrences(expected, actual []map[string]any) []map[string]any {
	counts := map[string]int{}
	for _, row := range expected {
		data, _ := json.Marshal(row)
		counts[string(data)]++
	}
	result := []map[string]any{}
	for _, row := range actual {
		data, _ := json.Marshal(row)
		key := string(data)
		if counts[key] > 0 {
			counts[key]--
			result = append(result, row)
		}
	}
	sortRows(result)
	return result
}

func normalizeEmbeddingSelection(expected, actual []map[string]any) []map[string]any {
	byID := map[string]map[string]any{}
	for _, row := range expected {
		byID[fmt.Sprint(row["id"])] = row
	}
	for _, row := range actual {
		original := byID[fmt.Sprint(row["id"])]
		if original == nil {
			continue
		}
		row["embedding_model_id"] = original["embedding_model_id"]
		row["updated_at"] = original["updated_at"]
		oldTiers, _ := original["model_tiers"].(map[string]any)
		newTiers, ok := row["model_tiers"].(map[string]any)
		if ok {
			cloned := map[string]any{}
			for key, value := range newTiers {
				if key != "embedding" {
					cloned[key] = value
				}
			}
			if value, present := oldTiers["embedding"]; present {
				cloned["embedding"] = value
			}
			row["model_tiers"] = cloned
		}
	}
	sortRows(actual)
	return actual
}

func normalizeEmbeddingModels(expected, actual []map[string]any, selected map[string]bool) []map[string]any {
	byID := map[string]map[string]any{}
	for _, row := range expected {
		byID[fmt.Sprint(row["id"])] = row
	}
	result := []map[string]any{}
	for _, row := range actual {
		id := fmt.Sprint(row["id"])
		original := byID[id]
		if selected[id] && row["type"] == "Embedding" {
			if original == nil {
				continue // Explicitly selected new embedding configuration.
			}
			for _, field := range []string{"name", "display_name", "description", "source", "parameters", "updated_at"} {
				row[field] = original[field]
			}
		}
		result = append(result, row)
	}
	sortRows(result)
	return result
}

func keepExpectedRows(expected, actual []map[string]any) []map[string]any {
	ids := map[string]bool{}
	for _, row := range expected {
		ids[fmt.Sprint(row["id"])] = true
	}
	result := []map[string]any{}
	for _, row := range actual {
		if ids[fmt.Sprint(row["id"])] {
			result = append(result, row)
		}
	}
	sortRows(result)
	return result
}

func normalizeRebuildAppend(table string, expected, actual []map[string]any, modelIDs, newRuns map[string]bool) []map[string]any {
	existing := map[string]bool{}
	for _, row := range expected {
		existing[fmt.Sprint(row["id"])] = true
	}
	result := []map[string]any{}
	for _, row := range actual {
		if !existing[fmt.Sprint(row["id"])] {
			if table == "embedding_rebuild_items" && newRuns[fmt.Sprint(row["run_id"])] {
				continue
			}
			if table != "embedding_rebuild_items" && modelIDs[fmt.Sprint(row["model_id"])] {
				continue
			}
		}
		result = append(result, row)
	}
	sortRows(result)
	return result
}
