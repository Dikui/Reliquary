package service

import (
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

func TestQualityScoringApplies(t *testing.T) {
	timely := &types.Library{Template: types.TemplateExternalTimely}
	durable := &types.Library{Template: types.TemplateExternalDurable}
	own := &types.Library{Template: types.TemplateOwn}
	article := &types.Entry{SourceKind: "article"}
	report := &types.Entry{SourceKind: "report"}

	if !qualityScoringApplies(timely, article) || !qualityScoringApplies(durable, article) {
		t.Fatal("external libraries should be scored")
	}
	if qualityScoringApplies(own, article) {
		t.Fatal("own library should not be scored")
	}
	if qualityScoringApplies(timely, report) {
		t.Fatal("reports should not be scored")
	}
	if qualityScoringApplies(nil, article) || qualityScoringApplies(timely, nil) {
		t.Fatal("nil inputs should not be scored")
	}
}

func TestParseQualityScoringResult(t *testing.T) {
	if score, err := parseQualityScoringResult(`{"attentionScore": 72}`); err != nil || score != 72 {
		t.Fatalf("score=%d err=%v", score, err)
	}
	if _, err := parseQualityScoringResult(`{"attentionScore": 120}`); err == nil {
		t.Fatal("out-of-range score should fail")
	}
	if _, err := parseQualityScoringResult(`{"score": 50}`); err == nil {
		t.Fatal("missing attentionScore should fail")
	}
}

func TestValidateBatchRequestAllowsQualityScoring(t *testing.T) {
	err := validateBatchRequest(types.EntryBatchRequest{
		Action:    "score_quality",
		Selection: types.EntryBatchSelection{Filters: &types.EntryBatchFilters{}},
	})
	if err != nil {
		t.Fatalf("score_quality rejected: %v", err)
	}
}
