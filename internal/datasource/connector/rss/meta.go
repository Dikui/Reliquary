package rss

import (
	readability "codeberg.org/readeck/go-readability/v2"
	"context"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"net/url"
	"strings"
)

func metaAdapter() SiteAdapter {
	return SiteAdapter{Name: "meta", Rules: []SiteRule{{Domains: []string{"ai.meta.com"}, Path: "/blog", Prefix: true}},
		listing: &listingProfile{date: func(link *goquery.Selection) string {
			var date string
			link.Closest("._amcy, ._amda").Find("._amun, ._amdj").EachWithBreak(func(_ int, node *goquery.Selection) bool {
				candidate := strings.TrimSpace(node.Text())
				if _, valid := parseSiteDate(candidate); valid {
					date = candidate
					return false
				}
				return true
			})
			return date
		}, improveTitle: func(current, candidate string) bool {
			return candidate != "" && candidate != "FEATURED" && (current == "" || current == "FEATURED")
		}},
		Recover: func(_ context.Context, page *SitePage) SiteRecovery {
			if err := preserveMetaHeroImage(page.Document, page.Article, page.URL); err != nil {
				return SiteRecovery{Reason: err.Error()}
			}
			return SiteRecovery{Success: true}
		},
	}
}

func preserveMetaHeroImage(doc *goquery.Document, article *readability.Article, pageURL *url.URL) error {
	header := doc.Find("h1").First().Closest("._amuh")
	if header.Length() != 1 {
		return fmt.Errorf("Meta article header unavailable")
	}
	hero := header.Find("img[src]")
	if hero.Length() > 1 {
		return fmt.Errorf("Meta article hero image ambiguous")
	}
	if hero.Length() == 0 {
		return nil
	}
	raw, _ := hero.Attr("src")
	raw = strings.TrimSpace(raw)
	ref, err := url.Parse(raw)
	if raw == "" || err != nil || ref.User != nil || (ref.Scheme != "" && ref.Scheme != "http" && ref.Scheme != "https") {
		return fmt.Errorf("Meta article hero image URL invalid")
	}
	imageURL := pageURL.ResolveReference(ref).String()
	if goquery.NewDocumentFromNode(article.Node).Find("img[src]").FilterFunction(func(_ int, img *goquery.Selection) bool {
		src, _ := img.Attr("src")
		return src == imageURL
	}).Length() > 0 {
		return nil
	}
	hero.SetAttr("src", imageURL)
	image := hero.Get(0)
	image.Parent.RemoveChild(image)
	article.Node.InsertBefore(image, article.Node.FirstChild)
	return nil
}
