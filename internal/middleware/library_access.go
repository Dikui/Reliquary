package middleware

import (
	"context"
	stderrors "errors"

	"github.com/gin-gonic/gin"
	apprepo "reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// Entry-base access stays within the authenticated workspace.
var ErrResourceNotFound = stderrors.New("resource not found")

// LibraryAccess captures the result of a successful Library access resolution.
// Stashed on gin.Context under LibraryAccessContextKey so handlers that
// need the resolved Library (e.g. to render
// resource projection in the response) can pull it without re-running the
// resolution.
type LibraryAccess struct {
	Library          *types.Library
	EffectiveSpaceID uint64
}

// LibraryAccessContextKey is the gin.Context key under which a successful
// Library access resolution is stored.
const LibraryAccessContextKey = "access.library_access"

// LibraryAccessFromContext returns the LibraryAccess stashed by the guard, if
// any. Handlers that don't care can just rely on the rewritten
// c.Request.Context() for space scoping.
func LibraryAccessFromContext(c *gin.Context) (*LibraryAccess, bool) {
	v, ok := c.Get(LibraryAccessContextKey)
	if !ok {
		return nil, false
	}
	a, ok := v.(*LibraryAccess)
	return a, ok
}

// LibraryLookup is the minimum surface ResolveLibraryAccess needs from the
// library service: a single method that turns an ID into a
// Library pointer (or repo.ErrLibraryNotFound). Defining
// it as a tiny dedicated interface keeps the guard testable without
// forcing test stubs to satisfy the full LibraryService surface.
type LibraryLookup interface {
	GetLibraryByID(ctx context.Context, id string) (*types.Library, error)
}

// EntryLookup mirrors LibraryLookup but for resolving a entry id
// (document id) back to its parent Library. Used by the chunk routes whose
// URL param is a entry_id, not a library_id.
type EntryLookup interface {
	GetEntryByIDOnly(ctx context.Context, id string) (*types.Entry, error)
}

// ChunkLookup mirrors LibraryLookup for resolving a chunk id back to its
// owning entry document, which then resolves to the parent Library.
// Used by the /chunks/by-id/:id routes that address chunks directly.
type ChunkLookup interface {
	GetChunkByIDOnly(ctx context.Context, id string) (*types.Chunk, error)
}

// LibraryIDResolver tells the guard how to find the library_id for a given
// request. Built-in resolvers below cover the param shapes we use:
// :id, :library_id, :entry_id (-> parent Library).
//
// On error, resolvers MUST return either a 4xx apperror (bad request /
// not found) or a generic Go error for transient/internal failures;
// the guard maps the latter to 503.
type LibraryIDResolver func(c *gin.Context) (string, error)

// LibraryIDFromParam returns a resolver that reads a fixed gin param.
func LibraryIDFromParam(param string) LibraryIDResolver {
	return func(c *gin.Context) (string, error) {
		v := c.Param(param)
		if v == "" {
			return "", apperrors.NewBadRequestError("missing " + param + " in path")
		}
		return v, nil
	}
}

// LibraryIDFromEntryIDParam reads `:entry_id` from the URL, looks
// up the entry document, and returns its Library id. Used by the chunk
// routes that address a chunk via /chunks/:entry_id.
//
// A genuine "not found" maps to 404; transient errors (DB hiccup,
// service unavailable) are surfaced as a plain Go error so the guard
// can return 503 instead of pretending the resource doesn't exist
// (a 404 here would also short-circuit any retry / monitoring).
func LibraryIDFromEntryIDParam(param string, kgService EntryLookup) LibraryIDResolver {
	return func(c *gin.Context) (string, error) {
		v := c.Param(param)
		if v == "" {
			return "", apperrors.NewBadRequestError("missing " + param + " in path")
		}
		k, err := kgService.GetEntryByIDOnly(c.Request.Context(), v)
		if err != nil {
			if isResourceNotFound(err) {
				return "", apperrors.NewNotFoundError("Entry not found")
			}
			return "", err
		}
		if k == nil {
			return "", apperrors.NewNotFoundError("Entry not found")
		}
		return k.LibraryID, nil
	}
}

// LibraryIDFromChunkIDParam walks chunk_id -> entry_id -> library_id.
// Used by /chunks/by-id/:id routes that address a chunk directly. The
// chunk's LibraryID is denormalised on the row, so a single
// lookup is enough — no need to chain through GetEntryByIDOnly.
//
// Not-found / transient split mirrors LibraryIDFromEntryIDParam.
func LibraryIDFromChunkIDParam(param string, chunkService ChunkLookup) LibraryIDResolver {
	return func(c *gin.Context) (string, error) {
		v := c.Param(param)
		if v == "" {
			return "", apperrors.NewBadRequestError("missing " + param + " in path")
		}
		ch, err := chunkService.GetChunkByIDOnly(c.Request.Context(), v)
		if err != nil {
			if isResourceNotFound(err) {
				return "", apperrors.NewNotFoundError("Chunk not found")
			}
			return "", err
		}
		if ch == nil {
			return "", apperrors.NewNotFoundError("Chunk not found")
		}
		if ch.LibraryID == "" {
			// Should-never-happen on a fresh schema; on legacy rows the
			// chunk effectively isn't resolvable to a Library so the client
			// gets the same 404 they'd get for a missing chunk rather
			// than a 500 that pollutes alerting.
			logger.Warnf(c.Request.Context(),
				"[library_access] chunk %s has empty library_id; treating as not-found", v)
			return "", apperrors.NewNotFoundError("Chunk not found")
		}
		return ch.LibraryID, nil
	}
}

// isResourceNotFound recognises the various "not found" sentinels we
// might see from the underlying services. Keeps the resolvers above
// from forcing every service to standardise on a single error type
// before this refactor is useful.
func isResourceNotFound(err error) bool {
	// ErrChunkNotFound is defined in the repository layer and aliased by the
	// service; match the canonical repo sentinel so this predicate depends
	// only on the repository package (Library / Entry / Chunk are all here).
	return stderrors.Is(err, apprepo.ErrLibraryNotFound) ||
		stderrors.Is(err, apprepo.ErrEntryNotFound) ||
		stderrors.Is(err, apprepo.ErrChunkNotFound) ||
		stderrors.Is(err, ErrResourceNotFound)
}

// RequireLibraryExists resolves the requested library and preserves missing-resource errors.
func RequireLibraryExists(resolveLibraryID LibraryIDResolver, libraryService LibraryLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := resolveLibraryID(c)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		access, err := resolveLibraryAccessOnce(c.Request.Context(), c, id, libraryService)
		switch {
		case stderrors.Is(err, errLibraryAccessUnauthorized):
			err = apperrors.NewUnauthorizedError("Unauthorized")
		case stderrors.Is(err, errLibraryAccessNotFound):
			err = apperrors.NewNotFoundError("library not found")
		case stderrors.Is(err, errLibraryAccessForbidden):
			err = apperrors.NewForbiddenError("library belongs to another workspace")
		case err != nil:
			err = apperrors.NewServiceUnavailableError("cannot verify library access")
		}
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		c.Set(LibraryAccessContextKey, access)
		c.Next()
	}
}

func resolveLibraryAccessOnce(ctx context.Context, c *gin.Context, libraryID string, libraryService LibraryLookup) (*LibraryAccess, error) {
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok || spaceID == 0 {
		return nil, errLibraryAccessUnauthorized
	}
	library, err := libraryService.GetLibraryByID(ctx, libraryID)
	if err != nil {
		if stderrors.Is(err, apprepo.ErrLibraryNotFound) {
			return nil, errLibraryAccessNotFound
		}
		return nil, err
	}
	if library == nil {
		return nil, errLibraryAccessNotFound
	}
	if library.SpaceID != spaceID {
		return nil, errLibraryAccessForbidden
	}
	return &LibraryAccess{Library: library, EffectiveSpaceID: spaceID}, nil
}

var (
	errLibraryAccessUnauthorized = stderrors.New("library_access: unauthorized")
	errLibraryAccessNotFound     = stderrors.New("library_access: not found")
	errLibraryAccessForbidden    = stderrors.New("library_access: forbidden")
)
