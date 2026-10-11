package maintenance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func sameUploadInventory(expected, actual []File) bool {
	if len(expected) != len(actual) {
		return false
	}
	files := map[string]File{}
	for _, file := range actual {
		files[file.Path] = file
	}
	for _, file := range expected {
		if files[file.Path] != file {
			return false
		}
	}
	return true
}

// The archive deliberately retains removed originals. Current uploads are an
// independent exact inventory; an explicitly empty list restores no live file.
func validateActiveUploads(root string, manifest *ReplicaManifest) error {
	if manifest.ActiveUploads == nil {
		return nil // Legacy replicas did not distinguish active from archived.
	}
	archive := map[string]File{}
	for _, file := range manifest.Files {
		if !strings.HasPrefix(file.Path, "resources/") {
			continue
		}
		path := strings.TrimPrefix(file.Path, "resources/")
		if _, duplicate := archive[path]; duplicate {
			return errors.New("duplicate archived resource inventory entry")
		}
		archive[path] = file
	}
	seen := map[string]bool{}
	for _, file := range *manifest.ActiveUploads {
		if _, err := SafePath(filepath.Join(root, ".reliquary", "resources"), file.Path); err != nil {
			return errors.New("unsafe active upload inventory path")
		}
		if seen[file.Path] {
			return errors.New("duplicate active upload inventory entry")
		}
		seen[file.Path] = true
		stored, found := archive[file.Path]
		if !found || stored.SHA256 != file.SHA256 || stored.Size != file.Size {
			return errors.New("active upload inventory does not match archived resource hashes")
		}
	}
	return nil
}

// RestoreResources never removes historical archive copies. Replicas without
// the new inventory retain the former behavior of restoring every archive file.
func RestoreResources(root, destination string, activeUploads *[]File) error {
	source := filepath.Join(root, ".reliquary", "resources")
	if activeUploads == nil {
		return mirrorResources(source, destination)
	}
	for _, file := range *activeUploads {
		original, err := SafePath(source, file.Path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(original)
		if err != nil || sum(data) != file.SHA256 || int64(len(data)) != file.Size {
			return errors.New("active upload changed after archive verification")
		}
		target, err := SafePath(destination, file.Path)
		if err != nil {
			return err
		}
		if err := AtomicWrite(target, data); err != nil {
			return err
		}
	}
	return nil
}
