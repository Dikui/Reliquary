package maintenance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/vaultpath"
)

type Snapshot struct {
	FormatVersion    int                            `json:"format_version"`
	Tables           map[string][]map[string]any    `json:"tables"`
	Required         []Requirement                  `json:"required"`
	ActiveUploads    *[]File                        `json:"active_uploads,omitempty"`
	JSONNulls        map[string]map[string][]string `json:"json_nulls"`
	credentialValues []string
	registryVersion  int
	schemaVersion    int64
	storedFormat     int
}
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type ReplicaManifest struct {
	SchemaVersion   int64                          `json:"schema_version,omitempty"`
	RegistryVersion int                            `json:"registry_version,omitempty"`
	FormatVersion   int                            `json:"format_version"`
	Files           []File                         `json:"files"`
	Required        []Requirement                  `json:"required"`
	ActiveUploads   *[]File                        `json:"active_uploads,omitempty"`
	JSONNulls       map[string]map[string][]string `json:"json_nulls"`
	VaultFiles      []File                         `json:"vault_files"`
	Tables          []string                       `json:"tables"`
}

func Capture(ctx context.Context, db *gorm.DB) (*Snapshot, error) {
	if err := ValidateClassification(ctx, db); err != nil {
		return nil, err
	}
	rules, err := schemaRules(ctx, db)
	if err != nil {
		return nil, err
	}
	snapshot := &Snapshot{FormatVersion: FormatVersion, Tables: map[string][]map[string]any{}, Required: []Requirement{}, JSONNulls: map[string]map[string][]string{}}
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.WithContext(ctx).Table("schema_migrations").Select("version").Scan(&snapshot.schemaVersion).Error; err != nil {
			return nil, err
		}
	}
	columns, err := Schema(ctx, db)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	jsonColumns := map[string][]string{}
	for _, column := range columns {
		present[column.Table] = true
		if column.DataType == "jsonb" && rules[column.Table].Columns()[column.Name] != Regenerated && !secretColumn(column.Table, column.Name) {
			jsonColumns[column.Table] = append(jsonColumns[column.Table], column.Name)
		}
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		needles, err := credentialNeedles(ctx, tx)
		if err != nil {
			return err
		}
		snapshot.credentialValues = needles
		for _, table := range sortedTables(rules) {
			if _, backupOnly := BackupOnlyTables[table]; backupOnly {
				continue
			}
			if !present[table] || table == "schema_migrations" {
				continue
			}
			rule := rules[table].Columns()
			persistent := false
			for _, category := range rule {
				if category != Regenerated {
					persistent = true
				}
			}
			if !persistent {
				continue
			}
			nullTests := []string{}
			for _, column := range jsonColumns[table] {
				nullTests = append(nullTests, "CASE WHEN "+quote(column)+"='null'::jsonb THEN '"+column+"'::text END")
			}
			nullQuery := "'[]'::text"
			if len(nullTests) > 0 {
				nullQuery = "to_jsonb(array_remove(ARRAY[" + strings.Join(nullTests, ",") + "],NULL))::text"
			}
			rows, err := tx.Raw(`SELECT to_jsonb(t)::text, ` + nullQuery + ` FROM public.` + quote(table) + ` t`).Rows()
			if err != nil {
				return err
			}
			items := []map[string]any{}
			for rows.Next() {
				var data, nullData []byte
				if err := rows.Scan(&data, &nullData); err != nil {
					rows.Close()
					return err
				}
				row, err := jsonRow(data)
				if err != nil {
					rows.Close()
					return err
				}
				clean, removed := sanitizeRow(table, row)
				clean = redactCredentials(clean, needles).(map[string]any)
				var nullColumns []string
				if err := json.Unmarshal(nullData, &nullColumns); err != nil {
					rows.Close()
					return err
				}
				if len(nullColumns) > 0 {
					if snapshot.JSONNulls[table] == nil {
						snapshot.JSONNulls[table] = map[string][]string{}
					}
					snapshot.JSONNulls[table][rowIdentity(row)] = nullColumns
				}
				if table == "models" {
					if parameters, ok := clean["parameters"].(map[string]any); ok {
						if extra, ok := parameters["extra_config"].(map[string]any); ok {
							removed = append(removed, requirementFields(extra["maintenance_required_fields"])...)
						}
					}
					if row["source"] == "chatgpt" {
						removed = append(removed, "chatgpt_login")
					}
				}
				if table == "data_sources" {
					if config, ok := clean["config"].(map[string]any); ok {
						removed = append(removed, requirementFields(config["_reliquary_maintenance_required_fields"])...)
					}
				}
				if table == "chatgpt_accounts" {
					removed = append(removed, "chatgpt_login")
				}
				if inactiveCredentialTable(table) {
					snapshot.Required = append(snapshot.Required, inactiveRequirement(table, clean))
				} else if len(removed) > 0 {
					snapshot.Required = append(snapshot.Required, Requirement{Kind: table, ID: fmt.Sprint(clean["id"]), Name: fmt.Sprint(clean["name"]), Fields: removed, Reason: "credentials are intentionally excluded; enter them again"})
				}
				for name := range clean {
					if rule[name] == Regenerated {
						delete(clean, name)
					}
				}
				// Published article text lives in the vault. Imported personal
				// documents retain their database recovery copy in this replica.
				if table == "entries" && row["vault_path"] != "" && row["source_kind"] != "mine" && row["source_kind"] != "report" {
					delete(clean, "vault_body")
				}
				items = append(items, clean)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			sortRows(items)
			snapshot.Tables[table] = items
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return snapshot, err
}

func quote(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func rowIdentity(row map[string]any) string {
	if id, ok := row["id"]; ok {
		return fmt.Sprint(id)
	}
	data, _ := json.Marshal(row)
	return sum(data)
}
func sortRows(rows []map[string]any) {
	sort.Slice(rows, func(i, j int) bool {
		left, _ := json.Marshal(rows[i])
		right, _ := json.Marshal(rows[j])
		return bytes.Compare(left, right) < 0
	})
}
func sum(data []byte) string         { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
func ProfileHash(data []byte) string { return sum(data) }

// AtomicWrite fsyncs and reads back the temporary file before replacing the
// destination. Identical content does not change the inode or modification time.
func AtomicWrite(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".reliquary-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	readback, err := os.ReadFile(file.Name())
	if err != nil {
		return err
	}
	if !bytes.Equal(readback, data) {
		return errors.New("replica temporary file readback mismatch")
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func appendLogs(path string, rows []map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	existing := map[string][]byte{}
	file, err := os.Open(path)
	if err == nil {
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 65536), 64*1024*1024)
		for scanner.Scan() {
			row, err := jsonRow(scanner.Bytes())
			if err != nil {
				file.Close()
				return fmt.Errorf("invalid append-only operation log: %w", err)
			}
			addLogDefaults(row)
			canonical, _ := json.Marshal(row)
			existing[fmt.Sprint(row["id"])] = canonical
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := fmt.Sprint(rows[i]["created_at"]), fmt.Sprint(rows[j]["created_at"])
		if left == right {
			return fmt.Sprint(rows[i]["id"]) < fmt.Sprint(rows[j]["id"])
		}
		return left < right
	})
	var additional bytes.Buffer
	for _, row := range rows {
		data, _ := json.Marshal(row)
		id := fmt.Sprint(row["id"])
		if old, ok := existing[id]; ok {
			if !bytes.Equal(old, data) {
				return fmt.Errorf("append-only operation log %s was modified", id)
			}
			delete(existing, id)
			continue
		}
		additional.Write(data)
		additional.WriteByte('\n')
	}
	if len(existing) > 0 {
		return errors.New("append-only operation logs were deleted from the database")
	}
	if additional.Len() == 0 {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	if _, err := output.Write(additional.Bytes()); err != nil {
		return err
	}
	return output.Sync()
}

func replicaFiles(snapshot *Snapshot) (map[string][]byte, error) {
	return replicaFilesForFormat(snapshot, FormatVersion)
}

func replicaFilesForFormat(snapshot *Snapshot, format int) (map[string][]byte, error) {
	files := map[string][]byte{}
	for table, rows := range snapshot.Tables {
		if table == "operation_logs" {
			continue
		}
		dir := "data"
		if configTable(table) {
			dir = "config"
		}
		if format < FormatVersion {
			rows = entryNamedRows(table, rows, true, format)
			table = legacyTable(table, format)
		}
		data, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return nil, err
		}
		path := filepath.ToSlash(filepath.Join(dir, table+".json"))
		if registered, ok := registeredPath(table); ok {
			path = registered
		}
		files[path] = append(data, '\n')
	}
	registry, err := registryBytes(snapshot)
	if err != nil {
		return nil, err
	}
	files["registry.json"] = registry
	return files, nil
}

func WriteSnapshot(root, uploads string, snapshot *Snapshot) error {
	retired := false
	if _, current := snapshot.Tables["data_sources"]; !current {
		var err error
		retired, err = archiveCollectionReplica(root)
		if err != nil {
			return err
		}
	}
	if uploads != "" && len(snapshot.credentialValues) > 0 {
		if err := checkResourceCredentials(uploads, snapshot.credentialValues); err != nil {
			return errors.New("upload contains an operational credential or cannot be checked; replica refused without changing the original")
		}
	}
	directory, err := SafePath(root, ".reliquary")
	if err != nil {
		return err
	}
	required, err := portableRequirements(directory, snapshot)
	if err != nil {
		return err
	}
	files, err := replicaFiles(snapshot)
	if err != nil {
		return err
	}
	files["config/required_credentials.json"], err = requirementCopy(required)
	if err != nil {
		return err
	}
	for path, data := range files {
		target, err := SafePath(directory, path)
		if err != nil {
			return err
		}
		if err := AtomicWrite(target, data); err != nil {
			return err
		}
	}
	if err := appendLogs(filepath.Join(directory, "operation_logs.jsonl"), namedRows(snapshot.Tables["operation_logs"], true)); err != nil {
		return err
	}
	if uploads != "" {
		if err := mirrorResources(uploads, filepath.Join(directory, "resources")); err != nil {
			return err
		}
	}
	activeUploads, err := Files(uploads)
	if err != nil {
		return err
	}
	manifest := ReplicaManifest{SchemaVersion: snapshot.schemaVersion, RegistryVersion: RegistryVersion, FormatVersion: FormatVersion, Required: required, ActiveUploads: &activeUploads, JSONNulls: snapshot.JSONNulls, Files: []File{}, Tables: []string{}}
	for table := range snapshot.Tables {
		manifest.Tables = append(manifest.Tables, table)
	}
	sort.Strings(manifest.Tables)
	manifest.VaultFiles, err = vaultFiles(root)
	if err != nil {
		return err
	}
	for path, data := range files {
		manifest.Files = append(manifest.Files, File{Path: path, SHA256: sum(data), Size: int64(len(data))})
	}
	for _, subdir := range []string{"resources"} {
		items, err := Files(filepath.Join(directory, subdir))
		if err != nil {
			return err
		}
		for _, item := range items {
			item.Path = subdir + "/" + item.Path
			manifest.Files = append(manifest.Files, item)
		}
	}
	log, err := os.ReadFile(filepath.Join(directory, "operation_logs.jsonl"))
	if err != nil {
		return err
	}
	manifest.Files = append(manifest.Files, File{Path: "operation_logs.jsonl", SHA256: sum(log), Size: int64(len(log))})
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := AtomicWrite(filepath.Join(directory, "manifest.json"), append(data, '\n')); err != nil {
		return err
	}
	if _, currentQueue := snapshot.Tables["task_pending_ops"]; currentQueue {
		if err := os.Remove(filepath.Join(directory, "data/task_jobs.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if retired {
		for _, path := range []string{"config/data_sources.json", "data/sync_logs.json"} {
			if err := os.Remove(filepath.Join(directory, path)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	for _, table := range []string{"spaces", "api_keys", "libraries", "entries", "entry_entities", "entry_topics", "entry_processing_spans", "entry_repost_sources"} {
		if _, present := snapshot.Tables[table]; !present {
			continue
		}
		dir := "data"
		if configTable(table) {
			dir = "config"
		}
		oldPath := filepath.Join(directory, dir, legacyTable(table)+".json")
		if err := os.Remove(oldPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func checkResourceCredentials(root string, needles []string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) && path == root {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("resource symlink refused")
		}
		if err := scanCredentialReader(strings.NewReader(entry.Name()), needles); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		return scanCredentialReader(file, needles)
	})
}

func SafePath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("asset path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("asset path escapes root")
	}
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("asset path contains symlink: %s", relative)
		}
	}
	return current, nil
}

func Files(root string) ([]File, error) {
	return files(root, nil)
}

// VaultFiles inventories managed vault content while leaving user configuration
// available to physical backups. Upload inventories continue to include all files.
func VaultFiles(root string) ([]File, error) {
	return files(root, vaultpath.UserMetadata)
}

func files(root string, ignore func(string) bool) ([]File, error) {
	result := []File{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) && path == root {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if ignore != nil && ignore(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("asset symlink refused: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result = append(result, File{Path: relative, SHA256: sum(data), Size: int64(len(data))})
		return nil
	})
	return result, err
}

// RuntimeVaultFiles retains original attachments and historical migration exports.
func RuntimeVaultFiles(root string) ([]File, error) {
	return files(root, func(relative string) bool {
		return relative != "." && relative != "attachments" && !strings.HasPrefix(relative, "attachments/") &&
			relative != ".reliquary" && relative != ".reliquary/exports" && !strings.HasPrefix(relative, ".reliquary/exports/")
	})
}

func vaultFiles(root string) ([]File, error) {
	return files(root, func(relative string) bool {
		return relative == ".reliquary" || strings.HasPrefix(relative, ".reliquary/") || vaultpath.UserMetadata(relative)
	})
}

func mirrorResources(source, destination string) error {
	files, err := Files(source)
	if err != nil {
		return err
	}
	for _, file := range files {
		from, err := SafePath(source, file.Path)
		if err != nil {
			return err
		}
		to, err := SafePath(destination, file.Path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := AtomicWrite(to, data); err != nil {
			return err
		}
	}
	return nil
}

type Writer struct {
	db        *gorm.DB
	root      string
	uploads   string
	debounce  time.Duration
	mu        sync.Mutex
	flushMu   sync.Mutex
	timer     *time.Timer
	stop      chan struct{}
	done      chan struct{}
	closed    bool
	started   bool
	lastError error
}

func NewWriter(db *gorm.DB, root, uploads string, debounce time.Duration) *Writer {
	if debounce <= 0 {
		debounce = 5 * time.Second
	}
	return &Writer{db: db, root: root, uploads: uploads, debounce: debounce, stop: make(chan struct{}), done: make(chan struct{})}
}
func (w *Writer) Flush(ctx context.Context) error {
	w.flushMu.Lock()
	defer w.flushMu.Unlock()
	snapshot, err := Capture(ctx, w.db)
	if err == nil {
		err = WriteSnapshot(w.root, w.uploads, snapshot)
	}
	w.mu.Lock()
	w.lastError = err
	w.mu.Unlock()
	return err
}
func (w *Writer) Error() error { w.mu.Lock(); defer w.mu.Unlock(); return w.lastError }
func (w *Writer) Schedule() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(w.debounce, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_ = w.Flush(ctx)
	})
}
func (w *Writer) Start() error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return nil
	}
	w.started = true
	w.mu.Unlock()
	callback := func(tx *gorm.DB) {
		if tx.Error == nil {
			w.Schedule()
		}
	}
	if err := w.db.Callback().Create().After("gorm:commit_or_rollback_transaction").Register("reliquary:replica", callback); err != nil {
		return err
	}
	if err := w.db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("reliquary:replica", callback); err != nil {
		return err
	}
	if err := w.db.Callback().Delete().After("gorm:commit_or_rollback_transaction").Register("reliquary:replica", callback); err != nil {
		return err
	}
	w.Schedule()
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(w.debounce)
		defer ticker.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				_ = w.Flush(ctx)
				cancel()
			}
		}
	}()
	return nil
}
func (w *Writer) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return w.Error()
	}
	w.closed = true
	started := w.started
	if w.timer != nil {
		w.timer.Stop()
	}
	close(w.stop)
	w.mu.Unlock()
	if started {
		<-w.done
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return w.Flush(ctx)
}
