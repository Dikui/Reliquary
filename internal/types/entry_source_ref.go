package types

import (
	"errors"
	"gorm.io/gorm"
)

// FindLatestSourceEntry is shared by entry reads and future graph consumers.
// Deleted entries are excluded; retained older entries remain independently readable.
func FindLatestSourceEntry(db *gorm.DB, spaceID uint64, libraryID, sourceRef string) (*Entry, error) {
	if sourceRef == "" {
		return nil, nil
	}
	var entry Entry
	err := db.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Where("space_id=? AND library_id=? AND source_ref<>'' AND md5(source_ref)=md5(?) AND source_ref=?", spaceID, libraryID, sourceRef, sourceRef).Order("created_at DESC,id DESC").First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entry.LatestEntryID = entry.ID
	return &entry, nil
}

func (entry *Entry) AfterFind(tx *gorm.DB) error {
	if entry.SourceRef == "" {
		return nil
	}
	// A fresh statement avoids inheriting the original read's ID/filter clauses.
	latest, err := FindLatestSourceEntry(tx, entry.SpaceID, entry.LibraryID, entry.SourceRef)
	if err != nil {
		return err
	}
	if latest != nil {
		entry.LatestEntryID, entry.HasUpdate = latest.ID, latest.ID != entry.ID
	}
	return nil
}
