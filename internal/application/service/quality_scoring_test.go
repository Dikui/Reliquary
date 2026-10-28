package service

import (
	"context"
	"testing"

	"reliquary/internal/config"
	"reliquary/internal/types"
)

func TestQualityScoringPassThreshold(t *testing.T) {
	s := &entryService{config: &config.Config{QualityScoring: &config.QualityScoringConfig{Threshold: 60}}}
	cases := []struct {
		first, second int
		want          bool
	}{
		{60, 60, true},
		{70, 50, true},
		{59, 61, true},
		{59, 60, false},
		{0, 0, false},
	}
	for _, tc := range cases {
		if got := s.qualityPassed(tc.first, tc.second); got != tc.want {
			t.Fatalf("qualityPassed(%d,%d)=%v want %v", tc.first, tc.second, got, tc.want)
		}
	}
}

func TestQualityScoringBodyLimit(t *testing.T) {
	body := "一二三四五"
	if got := qualityScoringBody(body, 3); got != "一二三" {
		t.Fatalf("truncated body=%q", got)
	}
	if got := qualityScoringBody(body, 0); got != body {
		t.Fatalf("zero limit body=%q", got)
	}
	if got := qualityScoringBody(body, 10); got != body {
		t.Fatalf("large limit body=%q", got)
	}
}

func TestQualityGateScoresOnlyOnce(t *testing.T) {
	s := &entryService{}
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		status string
		want   bool
	}{
		{"stored pass is reused", ctx, types.QualityStatusPassed, true},
		{"stored low is not rescored", ctx, types.QualityStatusLow, false},
		{"rebuild never scores", context.WithValue(ctx, libraryRebuildContextKey{}, true), "", true},
		{"no switch means no scoring", ctx, "", true},
	} {
		kept, err := s.applyQualityGate(tc.ctx, &types.Library{ID: "library"}, &types.Entry{QualityStatus: tc.status}, "body")
		if err != nil || kept != tc.want {
			t.Fatalf("%s: kept=%v err=%v", tc.name, kept, err)
		}
	}
}

func TestParseQualityScoringResult(t *testing.T) {
	if score, err := parseQualityScoringResult(`{"attentionScore": 72}`); err != nil || score != 72 {
		t.Fatalf("score=%d err=%v", score, err)
	}
	if score, err := parseQualityScoringResult(`{"attentionScore": 72.6}`); err != nil || score != 73 {
		t.Fatalf("fractional score=%d err=%v", score, err)
	}
	if _, err := parseQualityScoringResult(`{"attentionScore": 120}`); err == nil {
		t.Fatal("out-of-range score should fail")
	}
	if _, err := parseQualityScoringResult(`{"score": 50}`); err == nil {
		t.Fatal("missing attentionScore should fail")
	}
}

func TestValidateBatchRequestRejectsRetiredQualityScoring(t *testing.T) {
	err := validateBatchRequest(types.EntryBatchRequest{
		Action:    "score_quality",
		Selection: types.EntryBatchSelection{Filters: &types.EntryBatchFilters{}},
	})
	if err == nil {
		t.Fatal("score_quality must no longer be accepted")
	}
}
