package maintenance

import (
	"context"

	"gorm.io/gorm"
)

// Historical migration tests use the same temporary naming adapter as the
// offline CLI, so their raw databases retain the installed version's schema.
func withHistoricalNames[T any](db *gorm.DB, run func(*gorm.DB) (T, error)) (T, error) {
	var value T
	err := WithCurrentNames(db, func(tx *gorm.DB) error {
		var err error
		value, err = run(tx)
		return err
	})
	return value, err
}

func captureHistoricalSchema(ctx context.Context, db *gorm.DB) (*Snapshot, error) {
	return withHistoricalNames(db, func(tx *gorm.DB) (*Snapshot, error) { return Capture(ctx, tx) })
}
func manifestHistoricalSchema(ctx context.Context, db *gorm.DB, root, uploads string) (*Manifest, error) {
	return withHistoricalNames(db, func(tx *gorm.DB) (*Manifest, error) { return BuildManifest(ctx, tx, root, uploads) })
}
func exportHistoricalSources(ctx context.Context, db *gorm.DB, root string) (*SourcesExportResult, error) {
	return withHistoricalNames(db, func(tx *gorm.DB) (*SourcesExportResult, error) { return ExportSources(ctx, tx, root) })
}
func classifyHistoricalSchema(ctx context.Context, db *gorm.DB) error {
	return WithCurrentNames(db, func(tx *gorm.DB) error { return ValidateClassification(ctx, tx) })
}
