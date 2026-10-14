package service

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Only versioned replica restoration may convert frozen pre-23 header names.
// The public parser accepts current names. Body bytes are always preserved.
func parseVaultReplicaHeader(data []byte, legacy bool) (VaultArticleHeader, []byte, error) {
	if !legacy {
		return ParseVaultArticle(data)
	}
	header, body, err := SplitVaultArticle(data)
	if err != nil {
		return VaultArticleHeader{}, nil, err
	}
	first := bytes.IndexByte(header, '\n') + 1
	last := bytes.LastIndex(header, []byte("\n---"))
	var doc yaml.Node
	if err := yaml.Unmarshal(header[first:last], &doc); err != nil {
		return VaultArticleHeader{}, nil, err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return VaultArticleHeader{}, nil, fmt.Errorf("invalid legacy header")
	}
	fields := doc.Content[0].Content
	pairs := map[string]string{"latest_article_id": "latest_entry_id", "related_article_ids": "related_entry_ids"}
	seen := map[string]bool{}
	for i := 0; i < len(fields); i += 2 {
		name := fields[i].Value
		if current, ok := pairs[name]; ok {
			name = current
			fields[i].Value = current
		}
		if seen[name] {
			return VaultArticleHeader{}, nil, fmt.Errorf("duplicate replica header field %s", name)
		}
		seen[name] = true
	}
	encoded, err := yaml.Marshal(&doc)
	if err != nil {
		return VaultArticleHeader{}, nil, err
	}
	converted := append([]byte("---\n"), encoded...)
	converted = append(converted, []byte("---\n")...)
	converted = append(converted, body...)
	return ParseVaultArticle(converted)
}
