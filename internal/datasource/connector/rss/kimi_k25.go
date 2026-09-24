package rss

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var kimiAnyComponent = regexp.MustCompile(`"className":"language-([^"]+)"`)
var kimiComponentValue = regexp.MustCompile(`"value":("(?:[^"\\]|\\.)*")`)

func (c *client) recoverKimiK25(ctx context.Context, doc *goquery.Document, content *goquery.Selection, pageURL *url.URL) (int, error) {
	if pageURL == nil || (pageURL.Hostname() != "www.kimi.ai" && pageURL.Hostname() != "kimi.ai") || pageURL.Path != "/blog/kimi-k2-5" {
		return 0, nil
	}
	placeholders := content.Find("div.animate-pulse")
	if placeholders.Length() != 8 {
		return 0, nil
	}
	flight, chunks, err := kimiFlightData(doc)
	if err != nil {
		return 0, err
	}
	kinds := []string{"chartCompare", "videoList", "videoList", "singleChat", "videoList", "codeDemo", "codeDemo", "benchmarkTable"}
	heights := []string{"160", "320", "320", "160", "320", "160", "160", "360"}
	matches := kimiAnyComponent.FindAllStringSubmatchIndex(flight, -1)
	if len(matches) != len(kinds) {
		return 0, nil
	}
	var rendered [8]string
	for i, match := range matches {
		kind := flight[match[2]:match[3]]
		if kind != kinds[i] || !strings.Contains(placeholders.Eq(i).AttrOr("class", "")+placeholders.Eq(i).AttrOr("style", ""), heights[i]+"px") {
			return 0, nil
		}
		boundary := strings.Index(flight[match[1]:], `"position":`)
		if boundary < 0 {
			return 0, fmt.Errorf("Kimi K2.5 %s component incomplete", kind)
		}
		value := kimiComponentValue.FindStringSubmatch(flight[match[1] : match[1]+boundary])
		if len(value) != 2 {
			return 0, fmt.Errorf("Kimi K2.5 %s data unavailable", kind)
		}
		var source string
		if err := json.Unmarshal([]byte(value[1]), &source); err != nil {
			return 0, err
		}
		raw := json.RawMessage(source)
		if strings.HasPrefix(source, "$") {
			raw = chunks[strings.TrimPrefix(source, "$")]
		}
		if !json.Valid(raw) {
			return 0, fmt.Errorf("Kimi K2.5 %s data invalid", kind)
		}
		switch kind {
		case "chartCompare":
			rendered[i], err = renderKimiChart(raw)
		case "benchmarkTable":
			rendered[i], err = renderKimiBenchmark(raw)
		case "videoList":
			rendered[i], err = c.renderKimiVideos(ctx, raw, pageURL)
		case "codeDemo":
			rendered[i], err = c.renderKimiDemos(ctx, raw, pageURL)
		case "singleChat":
			rendered[i], err = renderKimiChat(raw, pageURL)
		}
		if err != nil {
			return 0, err
		}
	}
	for i, body := range rendered {
		placeholders.Eq(i).ReplaceWithHtml(`<div data-reliquary-kimi-widget="true">` + body + `</div>`)
	}
	return len(rendered), nil
}

func kimiMediaURL(raw string, pageURL *url.URL) (string, error) {
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || ref.User != nil || raw == "" {
		return "", fmt.Errorf("Kimi media URL invalid")
	}
	resolved := pageURL.ResolveReference(ref)
	if resolved.Scheme != "https" {
		return "", fmt.Errorf("Kimi media URL must use HTTPS")
	}
	return resolved.String(), nil
}

func (c *client) kimiVideoLink(ctx context.Context, raw, poster, label string, pageURL *url.URL) (string, error) {
	videoURL, err := kimiMediaURL(raw, pageURL)
	if err != nil {
		return "", err
	}
	if err := c.verifyVideoURL(ctx, videoURL); err != nil {
		return "", err
	}
	var out strings.Builder
	if poster != "" {
		posterURL, err := kimiMediaURL(poster, pageURL)
		if err != nil {
			return "", err
		}
		out.WriteString(`<img src="` + html.EscapeString(posterURL) + `" alt="视频封面">`)
	}
	if label == "" {
		label = "播放原始视频"
	}
	out.WriteString(`<p><a data-reliquary-video="true" href="` + html.EscapeString(videoURL) + `">` + html.EscapeString(label) + `</a></p>`)
	return out.String(), nil
}

func (c *client) renderKimiVideos(ctx context.Context, raw json.RawMessage, pageURL *url.URL) (string, error) {
	var source struct {
		Column int `json:"column"`
		List   []struct {
			URL, Type, Poster, Caption string
		} `json:"list"`
	}
	if err := decodeKimiData(raw, &source); err != nil || len(source.List) == 0 {
		return "", fmt.Errorf("Kimi video list incomplete: %v", err)
	}
	var out strings.Builder
	for _, item := range source.List {
		if item.Type != "video" {
			return "", fmt.Errorf("Kimi video type unsupported: %s", item.Type)
		}
		link, err := c.kimiVideoLink(ctx, item.URL, item.Poster, item.Caption, pageURL)
		if err != nil {
			return "", err
		}
		out.WriteString(link)
	}
	return out.String(), nil
}

