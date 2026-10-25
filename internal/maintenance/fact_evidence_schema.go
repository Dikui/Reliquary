package maintenance

var factEvidenceDefaults = map[string]map[string]any{"facts": {"recall_date": nil}}

func projectFactEvidenceSnapshot(snapshot *Snapshot, targetVersion int64) {
	projectColumnDefaultsSnapshot(snapshot, targetVersion, 43, factEvidenceDefaults)
}

func projectFactEvidenceManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	return projectColumnDefaultsManifest(expected, actual, 43, factEvidenceDefaults)
}
