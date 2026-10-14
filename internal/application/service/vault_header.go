package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"reliquary/internal/types"

	"gopkg.in/yaml.v3"
)

// VaultArticleHeader is the portable article metadata used by vault rebuild.
type VaultArticleHeader struct {
	LibraryID         string              `yaml:"library_id,omitempty" json:"library_id,omitempty"`
	Library           string              `yaml:"library,omitempty" json:"library,omitempty"`
	SourceRef         string              `yaml:"source_ref,omitempty" json:"source_ref,omitempty"`
	Identifier        string              `yaml:"identifier,omitempty" json:"identifier,omitempty"`
	HasUpdate         bool                `yaml:"has_update,omitempty" json:"has_update,omitempty"`
	LatestEntryID     string              `yaml:"latest_article_id,omitempty" json:"latest_article_id,omitempty"`
	Graph             *bool               `yaml:"graph,omitempty" json:"graph,omitempty"`
	RelatedArticleIDs []string            `yaml:"related_article_ids,omitempty" json:"related_article_ids,omitempty"`
	ID                string              `yaml:"id" json:"id"`
	SourceKind        string              `yaml:"source_kind,omitempty" json:"source_kind,omitempty"`
	URL               string              `yaml:"url" json:"url"`
	Title             string              `yaml:"title" json:"title"`
	Publisher         string              `yaml:"publisher" json:"publisher"`
	Author            string              `yaml:"author" json:"author"`
	PublishedAt       string              `yaml:"published_at" json:"published_at"`
	CollectedAt       string              `yaml:"collected_at" json:"collected_at"`
	ContentType       string              `yaml:"content_type" json:"content_type"`
	Summary           *types.EntrySummary `yaml:"summary" json:"summary"`
	Entities          []string            `yaml:"entities" json:"entities"`
	Topics            []string            `yaml:"topics" json:"topics"`
	Starred           bool                `yaml:"starred" json:"starred"`
	CollectReason     string              `yaml:"collect_reason" json:"collect_reason"`
	SourceUnverified  bool                `yaml:"source_unverified,omitempty" json:"source_unverified"`
	Acquisition       string              `yaml:"acquisition,omitempty" json:"acquisition,omitempty"`
}

// RenderVaultArticleHeader emits one ordered schema for every library.
func RenderVaultArticleHeader(h VaultArticleHeader) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	scalar := func(key, value string) {
		if value != "" {
			writeVaultScalar(&b, key, value)
		}
	}
	scalar("id", h.ID)
	scalar("library_id", h.LibraryID)
	scalar("library", h.Library)
	scalar("title", h.Title)
	scalar("url", h.URL)
	scalar("publisher", h.Publisher)
	scalar("author", h.Author)
	scalar("published_at", h.PublishedAt)
	scalar("collected_at", h.CollectedAt)
	scalar("content_type", h.ContentType)
	if len(h.Entities) > 0 {
		writeVaultList(&b, "entities", h.Entities, true)
	}
	if len(h.Topics) > 0 {
		writeVaultList(&b, "topics", h.Topics, true)
	}
	if h.Summary != nil && (h.Summary.Overview != "" || len(h.Summary.Points) > 0 || len(h.Summary.KeyTerms) > 0) {
		b.WriteString("summary:\n")
		scalar("  overview", h.Summary.Overview)
		if len(h.Summary.Points) > 0 {
			writeVaultList(&b, "  points", h.Summary.Points, false)
		}
		if len(h.Summary.KeyTerms) > 0 {
			writeVaultList(&b, "  key_terms", h.Summary.KeyTerms, false)
		}
	}
	fmt.Fprintf(&b, "starred: %t\n", h.Starred)
	scalar("collect_reason", h.CollectReason)
	scalar("acquisition", h.Acquisition)
	scalar("source_ref", h.SourceRef)
	scalar("identifier", h.Identifier)
	if h.SourceUnverified {
		b.WriteString("source_unverified: true\n")
	}
	if h.HasUpdate {
		b.WriteString("has_update: true\n")
	}
	scalar("latest_article_id", h.LatestEntryID)
	if h.Graph != nil {
		fmt.Fprintf(&b, "graph: %t\n", *h.Graph)
	}
	if len(h.RelatedArticleIDs) > 0 {
		writeVaultList(&b, "related_article_ids", h.RelatedArticleIDs, true)
	}
	b.WriteString("---\n")
	return []byte(b.String())
}

