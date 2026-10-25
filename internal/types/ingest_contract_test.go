package types

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIngestContractFieldSetAndExample(t *testing.T) {
	fields := []string{}
	typ := reflect.TypeOf(IngestRequest{Library: "official"})
	for i := 0; i < typ.NumField(); i++ {
		field := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if field != "-" {
			fields = append(fields, field)
		}
	}
	require.ElementsMatch(
		t,
		[]string{
			"recovery_failure_id",
			"mentioned_fact_ids",
			"library",
			"source_id",
			"source_revision",
			"source_ref",
			"identifier",
			"url",
			"content",
			"acquisition",

			"title",
			"author",
			"publisher",
			"published_at",
			"images",
			"thread",
			"quotes",
			"collect_reason",
			"agent_summary",
			"agent_tags",
			"source_name",
		},
		fields,
	)
	result := IngestResult{
		EntryID:       "article-example",
		Status:        "pending_review",
		Message:       "等待收录审核。",
		Reason:        "缺少来源",
		ImageFailures: []IngestImageFailure{},
	}
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(
		t,
		`{"source_unverified":false,
"library_id":"",
"library_name":"",
"has_update":false,
"index":0,
"entry_id":"article-example",
"status":"pending_review",
"message":"等待收录审核。",
"reason":"缺少来源",
"image_failures":[]}`,
		string(data),
	)
}
