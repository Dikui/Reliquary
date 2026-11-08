package types

import (
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
)

// ShelfParser is a named library-local parser. HTTP endpoints are explicitly
// configured trusted services and may be on the local network.
type ShelfParser struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	URL            string `json:"url,omitempty"`
	Authorization  string `json:"authorization,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	Concurrency    int    `json:"concurrency,omitempty"`
	ModelID        string `json:"model_id,omitempty"`
	RenderDPI      int    `json:"render_dpi,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
}

// All populated selectors in a rule must match; rule order is authoritative.
type ShelfParserRule struct {
	ParserKey     string `json:"parser_key"`
	Extension     string `json:"extension,omitempty"`
	RequirementID string `json:"requirement_id,omitempty"`
	FileCategory  string `json:"file_category,omitempty"`
}

type ShelfReparseInput struct {
	VersionID string `json:"version_id"`
	ParserKey string `json:"parser_key"`
}

type ShelfParsePayload struct {
	SpaceID   uint64 `json:"tenant_id"`
	VersionID string `json:"version_id"`
	JobID     string `json:"job_id"` // queue serialization identity is the version
	Attempt   int64  `json:"attempt"`
}

func (c ShelfConfig) Parser(key string) (ShelfParser, bool) {
	for _, p := range c.Parsers {
		if p.Key == key {
			return p, true
		}
	}
	return ShelfParser{}, false
}

func (c ShelfConfig) SelectParser(filename, requirementID, category string) string {
	return c.SelectParserForRequirements(filename, RequirementReferences(requirementID, nil), category)
}

func (c ShelfConfig) SelectParserForRequirements(filename string, requirementIDs []string, category string) string {
	extension := strings.ToLower(filepath.Ext(filename))
	for _, r := range c.ParserRules {
		if r.Extension != "" && strings.ToLower(r.Extension) != extension {
			continue
		}
		if r.RequirementID != "" && !slices.Contains(requirementIDs, r.RequirementID) {
			continue
		}
		if r.FileCategory != "" && r.FileCategory != category {
			continue
		}
		return r.ParserKey
	}
	return c.DefaultParser
}

func ValidateShelfParsers(c ShelfConfig) error {
	if len(c.Parsers) > 100 || len(c.ParserRules) > 500 {
		return fmt.Errorf("too many shelf parsers or rules")
	}
	keys, names := map[string]bool{}, map[string]bool{}
	for _, p := range c.Parsers {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if !entitySchemaKey.MatchString(p.Key) || keys[p.Key] || name == "" || names[name] || len([]rune(p.Name)) > 100 {
			return fmt.Errorf("parsers require unique keys and nonblank unique names of at most 100 characters")
		}
		keys[p.Key], names[name] = true, true
		switch p.Type {
		case "none":
			if p.URL != "" || p.Authorization != "" || p.TimeoutSeconds != 0 || p.Concurrency != 0 || p.ModelID != "" || p.RenderDPI != 0 || p.Prompt != "" {
				return fmt.Errorf("none parser cannot contain service or model settings")
			}
		case "http":
			u, err := url.Parse(p.URL)
			if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
				return fmt.Errorf("HTTP parser requires an HTTP(S) URL without credentials or fragment")
			}
			if p.TimeoutSeconds < 1 || p.TimeoutSeconds > 3600 || p.Concurrency < 1 || p.Concurrency > 100 || strings.ContainsAny(p.Authorization, "\r\n") || len(p.Authorization) > 8192 || p.ModelID != "" || p.RenderDPI != 0 || p.Prompt != "" {
				return fmt.Errorf("invalid HTTP parser timeout, concurrency or authentication")
			}
		case "vision":
			if strings.TrimSpace(p.ModelID) == "" || p.RenderDPI < 72 || p.RenderDPI > 600 || strings.TrimSpace(p.Prompt) == "" || len([]rune(p.Prompt)) > 20000 || p.URL != "" || p.Authorization != "" || p.TimeoutSeconds != 0 || p.Concurrency != 0 {
				return fmt.Errorf("vision parser requires model, render DPI (72-600) and prompt")
			}
		default:
			return fmt.Errorf("parser type must be http, vision or none")
		}
	}
	if len(c.Parsers) > 0 && !keys[c.DefaultParser] || len(c.Parsers) == 0 && (c.DefaultParser != "" || len(c.ParserRules) > 0) {
		return fmt.Errorf("default parser must reference a configured parser")
	}
	for _, r := range c.ParserRules {
		if !keys[r.ParserKey] || r.Extension == "" && r.RequirementID == "" && r.FileCategory == "" {
			return fmt.Errorf("parser rule requires a configured parser and at least one selector")
		}
		if r.Extension != "" && (!strings.HasPrefix(r.Extension, ".") || strings.ContainsAny(r.Extension, "/\\ *?\r\n") || len(r.Extension) > 30) {
			return fmt.Errorf("invalid parser rule extension")
		}
		if len([]rune(r.FileCategory)) > 20000 {
			return fmt.Errorf("parser rule file category is too long")
		}
	}
	return nil
}
