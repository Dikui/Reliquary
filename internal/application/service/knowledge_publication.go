package service

import (
	"sort"

	"reliquary/internal/types"
)

// Missing publication dates exclude sources from publication windows and heat.
// A source is counted once even if several evidence versions refer to it.
func contextMissingPublicationSources(evidence map[string]types.KnowledgeEvidence) []string {
	missing := map[string]bool{}
	for _, item := range evidence {
		if parseContextTime(item.PublishedAt) == nil {
			id := "evidence:" + item.ID
			if item.KnowledgeID != "" {
				id = "knowledge:" + item.KnowledgeID
			}
			missing[id] = true
		}
		for _, repost := range item.RepostSources {
			if parseContextTime(repost.PublishedAt) == nil {
				missing["repost:"+repost.ID] = true
			}
		}
	}
	ids := make([]string, 0, len(missing))
	for id := range missing {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
