package maintenance

func projectUnderstandingSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion < 38 && targetVersion >= 38 {
		projectEntryDefaults(snapshot, map[string]any{"title_manual": false, "description_manual": false})
	}
}
