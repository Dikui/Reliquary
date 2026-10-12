package types

import "fmt"

// DuplicateEntryError duplicate entry error, contains the existing entry object
type DuplicateEntryError struct {
	Message string
	Entry   *Entry
}

func (e *DuplicateEntryError) Error() string {
	return e.Message
}

// NewDuplicateFileError creates a duplicate file error
func NewDuplicateFileError(entry *Entry) *DuplicateEntryError {
	return &DuplicateEntryError{
		Message: fmt.Sprintf("File already exists: %s", entry.FileName),
		Entry:   entry,
	}
}

// NewDuplicateURLError creates a duplicate URL error
func NewDuplicateURLError(entry *Entry) *DuplicateEntryError {
	return &DuplicateEntryError{
		Message: fmt.Sprintf("URL already exists: %s", entry.Source),
		Entry:   entry,
	}
}
