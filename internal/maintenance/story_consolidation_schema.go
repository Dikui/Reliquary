package maintenance

func projectStoryConsolidationSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 44 || targetVersion < 44 {
		return
	}
	for _, table := range []string{"story_bridge_evidence", "story_links"} {
		if _, ok := snapshot.Tables[table]; !ok {
			snapshot.Tables[table] = []map[string]any{}
		}
	}
}

func projectStoryConsolidationManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 44 || actual.SchemaVersion < 44 {
		return expected, actual
	}
	for _, table := range []string{"story_bridge_evidence", "story_links"} {
		expected, actual = projectEmptyRegisteredTable(expected, actual, table)
	}
	return expected, actual
}
