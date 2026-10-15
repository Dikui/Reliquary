package search

import (
	"context"
	"errors"
	"testing"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type failingSearchLibraryService struct {
	interfaces.LibraryService
	err error
}

func (s *failingSearchLibraryService) GetLibrariesByIDsOnly(
	context.Context, []string,
) ([]*types.Library, error) {
	return []*types.Library{{
		ID:               "library-1",
		Type:             types.LibraryTypeDocument,
		EmbeddingModelID: "embedding-1",
		IndexingStrategy: types.DefaultIndexingStrategy(),
	}}, nil
}

func (s *failingSearchLibraryService) ResolveEmbeddingModelKeys(
	context.Context, []*types.Library,
) map[string]string {
	return map[string]string{"library-1": "doubao|https://ark.cn-beijing.volces.com/api/v3"}
}

func (s *failingSearchLibraryService) GetQueryEmbedding(
	context.Context, string, string,
) ([]float32, error) {
	return nil, s.err
}

func (s *failingSearchLibraryService) HybridSearch(
	context.Context, string, types.SearchParams,
) ([]*types.SearchResult, error) {
	return nil, s.err
}

// embedding 失败会先降级为关键词检索；本用例中关键词检索也失败，
// 此时必须仍报 search_failed 并保留根因，而不是伪装成"无结果"。
func TestSearchEmbeddingFailureIsNotReportedAsNoResults(t *testing.T) {
	rootCause := errors.New("embedding endpoint unavailable")
	plugin := &SearchStage{
		libraryService: &failingSearchLibraryService{err: rootCause},
	}
	state := &State{
		Request: Request{
			SearchTargets: types.SearchTargets{{
				Type:      types.SearchTargetTypeLibrary,
				LibraryID: "library-1",
			}},
			EmbeddingTopK: 10,
		},
		Results: Results{RewriteQuery: "精简邮件效果"},
	}

	err := plugin.Run(context.Background(), state, func() *StageError {
		return nil
	})
	if err == nil || err.ErrorType != ErrSearch.ErrorType {
		t.Fatalf("expected search_failed, got %#v", err)
	}
	if !errors.Is(err.Err, rootCause) {
		t.Fatalf("expected root cause to be preserved, got %v", err.Err)
	}
}

type degradingSearchLibraryService struct {
	interfaces.LibraryService
	embedErr     error
	gotParams    *types.SearchParams
	searchResult []*types.SearchResult
}

func (s *degradingSearchLibraryService) GetLibrariesByIDsOnly(
	context.Context, []string,
) ([]*types.Library, error) {
	return []*types.Library{{
		ID:               "library-1",
		Type:             types.LibraryTypeDocument,
		EmbeddingModelID: "embedding-1",
		IndexingStrategy: types.DefaultIndexingStrategy(),
	}}, nil
}

func (s *degradingSearchLibraryService) ResolveEmbeddingModelKeys(
	context.Context, []*types.Library,
) map[string]string {
	return map[string]string{"library-1": "doubao|https://ark.cn-beijing.volces.com/api/v3"}
}

func (s *degradingSearchLibraryService) GetQueryEmbedding(
	context.Context, string, string,
) ([]float32, error) {
	return nil, s.embedErr
}

func (s *degradingSearchLibraryService) HybridSearch(
	_ context.Context, _ string, params types.SearchParams,
) ([]*types.SearchResult, error) {
	s.gotParams = &params
	return s.searchResult, nil
}

// embedding API 限流/长尾时检索必须降级为纯关键词模式返回结果，
// 而不是让整次 search 以 500 失败（评测中三次复现的故障）。
func TestSearchEmbeddingFailureDegradesToKeywordSearch(t *testing.T) {
	svc := &degradingSearchLibraryService{
		embedErr: errors.New("embedding rate limited"),
		searchResult: []*types.SearchResult{
			{ID: "chunk-1", Content: "关键词命中的内容", EntryID: "k-1"},
		},
	}
	plugin := &SearchStage{libraryService: svc}
	state := &State{
		Request: Request{
			SearchTargets: types.SearchTargets{{
				Type:      types.SearchTargetTypeLibrary,
				LibraryID: "library-1",
			}},
			EmbeddingTopK: 10,
		},
		Results: Results{RewriteQuery: "树状筛选器 新建入口"},
	}

	err := plugin.Run(context.Background(), state, func() *StageError {
		return nil
	})
	if err != nil {
		t.Fatalf("expected degraded keyword search to succeed, got %#v", err)
	}
	if len(state.SearchResult) != 1 || state.SearchResult[0].ID != "chunk-1" {
		t.Fatalf("expected keyword results to be returned, got %#v", state.SearchResult)
	}
	if svc.gotParams == nil || !svc.gotParams.DisableVectorMatch {
		t.Fatalf("expected DisableVectorMatch=true after embedding failure, got %#v", svc.gotParams)
	}
	if len(svc.gotParams.QueryEmbedding) != 0 {
		t.Fatalf("expected empty query embedding in degraded mode")
	}
}

type vectorOnlySearchLibraryService struct {
	interfaces.LibraryService
	embedErr     error
	hybridCalled bool
}

func (s *vectorOnlySearchLibraryService) GetLibrariesByIDsOnly(
	context.Context, []string,
) ([]*types.Library, error) {
	return []*types.Library{{
		ID:               "document-1",
		Type:             types.LibraryTypeDocument,
		EmbeddingModelID: "embedding-1",
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true},
	}}, nil
}

