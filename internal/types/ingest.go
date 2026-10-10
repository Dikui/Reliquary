package types

// IngestRequest is sourced content collected deliberately by an external agent.
// Agent annotations are stored separately from article metadata and processing input.
type IngestRequest struct {
	URL           string             `json:"url"`
	Content       string             `json:"content"`
	Title         string             `json:"title,omitempty"`
	Author        string             `json:"author,omitempty"`
	Publisher     string             `json:"publisher,omitempty"`
	PublishedAt   string             `json:"published_at,omitempty"`
	Images        []IngestImage      `json:"images,omitempty"`
	Thread        []IngestThreadItem `json:"thread,omitempty"`
	Quotes        []IngestQuote      `json:"quotes,omitempty"`
	CollectReason string             `json:"collect_reason,omitempty"`
	AgentSummary  string             `json:"agent_summary,omitempty"`
	AgentTags     []string           `json:"agent_tags,omitempty"`
	SourceName    string             `json:"source_name,omitempty"`
}

type IngestImage struct {
	URL     string `json:"url,omitempty"`
	Data    string `json:"data,omitempty"`
	Caption string `json:"caption,omitempty"`
	Text    string `json:"text,omitempty"`
}

type IngestThreadItem struct {
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Content     string `json:"content"`
}

type IngestQuote struct {
	Content string `json:"content"`
	Author  string `json:"author,omitempty"`
	URL     string `json:"url,omitempty"`
}

type IngestImageFailure struct {
	Index  int    `json:"index"`
	URL    string `json:"url,omitempty"`
	Reason string `json:"reason"`
}

type IngestResult struct {
	ArticleID     string               `json:"article_id"`
	Status        string               `json:"status"`
	Message       string               `json:"message"`
	ImageFailures []IngestImageFailure `json:"image_failures"`
}
