package utils

import "testing"

func TestParseTaskID_CompoundTaskType(t *testing.T) {
	taskID := GenerateTaskID("document_process", 42, "kb-abc-123")
	taskType, spaceID, _, uuidPart, businessID, err := ParseTaskID(taskID)
	if err != nil {
		t.Fatalf("ParseTaskID failed: %v", err)
	}
	if taskType != "document_process" {
		t.Fatalf("taskType = %q, want document_process", taskType)
	}
	if spaceID != 42 {
		t.Fatalf("spaceID = %d, want 42", spaceID)
	}
	if uuidPart == "" {
		t.Fatal("expected uuid part")
	}
	if businessID == "" {
		t.Fatal("expected business ID")
	}
}

func TestTaskSpaceID(t *testing.T) {
	taskID := GenerateTaskID("entry_move", 7, "source-kb")
	got, err := TaskSpaceID(taskID)
	if err != nil {
		t.Fatalf("TaskSpaceID failed: %v", err)
	}
	if got != 7 {
		t.Fatalf("TaskSpaceID = %d, want 7", got)
	}
}
