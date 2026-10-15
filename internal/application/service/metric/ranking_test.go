package metric

import (
	"github.com/stretchr/testify/require"
	"math"
	"reliquary/internal/types"
	"testing"
)

func TestFiveRankingMetricsHandCalculated(t *testing.T) {
	binary := []types.EvaluationAnswer{{EntryID: "a", Grade: 1}, {EntryID: "b", Grade: 1}, {EntryID: "c", Grade: 1}}
	discount := 1 / math.Log2(3)
	cases := []struct {
		name     string
		ids      []string
		answers  []types.EvaluationAnswer
		k        int
		expected types.EvaluationMetrics
	}{
		// Hits at ranks 2 and 3: AP=(1/2+2/3)/3=7/18.
		{"missed recall", []string{"x", "a", "b"}, binary, 4, types.EvaluationMetrics{Precision: 0.5, Recall: 2.0 / 3, MRR: 0.5, NDCG: (discount + 0.5) / (1 + discount + 0.5), MAP: 7.0 / 18}},
		// Returning one item cannot shorten IDCG or the AP denominator.
		{"short result", []string{"a"}, binary, 2, types.EvaluationMetrics{Precision: 0.5, Recall: 1.0 / 3, MRR: 1, NDCG: 1 / (1 + discount), MAP: 1.0 / 3}},
		{"all hit", []string{"a", "b", "c"}, binary, 3, types.EvaluationMetrics{Precision: 1, Recall: 1, MRR: 1, NDCG: 1, MAP: 1}},
		{"no answers", []string{"a"}, nil, 2, types.EvaluationMetrics{}},
		{"no hits", []string{"x"}, binary, 2, types.EvaluationMetrics{}},
		{"empty result", nil, binary, 2, types.EvaluationMetrics{}},
		{"zero k", []string{"a"}, binary, 0, types.EvaluationMetrics{}},
		{"duplicate IDs", []string{"a", "a", "b", "b", "c"}, append(binary, binary[0]), 3, types.EvaluationMetrics{Precision: 1, Recall: 1, MRR: 1, NDCG: 1, MAP: 1}},
		// Grade 3 earns gain 7, grade 1 earns gain 1; swapped order lowers NDCG.
		{"graded relevance", []string{"b", "a"}, []types.EvaluationAnswer{{EntryID: "a", Grade: 3}, {EntryID: "b", Grade: 1}, {EntryID: "x", Grade: 0}}, 2, types.EvaluationMetrics{Precision: 1, Recall: 1, MRR: 1, NDCG: (1 + 7*discount) / (7 + discount), MAP: 1}},
		{"beyond k", []string{"x", "a", "b", "c"}, binary, 1, types.EvaluationMetrics{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := Compute(c.ids, c.answers, c.k)
			require.InDelta(t, c.expected.Precision, got.Precision, 1e-12, "Precision@K")
			require.InDelta(t, c.expected.Recall, got.Recall, 1e-12, "Recall@K")
			require.InDelta(t, c.expected.MRR, got.MRR, 1e-12, "MRR")
			require.InDelta(t, c.expected.NDCG, got.NDCG, 1e-12, "NDCG@K")
			require.InDelta(t, c.expected.MAP, got.MAP, 1e-12, "MAP")
		})
	}
}
