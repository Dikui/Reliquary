package maintenance

func projectDomainSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 37 || targetVersion < 37 {
		return
	}
	projectEntryDefaults(snapshot, map[string]any{"domains_manual": false})
	for _, table := range []string{"domains", "entry_domains", "domain_seed_imports"} {
		if _, ok := snapshot.Tables[table]; !ok {
			snapshot.Tables[table] = []map[string]any{}
		}
	}
}

func projectDomainManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 37 || actual.SchemaVersion < 37 {
		return expected, actual
	}
	for _, table := range []string{"domains", "entry_domains", "domain_seed_imports"} {
		expected, actual = projectEmptyRegisteredTable(expected, actual, table)
	}
	return expected, actual
}
