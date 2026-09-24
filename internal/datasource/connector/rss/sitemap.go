package rss

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
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
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123Z, time.RFC1123, "2006-01-02", "January 2, 2006", "Jan 2, 2006"} {
		if date, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return date, true
		}
	}
	return time.Time{}, false
}

func articlePublishedAt(doc *goquery.Document) string {
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
			DatePublished string `json:"datePublished"`
		}
		if json.Unmarshal([]byte(script.Text()), &article) == nil {
			if _, valid := parseSiteDate(article.DatePublished); valid {
				structuredDate = article.DatePublished
				return false
			}
		}
		return true
	})
	if structuredDate != "" {
		return structuredDate
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
