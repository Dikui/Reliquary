package rss

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestRecoverKimiK26RestoresAllPlaceholders(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{0})
	}))
	defer server.Close()
	columns := make([]map[string]string, 7)
	values := make(map[string]string, 7)
	for i := range columns {
		key := fmt.Sprintf("model%d", i)
		columns[i] = map[string]string{"key": key, "label": key}
		values[key] = "90"
	}
	rows := []map[string]any{{"type": "section", "label": "Reasoning"}}
	for i := 0; i < 5; i++ {
		rows = append(rows, map[string]any{"label": fmt.Sprintf("Metric %d", i), "values": values})
	}
	rows = append(rows, map[string]any{"type": "section", "label": "Coding"})
	sheet, _ := json.Marshal(map[string]any{"benchmarkLabel": "Benchmark", "description": "Results explained", "columns": columns, "rows": rows})
	quotes := make([]map[string]string, 13)
	var static strings.Builder
	for i := range quotes {
		quote := fmt.Sprintf("K2.6 testimonial number %d", i)
		quotes[i] = map[string]string{"quote": quote}
		fmt.Fprintf(&static, "<p>%s</p>", quote)
	}
	testimonials, _ := json.Marshal(map[string]any{"items": quotes})
	video := fmt.Sprintf(`{"column":1,"rounded":true,"title":"Video examples","titleAlign":"center","list":[{"url":%q,"type":"video","poster":"https://files.example/poster.png"}]}`, server.URL+"/clip.mp4")
	chart := `{"General Agents":[{"name":"Accuracy","data":[{"name":"Kimi K2.6","value":90}]}]}`
	benchmark := `{"data":[{"benchmark":"Vision","scores":[{"model":"Kimi K2.6","value":"90"}]}]}`
	kinds := []string{"benchmarkSheet", "chartCompare", "testimonialCarousel", "videoList", "videoList", "videoList", "videoList", "videoList", "videoList", "benchmarkTable"}
	data := []string{string(sheet), chart, string(testimonials), video, video, video, video, video, video, benchmark}
	var flight, placeholders strings.Builder
	for i, kind := range kinds {
		fmt.Fprintf(&flight, `%d:["$","pre",null,{"className":"language-%s","node":{"children":[{"type":"text","value":%q}]},"position":{}}]`+"\n", i+1, kind, data[i])
		if kind != "testimonialCarousel" {
			height := "320"
			if i < 2 {
				height = "160"
			} else if kind == "benchmarkTable" {
				height = "360"
			}
			fmt.Fprintf(&placeholders, `<div class="animate-pulse" style="height:%spx"></div>`, height)
		}
	}
	call, _ := json.Marshal([]any{1, flight.String()})
	page := `<html><body><article>` + static.String() + placeholders.String() + `</article><script>self.__next_f.push(` + string(call) + `)</script></body></html>`
	pageURL, _ := url.Parse("https://www.kimi.ai/blog/kimi-k2-6")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	count, err := (&client{httpClient: server.Client()}).recoverKimiK26(context.Background(), doc, doc.Find("article"), pageURL)
	article, _ := doc.Find("article").Html()
	if err != nil || count != 9 || strings.Count(article, "data-reliquary-kimi-widget") != 9 ||
		strings.Count(article, "data-reliquary-video") != 6 || strings.Contains(article, "animate-pulse") ||
		!strings.Contains(article, "Metric 4") || !strings.Contains(article, "Vision") || !strings.Contains(article, "Video examples") {
		t.Fatalf("K2.6 content incomplete: count=%d err=%v article=%s", count, err, article)
	}
	doc, _ = goquery.NewDocumentFromReader(strings.NewReader(strings.Replace(page, "height:160px", "height:999px", 1)))
	count, err = (&client{httpClient: server.Client()}).recoverKimiK26(context.Background(), doc, doc.Find("article"), pageURL)
	if err != nil || count != 0 || doc.Find("article div.animate-pulse").Length() != 9 {
		t.Fatalf("mismatched widget must remain unresolved: count=%d err=%v", count, err)
	}
}
