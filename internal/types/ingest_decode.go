package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// DecodeIngestBatch rejects malformed envelopes while retaining an invalid item
// in its original position so the shared service can return a per-item result.
func DecodeIngestBatch(reader io.Reader) (*IngestBatchRequest, error) {
	var envelope struct {
		Items []json.RawMessage `json:"items"`
	}
	decoder := json.NewDecoder(NewIngestJSONReader(reader, MaxIngestBatchBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("收录请求格式无效：%w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("收录请求尾部无效：%w", err)
		}
		return nil, fmt.Errorf("收录请求必须只有一个 JSON 对象")
	}
	if len(envelope.Items) < 1 || len(envelope.Items) > MaxIngestBatchItems {
		return nil, fmt.Errorf("每次收录必须包含 1 到 50 条")
	}
	request := &IngestBatchRequest{Items: make([]IngestRequest, len(envelope.Items))}
	for index, raw := range envelope.Items {
		if len(raw) > MaxIngestItemBytes {
			request.Items[index].InputError = "item JSON exceeds 64 MiB"
			continue
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			request.Items[index].InputError = "收录条目必须是 JSON 对象"
			continue
		}
		itemDecoder := json.NewDecoder(bytes.NewReader(raw))
		itemDecoder.DisallowUnknownFields()
		if err := itemDecoder.Decode(&request.Items[index]); err != nil {
			request.Items[index] = IngestRequest{InputError: "收录只接受 Markdown 正文和图片，条目字段无效：" + err.Error()}
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			request.Items[index].InputError = "收录只接受 Markdown 正文和图片，条目字段无效：" + err.Error()
			continue
		}
		_, request.Items[index].SourceRefProvided = fields["source_ref"]
		_, request.Items[index].IdentifierProvided = fields["identifier"]
		request.Items[index].RawInput = raw
		if bytes.Equal(bytes.TrimSpace(fields["source_id"]), []byte("null")) {
			request.Items[index].InputError = "source_id must be a string"
		}
		if bytes.Equal(bytes.TrimSpace(fields["source_revision"]), []byte("null")) {
			request.Items[index].InputError = "source_revision must be a nonnegative integer"
		}
		if bytes.Equal(bytes.TrimSpace(fields["source_ref"]), []byte("null")) || bytes.Equal(bytes.TrimSpace(fields["identifier"]), []byte("null")) {
			request.Items[index].InputError = "source_ref and identifier must be strings"
		}

	}
	return request, nil
}
