package errors

import "net/http"

// ErrKnowledgeDomainReferenced identifies a domain deletion blocked by active
// knowledge base references. Clients can localize its structured count.
const ErrKnowledgeDomainReferenced ErrorCode = 2400

func NewKnowledgeDomainReferencedError(message string, details any) *AppError {
	return &AppError{
		Code:     ErrKnowledgeDomainReferenced,
		Message:  message,
		Details:  details,
		HTTPCode: http.StatusConflict,
	}
}
