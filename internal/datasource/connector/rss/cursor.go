package rss

import (
	"net/url"
	"strings"
)

func cursorAdapter() SiteAdapter {
	return SiteAdapter{Name: "cursor", Rules: []SiteRule{{Domains: []string{"cursor.com"}, Path: "/blog"}}, listing: &listingProfile{skip: func(u *url.URL) bool { return strings.HasPrefix(u.Path, "/blog/topic/") }}}
}