func (s *vectorOnlySearchLibraryService) ResolveEmbeddingModelKeys(
	context.Context, []*types.Library,
) map[string]string {
	return map[string]string{"document-1": "doubao|https://ark.cn-beijing.volces.com/api/v3"}
}

func (s *vectorOnlySearchLibraryService) GetQueryEmbedding(
	context.Context, string, string,
) ([]float32, error) {
	return nil, s.embedErr
}

func (s *vectorOnlySearchLibraryService) HybridSearch(
	context.Context, string, types.SearchParams,
) ([]*types.SearchResult, error) {
	s.hybridCalled = true
	return nil, nil
}

func TestSearchEmbeddingFailureOnVectorOnlyTargetPreservesRootCause(t *testing.T) {
	rootCause := errors.New("embedding endpoint unavailable")
	svc := &vectorOnlySearchLibraryService{embedErr: rootCause}
	plugin := &SearchStage{libraryService: svc}
	state := &State{
		Request: Request{
			SearchTargets: types.SearchTargets{{
				Type:      types.SearchTargetTypeLibrary,
				LibraryID: "document-1",
			}},
			EmbeddingTopK: 10,
		},
		Results: Results{RewriteQuery: "退款规则"},
	}

	err := plugin.Run(context.Background(), state, func() *StageError {
		return nil
	})
	if err == nil || err.ErrorType != ErrSearch.ErrorType {
		t.Fatalf("expected search_failed, got %#v", err)
	}
	if !errors.Is(err.Err, rootCause) {
		t.Fatalf("expected root cause to be preserved, got %v", err.Err)
	}
	if svc.hybridCalled {
		t.Fatal("vector-only target must not run a disabled-vector search")
	}
}

type partialSearchLibraryService struct {
	interfaces.LibraryService
	rootCause error
}

func (s *partialSearchLibraryService) GetLibrariesByIDsOnly(
	_ context.Context, ids []string,
) ([]*types.Library, error) {
	result := make([]*types.Library, 0, len(ids))
	for _, id := range ids {
		result = append(result, &types.Library{
			ID:               id,
			Type:             types.LibraryTypeDocument,
			EmbeddingModelID: "embedding-" + id,
			IndexingStrategy: types.DefaultIndexingStrategy(),
		})
	}
	return result, nil
}

