package searchutil

import (
	"encoding/json"
	"strings"
	"testing"

	"reliquary/internal/types"
)

func TestSliceContentByDocumentRange(t *testing.T) {
	parent := "aaaPAGE1bbbPAGE2ccc"
	got := SliceContentByDocumentRange(parent, 100, 103, 108)
	want := "PAGE1"
	if got != want {
		t.Fatalf("slice: got %q, want %q", got, want)
	}
}

func TestFilterImageInfoByMatchRange(t *testing.T) {
	parent := "![p1](u1)\n\n![p2](u2)\n\n![p3](u3)"
	matchStart := len([]rune("![p1](u1)\n\n"))
	matchEnd := matchStart + len([]rune("![p2](u2)"))
	all := []types.ImageInfo{
		{URL: "u1"}, {URL: "u2"}, {URL: "u3"},
	}
	raw, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	got := FilterImageInfoByMatchRange(parent, 0, matchStart, matchEnd, string(raw))
	var filtered []types.ImageInfo
	if err := json.Unmarshal([]byte(got), &filtered); err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].URL != "u2" {
		t.Fatalf("filtered: %+v", filtered)
	}
}

func TestFilterImageInfoByContentURLs(t *testing.T) {
	content := "intro\n![page3](local://img3.jpg)\noutro"
	all := []types.ImageInfo{
		{URL: "local://img1.jpg", OCRText: "one"},
		{URL: "local://img3.jpg", OCRText: "three"},
	}
	raw, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	got := FilterImageInfoByContentURLs(content, string(raw))
	var filtered []types.ImageInfo
	if err := json.Unmarshal([]byte(got), &filtered); err != nil {
		t.Fatalf("unmarshal filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].URL != "local://img3.jpg" {
		t.Fatalf("filtered: %+v", filtered)
	}
}

func TestPruneMarkdownImagesOutsideRange(t *testing.T) {
	parent := "![p1](u1)\n\n![p2](u2)\n\n![p3](u3)"
	matchStart := len([]rune("![p1](u1)\n\n"))
	matchEnd := matchStart + len([]rune("![p2](u2)"))
	got := PruneMarkdownImagesOutsideRange(parent, 0, matchStart, matchEnd)
	if got != "![p2](u2)" {
		t.Fatalf("prune: got %q", got)
	}
}

func TestPruneMarkdownImagesByImageInfoIgnoresShiftedOffsets(t *testing.T) {
	content := "a manually inserted prefix that shifts every parser offset\n\n" +
		"![p1](u1)\n\nbody\n\n![p2](u2)"
	raw, err := json.Marshal([]types.ImageInfo{{URL: "u2"}})
	if err != nil {
		t.Fatal(err)
	}
	got := PruneMarkdownImagesByImageInfo(content, string(raw))
	if strings.Contains(got, "u1") {
		t.Fatalf("unscoped image remained: %q", got)
	}
	if !strings.Contains(got, "![p2](u2)") {
		t.Fatalf("scoped image was removed: %q", got)
	}
}

func TestImageURLsInContent(t *testing.T) {
	content := "![a](u1) x ![b](u2)"
	urls := ImageURLsInContent(content)
	if !urls["u1"] || !urls["u2"] || len(urls) != 2 {
		t.Fatalf("urls: %#v", urls)
	}
}
