package types

// EntityFactQuery selects an entity's timeline or one-hop graph by occurrence time.
type EntityFactQuery struct {
	From          string `form:"from"`
	To            string `form:"to"`
	OtherEntityID string `form:"other_entity_id" binding:"max=36"`
	Direction     string `form:"direction" binding:"omitempty,oneof=incoming outgoing"`
	Limit         int    `form:"limit" binding:"min=0,max=100"`
	Offset        int    `form:"offset" binding:"min=0"`
}

// EntityGraphEdge aggregates visible facts with the same ordered entity endpoints.
type EntityGraphEdge struct {
	SubjectEntityID string `json:"subject_entity_id"`
	ObjectEntityID  string `json:"object_entity_id"`
	FactCount       int64  `json:"fact_count"`
	LatestAction    string `json:"latest_action"`
}

// EntityGraph contains only the center and its direct visible fact endpoints.
type EntityGraph struct {
	Center Entity            `json:"center"`
	Nodes  []Entity          `json:"nodes"`
	Edges  []EntityGraphEdge `json:"edges"`
}

// EntityFactPage contains a bounded chronological timeline and its visible count.
type EntityFactPage struct {
	Items []StoryFact `json:"items"`
	Total int64       `json:"total"`
}
