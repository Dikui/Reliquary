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
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("收录请求格式无效：%w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("收录请求必须只有一个 JSON 对象")
	}
	if len(envelope.Items) < 1 || len(envelope.Items) > MaxIngestBatchItems {
		return nil, fmt.Errorf("每次收录必须包含 1 到 50 条")
	}
	request := &IngestBatchRequest{Items: make([]IngestRequest, len(envelope.Items))}
	for index, raw := range envelope.Items {
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
		url, present := fields["url"]
		if !present && request.Items[index].Kind != "mine" {
			request.Items[index].InputError = "url 必填；缺少原文链接时请明确提供空字符串"
		} else if bytes.Equal(bytes.TrimSpace(url), []byte("null")) {
			request.Items[index].InputError = "url 必须是字符串；缺少原文链接时请提供空字符串"
		}
	}
	return request, nil
}
