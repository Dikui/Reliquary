package service

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf16"

	"golang.org/x/net/html"
	"reliquary/internal/types"
)

var (
	sourceURLPattern      = regexp.MustCompile(`https?://[^\s<>"'\x00-\x1f]+`)
	doiPattern            = regexp.MustCompile(`(?i)\b10\.[0-9]{4,9}/[-._;()/:A-Z0-9]+`)
	arxivPattern          = regexp.MustCompile(`(?i)(?:arxiv\s*:\s*|arxiv\.org/(?:abs|pdf)/)([0-9]{4}\.[0-9]{4,5}(?:v[0-9]+)?|[a-z][a-z.\-]+/[0-9]{7}(?:v[0-9]+)?)`)
	explicitSourcePattern = regexp.MustCompile(`(?im)(?:(?:原文链接|原文地址|来源)\s*[:：]?|\b(?:source|original\s+(?:url|link))\s*[:：])\s*([^\r\n]+)`)
	pdfInfoRefPattern     = regexp.MustCompile(`/Info\s+([0-9]+)\s+([0-9]+)\s+R`)
	pdfLiteralPattern     = regexp.MustCompile(`(?s)/[A-Za-z][A-Za-z0-9]*\s*(?:\(((?:\\.|[^\\)])*)\)|<([0-9A-Fa-f\s]+)>)`)
	pdfXMPPattern         = regexp.MustCompile(`(?is)<(?:dc:(?:identifier|source)|prism:(?:doi|url)|pdfx:(?:url|doi|source)|xmp:identifier)[^>]*>(.*?)</(?:dc:(?:identifier|source)|prism:(?:doi|url)|pdfx:(?:url|doi|source)|xmp:identifier)>`)
)

// extractIntakeOriginalLink only inspects supplied bytes and parser output.
// The extracted URL is provenance; this function performs no network requests.
func extractIntakeOriginalLink(k *types.Knowledge, body string, raw []byte, fileType string, metadata map[string]string) error {
	intake, err := k.IntakeMetadata()
	if err != nil || intake == nil || strings.TrimSpace(intake.SourceInfo.OriginalURL) != "" {
		return err
	}
	link := extractOriginalSourceLink(body, raw, fileType, metadata)
	if link == "" {
		return nil
	}
	intake.SourceInfo.OriginalURL, intake.SourceInfo.OriginalURLSource = link, "from_file"
	return k.SetIntakeMetadata(intake)
}

func extractOriginalSourceLink(body string, raw []byte, fileType string, metadata map[string]string) string {
	for _, key := range []string{"original_url", "url", "URL", "pdf_url", "canonical", "og:url"} {
		if value := sourceLinkURL(metadata[key]); value != "" {
			return value
		}
	}
	pdfMetadata := ""
	if normalizeFileExtension(fileType) == "pdf" {
		pdfMetadata = extractPDFSourceMetadata(raw)
		if value := firstSourceURL(pdfMetadata); value != "" {
			return value
		}
	}
	arxiv := ""
	if metadata["arxiv"] != "" {
		arxiv = "arXiv: " + metadata["arxiv"]
	}
	identifiers := strings.Join([]string{metadata["doi"], metadata["DOI"], arxiv, pdfMetadata, body}, "\n")
	if match := doiPattern.FindString(identifiers); match != "" {
		return "https://doi.org/" + trimSourceIdentifier(match)
	}
	if match := arxivPattern.FindStringSubmatch(identifiers); len(match) == 2 {
		return "https://arxiv.org/abs/" + match[1]
	}
	for _, text := range []string{string(raw), body} {
		if value := extractHTMLSourceLink(text); value != "" {
			return value
		}
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
func extractPDFSourceMetadata(raw []byte) string {
	text := string(raw)
	values := []string{}
	if refs := pdfInfoRefPattern.FindAllStringSubmatch(text, -1); len(refs) > 0 {
		ref := refs[len(refs)-1]
		object := regexp.MustCompile(fmt.Sprintf(`(?s)\b%s\s+%s\s+obj\b(.*?)endobj`, ref[1], ref[2]))
		if match := object.FindStringSubmatch(text); len(match) == 2 {
			for _, literal := range pdfLiteralPattern.FindAllStringSubmatch(match[1], -1) {
				value := decodePDFMetadataString(literal[1], literal[2])
				values = append(values, value)
			}
		}
	}
	xmpTexts := []string{text}
	// Compressed XMP is common in generated PDFs. Limit each stream to 1 MiB
	// of output so a small uploaded file cannot inflate unbounded metadata.
	streamPattern := regexp.MustCompile(`(?s)<<([^<>]*(?:/Type\s*/Metadata|/Subtype\s*/XML)[^<>]*)>>\s*stream\r?\n(.*?)\r?\nendstream`)
	for _, stream := range streamPattern.FindAllSubmatch(raw, -1) {
		if bytes.Contains(stream[1], []byte("/FlateDecode")) {
			reader, err := zlib.NewReader(bytes.NewReader(stream[2]))
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(reader, 1<<20))
			_ = reader.Close()
			if err == nil {
				xmpTexts = append(xmpTexts, string(data))
			}
		}
	}
	for _, xmp := range xmpTexts {
		for _, match := range pdfXMPPattern.FindAllStringSubmatch(xmp, -1) {
			values = append(values, html.UnescapeString(match[1]))
		}
	}
	return strings.Join(values, "\n")
}

func decodePDFMetadataString(literal, encoded string) string {
	data := []byte(strings.NewReplacer(`\(`, "(", `\)`, ")", `\\`, `\`, `\n`, "\n", `\r`, "\r").Replace(literal))
	if encoded != "" {
		encoded = strings.Join(strings.Fields(encoded), "")
		if len(encoded)%2 != 0 {
			encoded += "0"
		}
		decoded, err := hex.DecodeString(encoded)
		if err != nil {
			return ""
		}
		data = decoded
	}
	if len(data) >= 2 && ((data[0] == 0xfe && data[1] == 0xff) || (data[0] == 0xff && data[1] == 0xfe)) {
		little := data[0] == 0xff
		units := make([]uint16, 0, (len(data)-2)/2)
		for i := 2; i+1 < len(data); i += 2 {
			unit := uint16(data[i])<<8 | uint16(data[i+1])
			if little {
				unit = uint16(data[i+1])<<8 | uint16(data[i])
			}
			units = append(units, unit)
		}
		return string(utf16.Decode(units))
	}
	return string(data)
}
