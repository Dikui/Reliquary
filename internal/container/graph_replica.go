package container

import (
	"log"
	"os"
	"time"

	"reliquary/internal/config"
	"reliquary/internal/graphreplica"
	"reliquary/internal/types/interfaces"
)

func newGraphReplicaStore(cfg *config.Config, cleaner interfaces.ResourceCleaner) *graphreplica.Store {
	store := graphreplica.NewStore(config.ResolveNeo4j(cfg))
	cleaner.RegisterWithName("Neo4j query replica", store.Close)
	return store
}

func startGraphReplica(s *graphreplica.Service, cleaner interfaces.ResourceCleaner) {
	interval := time.Minute
	if value := os.Getenv("RELIQUARY_GRAPH_SWEEP_INTERVAL"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			panic("RELIQUARY_GRAPH_SWEEP_INTERVAL must be a positive duration")
		}
		interval = parsed
	}
	cleaner.RegisterWithName("Graph replica notification sweep", s.Start(interval))
	log.Printf("Neo4j query replica synchronization registered; sweep interval %s", interval)
}
