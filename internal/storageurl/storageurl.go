package storageurl

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"reliquary/internal/logger"
	"reliquary/internal/types/interfaces"
)

// Pattern matches resource handles and local file paths, excluding Markdown
// and HTML delimiters from the reference.
var Pattern = regexp.MustCompile(`\b(?:resource://[0-9A-Za-z_-]+|local://[^\s)\]>"]+)`)

// IsHTTPURL reports whether s is an http(s) URL — the only form an external
// client can fetch; any provider scheme (local:// or resource://) is not. Scheme
// match is case-insensitive per RFC 3986 §3.1: a backend may emit an
// operator-configured host (e.g. OBS_PROXY_DOMAIN) with an uppercase scheme.
func IsHTTPURL(s string) bool {
	return len(s) >= 7 && strings.EqualFold(s[:7], "http://") ||
		len(s) >= 8 && strings.EqualFold(s[:8], "https://")
}

// Resolver maps a resource or local file reference to its file service.
type Resolver interface {
	// ResolveFileService returns nil when no backend can serve ref.
	ResolveFileService(ref string) interfaces.FileService
}

type Rewriter struct {
	resolver  Resolver
	logPrefix string

	mu   sync.Mutex
	memo map[string]string
}

func NewRewriter(r Resolver, logPrefix string) *Rewriter {
	return &Rewriter{resolver: r, logPrefix: logPrefix, memo: make(map[string]string)}
}

// Enabled reports whether this Rewriter can actually rewrite anything.
func (w *Rewriter) Enabled() bool {
	return w != nil && w.resolver != nil
}

// String replaces every storage reference in content with an HTTP URL.
// References that are already HTTP, that no backend claims, or that resolve to
// a non-HTTP location are left untouched so the caller degrades to the
// authenticated file proxy rather than emitting an unfetchable URL.
func (w *Rewriter) String(ctx context.Context, content string) string {
	if !w.Enabled() || content == "" {
		return content
	}
	return Pattern.ReplaceAllStringFunc(content, func(ref string) string {
		return w.ref(ctx, ref)
	})
}

// Ref rewrites a value that is a bare storage reference rather than prose
// containing one — for example `MessageImage.URL`.
func (w *Rewriter) Ref(ctx context.Context, ref string) string {
	if !w.Enabled() || ref == "" {
		return ref
	}
	if !Pattern.MatchString(ref) {
		return ref
	}
	return w.String(ctx, ref)
}

// ref resolves one reference while ensuring concurrent duplicates share the
// same generated URL.
func (w *Rewriter) ref(ctx context.Context, ref string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if cached, ok := w.memo[ref]; ok {
		return cached
	}
	resolved := w.resolve(ctx, ref)
	w.memo[ref] = resolved
	return resolved
}

func (w *Rewriter) resolve(ctx context.Context, ref string) string {
	fileSvc := w.resolver.ResolveFileService(ref)
	if fileSvc == nil {
		logger.Warnf(ctx, "[%s] storage URL rewrite: no file service for src=%s", w.logPrefix, ref)
		return ref
	}
	httpURL, err := fileSvc.GetFileURL(ctx, ref)
	if err != nil {
		logger.Warnf(ctx, "[%s] storage URL rewrite failed: src=%s err=%v", w.logPrefix, ref, err)
		return ref
	}
	// A non-http(s) result cannot be fetched by the client — this covers both an
	// unchanged no-op and a resource:// alias left as an internal local://
	// path (APP_EXTERNAL_URL unset / nginx not proxying /r/).
	if !IsHTTPURL(httpURL) {
		logger.Warnf(ctx,
			"[%s] storage URL rewrite no-op (resolved to non-HTTP URL %q; for local/private storage set "+
				"APP_EXTERNAL_URL and ensure nginx proxies /r/): src=%s",
			w.logPrefix, httpURL, ref)
		return ref
	}
	logger.Infof(ctx, "[%s] storage URL rewrite: src=%s", w.logPrefix, ref)
	logger.Debugf(ctx, "[%s] storage URL rewrite dst=%s", w.logPrefix, httpURL)
	return httpURL
}

// Rewrite is the one-shot form of Rewriter.String for callers that translate a
// single self-contained message.
func Rewrite(ctx context.Context, content string, r Resolver, logPrefix string) string {
	return NewRewriter(r, logPrefix).String(ctx, content)
}
