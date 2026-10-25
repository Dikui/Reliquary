package maintenance

func projectGroupingSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 41 || targetVersion < 41 {
		return
	}
	if _, ok := snapshot.Tables["grouping_decisions"]; !ok {
		snapshot.Tables["grouping_decisions"] = []map[string]any{}
	}
}

func projectGroupingManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 41 || actual.SchemaVersion < 41 {
		return expected, actual
	}
	expected, actual = projectEmptyRegisteredTable(expected, actual, "grouping_decisions")
	if _, ok := expected.Tables["fact_embeddings"]; !ok {
		left := *expected
		left.Tables = make(map[string]TableManifest, len(expected.Tables)+1)
		for table, item := range expected.Tables {
			left.Tables[table] = item
		}
		left.Tables["fact_embeddings"] = TableManifest{Regenerated: true, Columns: []string{}}
		expected = &left
	}
	return expected, actual
}
