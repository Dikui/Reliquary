package types

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"reflect"
	"strings"
	"testing"
)

func TestIngestContractFieldSetAndExample(t *testing.T) {
	fields := []string{}
	typ := reflect.TypeOf(IngestRequest{})
	for i := 0; i < typ.NumField(); i++ {
		field := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if field != "-" {
			fields = append(fields, field)
		}
	}
	require.ElementsMatch(t, []string{"url", "content", "acquisition", "content_type_hint", "file", "missing", "title", "author", "publisher", "published_at", "images", "thread", "quotes", "collect_reason", "agent_summary", "agent_tags", "source_name"}, fields)
	result := IngestResult{EntryID: "article-example", Status: "pending_review", Message: "等待收录审核。", Reason: "缺少来源", ImageFailures: []IngestImageFailure{}, Completeness: []IngestMissing{}}
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{"index":0,"article_id":"article-example","status":"pending_review","message":"等待收录审核。","reason":"缺少来源","image_failures":[],"completeness":[]}`, string(data))
}