func writeVaultScalar(b *strings.Builder, key, value string) {
	b.WriteString(key + ": ")
	if value == "" {
		b.WriteString("null")
	} else {
		encoded, _ := json.Marshal(value)
		b.Write(encoded)
	}
	b.WriteByte('\n')
}

func writeVaultList(b *strings.Builder, key string, values []string, ordered bool) {
	values = append([]string(nil), values...)
	if ordered {
		sort.Strings(values)
		values = uniqueVaultStrings(values)
	}
	if len(values) == 0 {
		b.WriteString(key + ": []\n")
		return
	}
	b.WriteString(key + ":\n")
	indent := key[:len(key)-len(strings.TrimLeft(key, " "))]
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		b.WriteString(indent + "  - ")
		b.Write(encoded)
		b.WriteByte('\n')
	}
}

func uniqueVaultStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

// SplitVaultArticle separates the opening front matter from body bytes. Only
// an exact delimiter line closes the header; subsequent delimiters are body.
func SplitVaultArticle(data []byte) (header, body []byte, err error) {
	end := bytes.IndexByte(data, '\n')
	if end < 0 || string(bytes.TrimSuffix(data[:end], []byte("\r"))) != "---" {
		return nil, nil, errors.New("article does not start with YAML front matter")
	}
	for start := end + 1; start < len(data); {
		lineEnd := bytes.IndexByte(data[start:], '\n')
		next := len(data)
		if lineEnd >= 0 {
			lineEnd += start
			next = lineEnd + 1
		} else {
			lineEnd = len(data)
		}
		if string(bytes.TrimSuffix(data[start:lineEnd], []byte("\r"))) == "---" {
			return data[:next], data[next:], nil
		}
		start = next
	}
	return nil, nil, errors.New("article YAML front matter has no closing delimiter")
}

// ParseVaultArticle validates portable metadata without modifying the file.
func ParseVaultArticle(data []byte) (VaultArticleHeader, []byte, error) {
	var h VaultArticleHeader
	header, body, err := SplitVaultArticle(data)
	if err != nil {
		return h, nil, err
	}
	first := bytes.IndexByte(header, '\n') + 1
	last := bytes.LastIndex(header, []byte("\n---"))
	if last < first {
		return h, nil, errors.New("article YAML front matter is empty")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(header[first:last]))
	decoder.KnownFields(true)
	if err = decoder.Decode(&h); err != nil {
		return h, nil, fmt.Errorf("parse article YAML front matter: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return h, nil, errors.New("article YAML front matter has multiple documents")
	}
	var fields map[string]any
	if err = yaml.Unmarshal(header[first:last], &fields); err != nil {
		return h, nil, fmt.Errorf("parse article YAML fields: %w", err)
	}
	if h.ID == "" || h.Title == "" || h.CollectedAt == "" {
		return h, nil, errors.New("article header requires id, title and collected_at")
	}
	if h.SourceKind != "" && h.SourceKind != "article" && h.SourceKind != "mine" {
		return h, nil, fmt.Errorf("unsupported source_kind %q", h.SourceKind)
	}
	if _, err = time.Parse(time.RFC3339Nano, h.CollectedAt); err != nil {
		return h, nil, fmt.Errorf("parse collected_at: %w", err)
	}
	return h, body, nil
}

// vaultSlug retains Unicode letters, including Chinese, while excluding
// filesystem metacharacters and control characters. Names stay below 80 runes.
func vaultSlug(title string) string {
	var out []rune
	for _, r := range strings.TrimSpace(title) {
		if unicode.IsControl(r) || strings.ContainsRune(`/\\:*?"<>|`, r) {
			continue
		}
		if unicode.IsSpace(r) {
			r = '-'
		}
		if r == '-' && len(out) > 0 && out[len(out)-1] == '-' {
			continue
		}
		out = append(out, r)
		if len(out) == 80 {
			break
		}
	}
	result := strings.Trim(string(out), ". -")
	if result == "" {
		return "untitled"
	}
	return result
}
