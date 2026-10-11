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
		fields = append(fields, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
	}
	require.ElementsMatch(t, []string{"url", "content", "title", "author", "publisher", "published_at", "images", "thread", "quotes", "collect_reason", "agent_summary", "agent_tags", "source_name"}, fields)
	result := IngestResult{EntryID: "article-example", Status: "pending_review", Message: "Awaiting review", Reason: "Missing source", ImageFailures: []IngestImageFailure{}}
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{"article_id":"article-example","status":"pending_review","message":"Awaiting review","reason":"Missing source","image_failures":[]}`, string(data))
}
