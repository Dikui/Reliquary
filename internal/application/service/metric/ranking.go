// Package metric adapts WeKnora's five retrieval ranking metrics to one question
// with unique document IDs. No generation or text similarity metrics are used.
package metric

import (
	"math"
	"reliquary/internal/types"
	"sort"
)

// Compute uses only the returned representatives. Folded members never earn
// credit. Missing ranks count against precision; AP divides by all positive
// answers, including answers beyond K. Grades affect NDCG only.
func Compute(ids []string, answers []types.EvaluationAnswer, k int) (types.EvaluationMetrics, []string) {
	m := types.EvaluationMetrics{}
	hits := []string{}
	if k <= 0 {
		return m, hits
	}
	gt := map[string]int{}
	for _, a := range answers {
		if a.Grade > gt[a.EntryID] {
			gt[a.EntryID] = a.Grade
		}
	}
	if len(gt) == 0 {
		return m, hits
	}
	ranked := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			ranked = append(ranked, id)
			seen[id] = true
		}
	}
	if len(ranked) > k {
		ranked = ranked[:k]
	}
	dcg := 0.0
	ap := 0.0
	for i, id := range ranked {
		if grade := gt[id]; grade > 0 {
			hits = append(hits, id)
			if m.MRR == 0 {
				m.MRR = 1 / float64(i+1)
			}
			ap += float64(len(hits)) / float64(i+1)
			dcg += (math.Pow(2, float64(grade)) - 1) / math.Log2(float64(i+2))
		}
	}
	grades := []int{}
	for _, g := range gt {
		grades = append(grades, g)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(grades)))
	idcg := 0.0
	for i, g := range grades[:min(k, len(grades))] {
		idcg += (math.Pow(2, float64(g)) - 1) / math.Log2(float64(i+2))
	}
	m.Precision = float64(len(hits)) / float64(k)
	m.Recall = float64(len(hits)) / float64(len(gt))
	m.MAP = ap / float64(len(gt))
	if idcg > 0 {
		m.NDCG = dcg / idcg
	}
	return m, hits
}
