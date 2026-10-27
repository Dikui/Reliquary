package types

// EntityFactQuery selects an entity timeline by occurrence time.
type EntityFactQuery struct {
	From          string `form:"from"`
	To            string `form:"to"`
	OtherEntityID string `form:"other_entity_id" binding:"max=36"`
	Direction     string `form:"direction" binding:"omitempty,oneof=incoming outgoing"`
	Limit         int    `form:"limit" binding:"min=0,max=100"`
	Offset        int    `form:"offset" binding:"min=0"`
}

// EntityFactPage contains a bounded chronological timeline and its visible count.
type EntityFactPage struct {
	Items []StoryFact `json:"items"`
	Total int64       `json:"total"`
}
