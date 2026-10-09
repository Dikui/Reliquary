package types

import "fmt"

// DuplicateKnowledgeError duplicate knowledge error, contains the existing knowledge object
type DuplicateKnowledgeError struct {
	Message   string
	Knowledge *Knowledge
}

func (e *DuplicateKnowledgeError) Error() string {
	return e.Message
}

// NewDuplicateFileError creates a duplicate file error
func NewDuplicateFileError(knowledge *Knowledge) *DuplicateKnowledgeError {
	return &DuplicateKnowledgeError{
		Message:   fmt.Sprintf("File already exists: %s", knowledge.FileName),
		Knowledge: knowledge,
	}
}

// NewDuplicateURLError creates a duplicate URL error
func NewDuplicateURLError(knowledge *Knowledge) *DuplicateKnowledgeError {
	return &DuplicateKnowledgeError{
		Message:   fmt.Sprintf("URL already exists: %s", knowledge.Source),
		Knowledge: knowledge,
	}
}
