package types

const (
	ProcessingReviewPending   = "pending"
	ProcessingReviewConfirmed = "confirmed"
	ProcessingReviewCorrected = "corrected"
	ProcessingReviewIgnored   = "ignored"
)

// ArticleProcessingMode retains the full source until an uncertain model
// classification is resolved, and always retains sources admitted without a URL.
func ArticleProcessingMode(k *Entry) string {
	if k.SourceUnverified || k.SourceKind == "mine" {
		return ProcessingModeFull
	}
	if k.ProcessingReviewStatus == ProcessingReviewIgnored {
		return k.ProcessingMode
	}
	if k.ContentTypeUncertain && !k.ContentTypeCorrected && k.ProcessingReviewStatus != ProcessingReviewConfirmed {
		return ProcessingModeFull
	}
	return ProcessingModeForContentType(k.ContentType)
}