func (s *partialSearchLibraryService) ResolveEmbeddingModelKeys(
	_ context.Context, libraries []*types.Library,
) map[string]string {
	result := make(map[string]string, len(libraries))
	for _, library := range libraries {
		result[library.ID] = "model-" + library.ID
	}
	return result
}

func (s *partialSearchLibraryService) GetQueryEmbedding(
	context.Context, string, string,
) ([]float32, error) {
	return []float32{1}, nil
}

func (s *partialSearchLibraryService) HybridSearch(
	_ context.Context, id string, _ types.SearchParams,
) ([]*types.SearchResult, error) {
	if id == "library-bad" {
		return nil, s.rootCause
	}
	return []*types.SearchResult{{ID: "chunk-good", Content: "可用结果", EntryID: "entry-good"}}, nil
}

type unindexedSearchLibraryService struct {
	interfaces.LibraryService
	embedErr     error
	hybridCalled bool
}

func (s *unindexedSearchLibraryService) GetLibrariesByIDsOnly(
	context.Context, []string,
) ([]*types.Library, error) {
	return []*types.Library{{
		ID:               "unindexed-1",
		Type:             types.LibraryTypeDocument,
		EmbeddingModelID: "embedding-1",
		IndexingStrategy: types.IndexingStrategy{},
	}}, nil
}

func (s *unindexedSearchLibraryService) ResolveEmbeddingModelKeys(
	context.Context, []*types.Library,
) map[string]string {
	return map[string]string{"unindexed-1": "doubao|https://ark.cn-beijing.volces.com/api/v3"}
}

func (s *unindexedSearchLibraryService) GetQueryEmbedding(
	context.Context, string, string,
) ([]float32, error) {
	return nil, s.embedErr
}

func (s *unindexedSearchLibraryService) HybridSearch(
	context.Context, string, types.SearchParams,
) ([]*types.SearchResult, error) {
	s.hybridCalled = true
	return nil, nil
}

func TestSearchEmbeddingFailureOnUnindexedTargetReportsNoResults(t *testing.T) {
	svc := &unindexedSearchLibraryService{
		embedErr: errors.New("embedding endpoint unavailable"),
	}
	plugin := &SearchStage{libraryService: svc}
	state := &State{
		Request: Request{
			SearchTargets: types.SearchTargets{{
				Type:      types.SearchTargetTypeLibrary,
				LibraryID: "unindexed-1",
			}},
			EmbeddingTopK: 10,
		},
		Results: Results{RewriteQuery: "unindexed"},
	}

	err := plugin.Run(context.Background(), state, func() *StageError {
		return nil
	})
	if err != ErrSearchNothing {
		t.Fatalf("expected search_nothing for unindexed Library, got %#v", err)
	}
	if !svc.hybridCalled {
		t.Fatal("unindexed target should still delegate empty recall to HybridSearch")
	}
}

func TestSearchKeepsSuccessfulResultsWhenAnotherTargetFails(t *testing.T) {
	rootCause := errors.New("one store unavailable")
	plugin := &SearchStage{libraryService: &partialSearchLibraryService{rootCause: rootCause}}
	state := &State{
		Request: Request{
			SearchTargets: types.SearchTargets{
				{Type: types.SearchTargetTypeLibrary, LibraryID: "library-good"},
				{Type: types.SearchTargetTypeLibrary, LibraryID: "library-bad"},
			},
			EmbeddingTopK: 10,
		},
		Results: Results{RewriteQuery: "部分成功"},
	}
	nextCalled := false

	err := plugin.Run(context.Background(), state, func() *StageError {
		nextCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected partial success to continue, got %#v", err)
	}
	if !nextCalled {
		t.Fatal("expected pipeline to continue with successful results")
	}
	if len(state.SearchResult) != 1 || state.SearchResult[0].ID != "chunk-good" {
		t.Fatalf("expected successful target result, got %#v", state.SearchResult)
	}
}