func (c *client) renderKimiDemos(ctx context.Context, raw json.RawMessage, pageURL *url.URL) (string, error) {
	var demos []struct {
		Title, Desc, Label, Intro, Type, Result string
	}
	if err := decodeKimiData(raw, &demos); err != nil || len(demos) == 0 {
		return "", fmt.Errorf("Kimi demos incomplete: %v", err)
	}
	var out strings.Builder
	for _, demo := range demos {
		if demo.Type != "video" || demo.Title == "" {
			return "", fmt.Errorf("Kimi demo incomplete or unsupported")
		}
		link, err := c.kimiVideoLink(ctx, demo.Result, "", demo.Label, pageURL)
		if err != nil {
			return "", err
		}
		out.WriteString("<h4>" + html.EscapeString(demo.Title) + "</h4>")
		for _, paragraph := range []string{demo.Desc, demo.Intro} {
			if paragraph != "" {
				out.WriteString("<p>" + html.EscapeString(paragraph) + "</p>")
			}
		}
		out.WriteString(link)
	}
	return out.String(), nil
}

func renderKimiChat(raw json.RawMessage, pageURL *url.URL) (string, error) {
	var source struct {
		Messages []struct {
			Role   string                       `json:"role"`
			Blocks []map[string]json.RawMessage `json:"blocks"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &source); err != nil || len(source.Messages) == 0 {
		return "", fmt.Errorf("Kimi chat unavailable: %v", err)
	}
	var out strings.Builder
	out.WriteString("<h4>示例对话</h4>")
	for _, message := range source.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			return "", fmt.Errorf("Kimi chat role unsupported")
		}
		out.WriteString("<h5>" + html.EscapeString(message.Role) + "</h5>")
		for _, block := range message.Blocks {
			for kind, value := range block {
				switch kind {
				case "id", "parentId", "messageId", "createTime", "multiStage", "stage":
					continue
				case "text", "think":
					var content struct{ Content string }
					if err := json.Unmarshal(value, &content); err != nil || content.Content == "" {
						return "", fmt.Errorf("Kimi chat %s incomplete", kind)
					}
					if kind == "think" {
						out.WriteString("<h6>推理过程</h6>")
					}
					out.WriteString("<p>" + html.EscapeString(content.Content) + "</p>")
				case "file":
					var file struct {
						Meta struct{ Name, ContentType string }
						Blob struct {
							SignURL string `json:"signUrl"`
						}
					}
					if err := json.Unmarshal(value, &file); err != nil || !strings.HasPrefix(file.Meta.ContentType, "image/") {
						return "", fmt.Errorf("Kimi chat file unsupported")
					}
					imageURL, err := kimiMediaURL(file.Blob.SignURL, pageURL)
					if err != nil {
						return "", err
					}
					out.WriteString(`<img src="` + html.EscapeString(imageURL) + `" alt="` + html.EscapeString(file.Meta.Name) + `">`)
				case "tool":
					tool, err := renderKimiChatTool(value, pageURL)
					if err != nil {
						return "", err
					}
					out.WriteString(tool)
				default:
					return "", fmt.Errorf("Kimi chat block %s unsupported", kind)
				}
			}
		}
	}
	return out.String(), nil
}

func renderKimiChatTool(raw json.RawMessage, pageURL *url.URL) (string, error) {
	var tool struct {
		Name     string `json:"name"`
		Args     string `json:"args"`
		Contents []struct {
			Text         string `json:"text"`
			ResourceLink struct {
				URL, MimeType, OriginalURL string
			} `json:"resourceLink"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &tool); err != nil || tool.Name == "" {
		return "", fmt.Errorf("Kimi chat tool unavailable")
	}
	var args struct {
		Code string `json:"code"`
	}
	if err := decodeKimiData([]byte(tool.Args), &args); err != nil || args.Code == "" {
		return "", fmt.Errorf("Kimi chat tool code unavailable")
	}
	var out strings.Builder
	out.WriteString("<h6>" + html.EscapeString(tool.Name) + "</h6><pre>" + html.EscapeString(args.Code) + "</pre>")
	for _, content := range tool.Contents {
		if content.Text != "" {
			out.WriteString("<pre>" + html.EscapeString(content.Text) + "</pre>")
		}
		if content.ResourceLink.URL != "" {
			if !strings.HasPrefix(content.ResourceLink.MimeType, "image/") {
				return "", fmt.Errorf("Kimi chat tool media unsupported")
			}
			imageURL, err := kimiMediaURL(content.ResourceLink.OriginalURL, pageURL)
			if err != nil {
				return "", err
			}
			out.WriteString(`<img src="` + html.EscapeString(imageURL) + `" alt="工具结果图片">`)
		}
		if content.Text == "" && content.ResourceLink.URL == "" {
			return "", fmt.Errorf("Kimi chat tool result empty")
		}
	}
	return out.String(), nil
}
