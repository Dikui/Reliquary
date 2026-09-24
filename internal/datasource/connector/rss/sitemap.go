package rss

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

func parseSitemap(data []byte) ([]sitemapURL, error) {
	var document struct {
		XMLName xml.Name     `xml:"urlset"`
		URLs    []sitemapURL `xml:"url"`
	}
	if err := xml.Unmarshal(data, &document); err != nil || document.XMLName.Local != "urlset" {
		return nil, fmt.Errorf("not a sitemap URL set")
	}
	return document.URLs, nil
}

func sitemapArticleURL(raw string, sitemap *url.URL, pathPrefix string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && sitemap != nil && u.User == nil && u.Scheme == sitemap.Scheme &&
		strings.EqualFold(u.Host, sitemap.Host) && strings.HasPrefix(u.Path, pathPrefix) &&
		u.Path != pathPrefix
}

func parseSiteDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if parts := strings.Fields(value); len(parts) == 3 && len(parts[0]) > 2 {
		day := parts[0]
		switch day[len(day)-2:] {
		case "st", "nd", "rd", "th":
			if _, err := strconv.Atoi(day[:len(day)-2]); err == nil {
				parts[0] = day[:len(day)-2]
				value = strings.Join(parts, " ")
			}
		}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123Z, time.RFC1123, "2006-01-02", "January 2, 2006", "Jan 2, 2006", "2 January 2006"} {
		if date, err := time.Parse(layout, value); err == nil {
			return date, true
		}
	}
	return time.Time{}, false
}

func articlePublishedAt(doc *goquery.Document, articleURL string) string {
	for _, selector := range []string{
		`meta[property="article:published_time"]`, `meta[name="date"]`, `meta[itemprop="datePublished"]`,
	} {
		if value, ok := doc.Find(selector).First().Attr("content"); ok {
			if _, valid := parseSiteDate(value); valid {
				return strings.TrimSpace(value)
			}
		}
	}
	var structuredDate string
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, script *goquery.Selection) bool {
		var article struct {
			Type          string `json:"@type"`
			URL           string `json:"url"`
			ID            string `json:"@id"`
			MainEntity    string `json:"mainEntityOfPage"`
			DatePublished string `json:"datePublished"`
		}
		if json.Unmarshal([]byte(script.Text()), &article) != nil ||
			(article.Type != "BlogPosting" && article.Type != "NewsArticle" && article.Type != "Article") {
			return true
		}
		current, currentErr := url.Parse(articleURL)
		for _, raw := range []string{article.MainEntity, article.URL, article.ID} {
			claimed, err := url.Parse(raw)
			if currentErr == nil && err == nil && claimed.Scheme == current.Scheme &&
				strings.EqualFold(claimed.Host, current.Host) &&
				strings.TrimRight(claimed.Path, "/") == strings.TrimRight(current.Path, "/") {
				if _, valid := parseSiteDate(article.DatePublished); valid {
					structuredDate = strings.TrimSpace(article.DatePublished)
					return false
				}
			}
		}
		return true
	})
	if structuredDate != "" {
		return structuredDate
	}
	current, currentErr := url.Parse(articleURL)
	var linkedDate string
	doc.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		if currentErr != nil {
			return false
		}
		claimed, err := current.Parse(link.AttrOr("href", ""))
		if err != nil || !strings.EqualFold(claimed.Host, current.Host) || claimed.Path == "/" ||
			!strings.HasPrefix(current.Path, strings.TrimRight(claimed.Path, "/")+"/") {
			return true
		}
		if _, valid := parseSiteDate(link.Text()); valid {
			linkedDate = strings.TrimSpace(link.Text())
			return false
		}
		return true
	})
	if linkedDate != "" {
		return linkedDate
	}
	heading := doc.Find("article h1, main h1").First()
	if heading.Length() == 0 {
		heading = doc.Find("h1").First()
	}
	for _, candidate := range []string{
		heading.Parent().Find("time").First().AttrOr("datetime", ""),
		heading.Next().Find("time, span, p").First().Text(),
		heading.Next().Find("time").First().AttrOr("datetime", ""),
		heading.Next().Find("p").First().Text(),
		heading.Next().Text(),
	} {
		if _, valid := parseSiteDate(candidate); valid {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}
