package service

import (
	"regexp"
	"strings"

	"reliquary/internal/types"

	"golang.org/x/net/html"
)

var (
	sourceURLPattern      = regexp.MustCompile(`https?://[^\s<>"'\x00-\x1f]+`)
	doiPattern            = regexp.MustCompile(`(?i)\b10\.[0-9]{4,9}/[-._;()/:A-Z0-9]+`)
	arxivPattern          = regexp.MustCompile(`(?i)(?:arxiv\s*:\s*|arxiv\.org/(?:abs|pdf)/)([0-9]{4}\.[0-9]{4,5}(?:v[0-9]+)?|[a-z][a-z.\-]+/[0-9]{7}(?:v[0-9]+)?)`)
	explicitSourcePattern = regexp.MustCompile(`(?im)(?:(?:原文链接|原文地址|来源)\s*[:：]?|\b(?:source|original\s+(?:url|link))\s*[:：])\s*([^\r\n]+)`)
)

// extractIntakeOriginalLink only inspects supplied Markdown and its source metadata.
// The extracted URL is provenance; this function performs no network requests.
func extractIntakeOriginalLink(k *types.Entry, body string, metadata map[string]string) error {
	if k.SourceKind == "mine" {
		return nil
	}
	intake, err := k.IntakeMetadata()
	if err != nil || intake == nil || strings.TrimSpace(intake.SourceInfo.OriginalURL) != "" {
		return err
	}
	link := extractOriginalSourceLink(body, metadata)
	if link == "" {
		return nil
	}
	intake.SourceInfo.OriginalURL, intake.SourceInfo.OriginalURLSource = link, "from_markdown"
	return k.SetIntakeMetadata(intake)
}

func extractOriginalSourceLink(body string, metadata map[string]string) string {
	for _, key := range []string{"original_url", "url", "URL", "canonical", "og:url"} {
		if value := sourceLinkURL(metadata[key]); value != "" {
			return value
		}
	}

	arxiv := ""
	if metadata["arxiv"] != "" {
		arxiv = "arXiv: " + metadata["arxiv"]
	}
	identifiers := strings.Join([]string{metadata["doi"], metadata["DOI"], arxiv, body}, "\n")
	if match := doiPattern.FindString(identifiers); match != "" {
		return "https://doi.org/" + trimSourceIdentifier(match)
	}
	if match := arxivPattern.FindStringSubmatch(identifiers); len(match) == 2 {
		return "https://arxiv.org/abs/" + match[1]
	}
	if value := extractHTMLSourceLink(body); value != "" {
		return value
	}
	for _, match := range explicitSourcePattern.FindAllStringSubmatch(body, -1) {
		if value := firstSourceURL(match[1]); value != "" {
			return value
		}
	}
	return ""
}

func sourceLinkURL(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimRight(value, ".,;，。；、")
	if validIngestURL(value) && len(value) <= 2048 {
		return value
	}
	return ""
}

func trimSourceIdentifier(value string) string {
	value = strings.TrimRight(value, ".,;:")
	return trimSourceClosingDelimiters(value)
}

func trimSourceClosingDelimiters(value string) string {
	for {
		trimmed := false
		for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"{", "}"}} {
			if strings.HasSuffix(value, pair[1]) && strings.Count(value, pair[1]) > strings.Count(value, pair[0]) {
				value = strings.TrimSuffix(value, pair[1])
				trimmed = true
				break
			}
		}
		if !trimmed {
			return value
		}
	}
}

func firstSourceURL(text string) string {
	for _, raw := range sourceURLPattern.FindAllString(text, -1) {
		// Markdown link closing delimiters are syntax, not source URL bytes.
		raw = trimSourceClosingDelimiters(raw)
		if value := sourceLinkURL(raw); value != "" {
			return value
		}
	}
	return ""
}

func extractHTMLSourceLink(text string) string {
	z := html.NewTokenizer(strings.NewReader(text))
	canonical, ogURL := "", ""
	for {
		switch z.Next() {
		case html.ErrorToken:
			if canonical != "" {
				return canonical
			}
			return ogURL
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			if token.Data != "link" && token.Data != "meta" {
				continue
			}
			attrs := map[string]string{}
			for _, attr := range token.Attr {
				attrs[strings.ToLower(attr.Key)] = attr.Val
			}
			if token.Data == "link" && canonical == "" {
				for _, rel := range strings.Fields(strings.ToLower(attrs["rel"])) {
					if rel == "canonical" {
						canonical = sourceLinkURL(attrs["href"])
					}
				}
			}
			if token.Data == "meta" && strings.EqualFold(attrs["property"], "og:url") && ogURL == "" {
				ogURL = sourceLinkURL(attrs["content"])
			}
		}
	}
}

// PDF Info dictionary strings and XMP identifiers are source metadata. Other
// arbitrary URLs in PDF streams are intentionally not treated as source URLs.
