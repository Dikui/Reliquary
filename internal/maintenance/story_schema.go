package maintenance

func projectStorySnapshot(snapshot *Snapshot, targetVersion int64) {
	projectEntityCandidatesSnapshot(snapshot, targetVersion)
	if snapshot.schemaVersion >= 39 || targetVersion < 39 {
		return
	}
	for _, table := range []string{"stories", "facts", "entry_facts"} {
		if _, ok := snapshot.Tables[table]; !ok {
			snapshot.Tables[table] = []map[string]any{}
		}
	}
}

func projectStoryManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	expected, actual = projectEntityCandidatesManifest(expected, actual)
	if expected.SchemaVersion >= 39 || actual.SchemaVersion < 39 {
		return expected, actual
	}
	for _, table := range []string{"stories", "facts", "entry_facts"} {
		expected, actual = projectEmptyRegisteredTable(expected, actual, table)
	}
	return expected, actual
}
