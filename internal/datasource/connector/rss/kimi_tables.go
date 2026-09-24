package rss

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var kimiFlightText = regexp.MustCompile(`(?m)^([0-9a-f]+):T([0-9a-f]+),`)
var kimiFlightValue = regexp.MustCompile(`"value":"\$([0-9a-f]+)"`)
var kimiFlightComponent = regexp.MustCompile(`"className":"language-(chartCompare|benchmarkTable)"`)

// recoverKimiTables restores chart/table data that Kimi streams separately
// from the server-rendered article. Other unresolved widgets still fail the
// ordinary completeness check.
func recoverKimiTables(doc *goquery.Document, content *goquery.Selection, pageURL *url.URL) (int, error) {
	if pageURL == nil || (pageURL.Hostname() != "www.kimi.ai" && pageURL.Hostname() != "kimi.ai") || !strings.HasPrefix(pageURL.Path, "/blog/") {
		return 0, nil
	}
	placeholders := content.Find("div.animate-pulse")
	if placeholders.Length() == 0 {
		return 0, nil
	}
	flight, chunks, err := kimiFlightData(doc)
	if err != nil {
		return 0, err
	}
	if flight == "" {
		return 0, nil
	}
	var chartTable, benchmarkTable string
	for _, match := range kimiFlightComponent.FindAllStringSubmatchIndex(flight, -1) {
		kind := flight[match[2]:match[3]]
		boundary := strings.Index(flight[match[1]:], `"position":`)
		if boundary < 0 {
			return 0, fmt.Errorf("Kimi %s component incomplete", kind)
		}
		end := match[1] + boundary
		ref := kimiFlightValue.FindStringSubmatch(flight[match[1]:end])
		if len(ref) != 2 || !json.Valid(chunks[ref[1]]) {
			return 0, fmt.Errorf("Kimi %s data unavailable", kind)
		}
		var err error
		if kind == "chartCompare" {
			if chartTable != "" {
				return 0, fmt.Errorf("Kimi chart component repeated")
			}
			chartTable, err = renderKimiChart(chunks[ref[1]])
		} else {
			if benchmarkTable != "" {
				return 0, fmt.Errorf("Kimi benchmark component repeated")
			}
			benchmarkTable, err = renderKimiBenchmark(chunks[ref[1]])
		}
		if err != nil {
			return 0, err
		}
	}
	if chartTable == "" || benchmarkTable == "" || placeholders.Length() != 2 {
		return 0, nil
	}
	chartPlaceholder := placeholders.FilterFunction(func(_ int, node *goquery.Selection) bool {
		return !node.Parent().Is("[data-blog-hero-width]")
	})
	benchmarkPlaceholder := placeholders.FilterFunction(func(_ int, node *goquery.Selection) bool {
		return node.Parent().Is("[data-blog-hero-width]")
	})
	if chartPlaceholder.Length() != 1 || benchmarkPlaceholder.Length() != 1 {
		return 0, nil
	}
	chartPlaceholder.ReplaceWithHtml(chartTable)
	benchmarkPlaceholder.ReplaceWithHtml(benchmarkTable)
	return 2, nil
}

func kimiFlightData(doc *goquery.Document) (string, map[string]json.RawMessage, error) {
	var stream strings.Builder
	doc.Find("script").Each(func(_ int, script *goquery.Selection) {
		text := strings.TrimSpace(script.Text())
		const prefix = "self.__next_f.push("
		if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, ")") {
			return
		}
		var call []json.RawMessage
		if json.Unmarshal([]byte(text[len(prefix):len(text)-1]), &call) != nil || len(call) != 2 || string(call[0]) != "1" {
			return
		}
		var chunk string
		if json.Unmarshal(call[1], &chunk) == nil {
			stream.WriteString(chunk)
		}
	})
	flight := stream.String()
	if flight == "" {
		return "", nil, nil
	}
	chunks := map[string]json.RawMessage{}
	for pos := 0; pos < len(flight); {
		match := kimiFlightText.FindStringSubmatchIndex(flight[pos:])
		if match == nil {
			break
		}
		start := pos + match[1]
		length, err := strconv.ParseUint(flight[pos+match[4]:pos+match[5]], 16, 32)
		if err != nil || length > uint64(len(flight)-start) {
			return "", nil, fmt.Errorf("Kimi embedded data truncated")
		}
		chunks[flight[pos+match[2]:pos+match[3]]] = json.RawMessage(flight[start : start+int(length)])
		pos = start + int(length)
	}
	return flight, chunks, nil
}

func kimiValue(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", fmt.Errorf("Kimi table value missing")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String(), nil
	}
	return "", fmt.Errorf("Kimi table value is not text or number")
}

func kimiCell(text string) string { return "<td>" + html.EscapeString(text) + "</td>" }

func decodeKimiData(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func renderKimiChart(raw json.RawMessage) (string, error) {
	var charts map[string]json.RawMessage
	if err := json.Unmarshal(raw, &charts); err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString(`<div data-reliquary-kimi-table="chart">`)
	rows := 0
	for title, data := range charts {
		if title == "options" {
			continue
		}
		var series []struct {
			Name string `json:"name"`
			Data []struct {
				Name, Category string
				Value          json.RawMessage `json:"value"`
				BarColor       string          `json:"barColor"`
			} `json:"data"`
		}
		if err := decodeKimiData(data, &series); err != nil {
			return "", err
		}
		out.WriteString("<h4>" + html.EscapeString(title) + `</h4><table><thead><tr><th>Metric</th><th>Model</th><th>Category</th><th>Value</th></tr></thead><tbody>`)
		for _, group := range series {
			for _, point := range group.Data {
				value, err := kimiValue(point.Value)
				if err != nil || group.Name == "" || point.Name == "" {
					return "", fmt.Errorf("Kimi chart data incomplete")
				}
				out.WriteString("<tr>" + kimiCell(group.Name) + kimiCell(point.Name) + kimiCell(point.Category) + kimiCell(value) + "</tr>")
				rows++
			}
		}
		out.WriteString("</tbody></table>")
	}
	out.WriteString("</div>")
	if rows == 0 {
		return "", fmt.Errorf("Kimi chart data empty")
	}
	return out.String(), nil
}

func renderKimiBenchmark(raw json.RawMessage) (string, error) {
	var source struct {
		Options json.RawMessage `json:"options"`
		Data    []struct {
			Benchmark, Type, Comment, Metric string
			Scores                           []struct {
				Model string          `json:"model"`
				Value json.RawMessage `json:"value"`
			} `json:"scores"`
		} `json:"data"`
	}
	if err := decodeKimiData(raw, &source); err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString(`<div data-reliquary-kimi-table="benchmark"><table><thead><tr><th>Benchmark</th><th>Type</th><th>Comment</th><th>Metric</th><th>Model</th><th>Value</th></tr></thead><tbody>`)
	rows := 0
	for _, benchmark := range source.Data {
		for _, score := range benchmark.Scores {
			value, err := kimiValue(score.Value)
			if err != nil || benchmark.Benchmark == "" || score.Model == "" {
				return "", fmt.Errorf("Kimi benchmark data incomplete")
			}
			out.WriteString("<tr>" + kimiCell(benchmark.Benchmark) + kimiCell(benchmark.Type) + kimiCell(benchmark.Comment) + kimiCell(benchmark.Metric) + kimiCell(score.Model) + kimiCell(value) + "</tr>")
			rows++
		}
	}
	out.WriteString("</tbody></table></div>")
	if rows == 0 {
		return "", fmt.Errorf("Kimi benchmark data empty")
	}
	return out.String(), nil
}
