package config

import "os"

// Neo4jConfig describes the disposable query replica, never the authority.
type Neo4jConfig struct {
	URI      string `yaml:"uri" json:"uri"`
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"-"`
}

func ResolveNeo4j(cfg *Config) Neo4jConfig {
	c := Neo4jConfig{URI: "bolt://127.0.0.1:7687", Username: "neo4j"}
	if cfg != nil && cfg.Neo4j != nil {
		c = *cfg.Neo4j
	}
	for name, target := range map[string]*string{"NEO4J_URI": &c.URI, "NEO4J_USERNAME": &c.Username, "NEO4J_PASSWORD": &c.Password} {
		if value, ok := os.LookupEnv(name); ok {
			*target = value
		}
	}
	return c
}
