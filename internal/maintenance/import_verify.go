package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// VerifyImportedReplica must run before the first post-import replica flush.
// Import mutates its in-memory rows to materialize article bodies and pause
// credential-dependent features, so reload the untouched portable originals.
func VerifyImportedReplica(ctx context.Context, db *gorm.DB, root string) error {
	original, err := Load(root)
	if err != nil {
		return fmt.Errorf("validate original vault replica: %w", err)
	}
	if err := retireSnapshotSources(root, original); err != nil {
		return err
	}
	markdownOnly, err := markdownOnlySchema(ctx, db)
	if err != nil {
		return err
	}
	if markdownOnly {
		projectMarkdownSnapshot(original)
	}
	current, err := Capture(ctx, db)
	if err != nil {
		return fmt.Errorf("capture imported replica: %w", err)
	}
	if _, modern := current.Tables["task_pending_ops"]; modern {
		if err := migrateQueueSnapshot(original); err != nil {
			return err
		}
	}
	if _, modern := current.Tables["library_content_types"]; modern {
		if err := migrateLibrarySnapshot(original, current.Tables["libraries"]); err != nil {
			return err
		}
	}
	if db.Migrator().HasColumn("topics", "library_ids") {
		projectFollowSnapshot(original)
	}
	if db.Migrator().HasColumn("entries", "source_ref") {
		projectIngestSnapshot(original)
	}
	if db.Migrator().HasColumn("ingest_discards", "library_id") {
		projectDiscardSnapshot(original)
	}
	if db.Migrator().HasColumn("embedding_rebuild_runs", "candidate") {
		var created, updated any
		for _, row := range current.Tables["models"] {
			if row["id"] == defaultVectorModelID {
				created, updated = row["created_at"], row["updated_at"]
			}
		}
		projectVectorSnapshot(original, created, updated, vectorSnapshotLegacy(original))
	}
	projectSegmentSnapshot(original, current)
	differences := []string{}
	for table, wanted := range original.Tables {
		actual, present := current.Tables[table]
		if !present {
			differences = append(differences, "missing table "+table)
			continue
		}
		left, right := comparableCredentialRows(table, wanted, actual, original.Required)
		before, _ := json.Marshal(left)
		after, _ := json.Marshal(right)
		if len(wanted) != len(actual) || !bytes.Equal(before, after) {
			differences = append(differences, "table "+table)
		}
		before, _ = json.Marshal(original.JSONNulls[table])
		after, _ = json.Marshal(current.JSONNulls[table])
		if !bytes.Equal(before, after) {
			differences = append(differences, "table JSONB null types "+table)
		}
	}
	for table := range current.Tables {
		if _, present := original.Tables[table]; !present {
			differences = append(differences, "unexpected table "+table)
		}
	}
	if len(differences) != 0 {
		sort.Strings(differences)
		return fmt.Errorf("imported replica verification failed; original files retained: %s", strings.Join(differences, ", "))
	}
	return nil
}
