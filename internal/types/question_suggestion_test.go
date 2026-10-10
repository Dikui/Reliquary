package types

import "testing"

func TestQuestionSuggestionConfigValidate(t *testing.T) {
	config := &QuestionSuggestionConfig{
		Starters: StarterSuggestionConfig{Mode: SuggestionModeHybrid, Count: 6},
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	config.Starters.Count = 9
	if err := config.Validate(); err == nil {
		t.Fatal("out-of-range starter count was accepted")
	}
}
