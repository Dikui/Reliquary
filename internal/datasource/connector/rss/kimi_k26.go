package rss

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func (c *client) recoverKimiK26(ctx context.Context, doc *goquery.Document, content *goquery.Selection, pageURL *url.URL) (int, error) {
	if pageURL == nil || (pageURL.Hostname() != "www.kimi.ai" && pageURL.Hostname() != "kimi.ai") || pageURL.Path != "/blog/kimi-k2-6" {
		return 0, nil
	}
	placeholders := content.Find("div.animate-pulse")
	if placeholders.Length() != 9 {
		return 0, nil
	}
	flight, chunks, err := kimiFlightData(doc)
	if err != nil {
		return 0, err
	}
	components := [...]struct{ kind, height string }{
		{"benchmarkSheet", "160"}, {"chartCompare", "160"}, {"testimonialCarousel", ""},
		{"videoList", "320"}, {"videoList", "320"}, {"videoList", "320"},
		{"videoList", "320"}, {"videoList", "320"}, {"videoList", "320"}, {"benchmarkTable", "360"},
	}
	matches := kimiAnyComponent.FindAllStringSubmatchIndex(flight, -1)
	if len(matches) != len(components) {
		return 0, nil
	}
	var rendered [9]string
	placeholderIndex := 0
	for i, match := range matches {
		kind := flight[match[2]:match[3]]
		if kind != components[i].kind {
			return 0, nil
		}
		raw, err := kimiComponentRaw(flight, chunks, match)
		if err != nil {
			return 0, err
		}
		if kind == "testimonialCarousel" {
			if err := kimiK26TestimonialsPresent(raw, content.Text()); err != nil {
				return 0, err
			}
			continue
		}
		if !strings.Contains(placeholders.Eq(placeholderIndex).AttrOr("class", "")+placeholders.Eq(placeholderIndex).AttrOr("style", ""), components[i].height+"px") {
			return 0, nil
		}
		switch kind {
		case "benchmarkSheet":
			rendered[placeholderIndex], err = renderKimiK26Sheet(raw)
		case "chartCompare":
			rendered[placeholderIndex], err = renderKimiChart(raw)
		case "videoList":
			rendered[placeholderIndex], err = c.renderKimiVideos(ctx, raw, pageURL)
		case "benchmarkTable":
			rendered[placeholderIndex], err = renderKimiBenchmark(raw)
		}
		if err != nil {
			return 0, err
		}
		placeholderIndex++
	}
	if placeholderIndex != len(rendered) {
		return 0, fmt.Errorf("Kimi K2.6 widgets incomplete")
	}
	for i, body := range rendered {
		placeholders.Eq(i).ReplaceWithHtml(`<div data-reliquary-kimi-widget="true">` + body + `</div>`)
	}
	return len(rendered), nil
}

func kimiK26TestimonialsPresent(raw json.RawMessage, text string) error {
	var source struct {
		Items []struct {
			Quote string
		}
	}
	if err := json.Unmarshal(raw, &source); err != nil || len(source.Items) != 13 {
		return fmt.Errorf("Kimi K2.6 testimonials unavailable: %v", err)
	}
	body := strings.Join(strings.Fields(text), " ")
	for _, item := range source.Items {
		quote := strings.Join(strings.Fields(item.Quote), " ")
		if quote == "" || !strings.Contains(body, quote) {
			return fmt.Errorf("Kimi K2.6 testimonial missing from article")
		}
	}
	return nil
}

func renderKimiK26Sheet(raw json.RawMessage) (string, error) {
	var source struct {
		BenchmarkLabel, AriaLabel, FullscreenTitle, Description string
		Columns                                                 []struct{ Key, Label string }
		Rows                                                    []struct {
			Type, ID, Label string
			Values          map[string]json.RawMessage
		}
	}
	if err := decodeKimiData(raw, &source); err != nil || len(source.Columns) != 7 || len(source.Rows) != 7 || source.BenchmarkLabel == "" {
		return "", fmt.Errorf("Kimi K2.6 benchmark sheet incomplete: %v", err)
	}
	var out strings.Builder
	out.WriteString("<h4>" + html.EscapeString(source.BenchmarkLabel) + "</h4>")
	out.WriteString("<table><thead><tr><th>Benchmark</th>")
	for _, column := range source.Columns {
		if column.Key == "" || column.Label == "" {
			return "", fmt.Errorf("Kimi K2.6 benchmark column incomplete")
		}
		out.WriteString("<th>" + html.EscapeString(column.Label) + "</th>")
	}
	out.WriteString("</tr></thead><tbody>")
	metrics := 0
	for _, row := range source.Rows {
		if row.Label == "" {
			return "", fmt.Errorf("Kimi K2.6 benchmark row incomplete")
		}
		if row.Type == "section" {
			out.WriteString("<tr><th colspan=\"8\">" + html.EscapeString(row.Label) + "</th></tr>")
			continue
		}
		if row.Type != "" || len(row.Values) != len(source.Columns) {
			return "", fmt.Errorf("Kimi K2.6 benchmark values incomplete")
		}
		out.WriteString("<tr><th>" + html.EscapeString(row.Label) + "</th>")
		for _, column := range source.Columns {
			value := row.Values[column.Key]
			var emphasized bool
			if len(value) > 0 && value[0] == '{' {
				var cell struct {
					Value      json.RawMessage
					Emphasized bool
				}
				if err := decodeKimiData(value, &cell); err != nil {
					return "", err
				}
				value, emphasized = cell.Value, cell.Emphasized
			}
			label, err := kimiValue(value)
			if err != nil {
				return "", err
			}
			if emphasized {
				out.WriteString("<td><strong>" + html.EscapeString(label) + "</strong></td>")
			} else {
				out.WriteString(kimiCell(label))
			}
		}
		out.WriteString("</tr>")
		metrics++
	}
	out.WriteString("</tbody></table>")
	if metrics != 5 || source.Description == "" {
		return "", fmt.Errorf("Kimi K2.6 benchmark sheet missing results")
	}
	out.WriteString("<p>" + html.EscapeString(source.Description) + "</p>")
	return out.String(), nil
}
