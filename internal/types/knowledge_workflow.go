package types

import "time"

const (
	WorkflowStepSearch      = "search"
	WorkflowStepFactQuery   = "fact_query"
	WorkflowStepWebResearch = "web_research"
	WorkflowStepReport      = "report"
	WorkflowStepWikiUpdate  = "wiki_update"
	WorkflowStepSkill       = "skill"
	WorkflowStepCondition   = "condition"
	WorkflowStepHumanReview = "human_review"

	WorkflowStatusQueued           = "queued"
	WorkflowStatusRunning          = "running"
	WorkflowStatusPaused           = "paused"
	WorkflowStatusWaitingIngestion = "waiting_ingestion"
	WorkflowStatusCompleted        = "completed"
	WorkflowStatusFailed           = "failed"
	WorkflowStatusRejected         = "rejected"
	WorkflowStatusCancelled        = "cancelled"

	WorkflowStepPending          = "pending"
	WorkflowStepRunning          = "running"
	WorkflowStepWaitingReview    = "waiting_review"
	WorkflowStepWaitingIngestion = "waiting_ingestion"
	WorkflowStepSucceeded        = "succeeded"
	WorkflowStepFailed           = "failed"
	WorkflowStepSkipped          = "skipped"
	WorkflowStepRejected         = "rejected"
)

// KnowledgeWorkflowSchedule is evaluated in the configured IANA time zone.
// An empty cron expression means that only manual runs are allowed.
type KnowledgeWorkflowSchedule struct {
	Cron     string `json:"cron,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

// KnowledgeWorkflowStep describes one persisted workflow operation.
// Config remains an object so step-specific fields can evolve without a second
// workflow schema or migrations.
type KnowledgeWorkflowStep struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	DependsOn []string       `json:"depends_on,omitempty"`
	When      *WorkflowWhen  `json:"when,omitempty"`
	Config    map[string]any `json:"config"`
}

// WorkflowWhen gates a step on the boolean result of a condition step.
type WorkflowWhen struct {
	StepID string `json:"step_id"`
	Equals bool   `json:"equals"`
}

// KnowledgeWorkflowInput is used for create/update. Schedule and steps are
// stored in the workflow row itself so a run always has one authoritative spec.
type KnowledgeWorkflowInput struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description,omitempty"`
	Enabled     bool                      `json:"enabled"`
	Schedule    KnowledgeWorkflowSchedule `json:"schedule"`
	Steps       []KnowledgeWorkflowStep   `json:"steps"`
}

type KnowledgeWorkflow struct {
	ID              string                    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64                    `json:"tenant_id" gorm:"not null;uniqueIndex:idx_kworkflow_name"`
	KnowledgeBaseID string                    `json:"knowledge_base_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kworkflow_name"`
	OwnerID         string                    `json:"owner_id" gorm:"type:varchar(64);not null;index"`
	Name            string                    `json:"name" gorm:"type:varchar(255);not null;uniqueIndex:idx_kworkflow_name"`
	Description     string                    `json:"description,omitempty" gorm:"type:text;not null;default:''"`
	Enabled         bool                      `json:"enabled" gorm:"not null;default:false;index"`
	Schedule        KnowledgeWorkflowSchedule `json:"schedule" gorm:"serializer:json;type:jsonb;not null"`
	Steps           []KnowledgeWorkflowStep   `json:"steps" gorm:"serializer:json;type:jsonb;not null"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

func (KnowledgeWorkflow) TableName() string { return "knowledge_workflows" }

type KnowledgeWorkflowRun struct {
	ID               string                     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID         uint64                     `json:"tenant_id" gorm:"not null;index:idx_kwrun_scope"`
	KnowledgeBaseID  string                     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_kwrun_scope"`
	WorkflowID       string                     `json:"workflow_id" gorm:"type:varchar(36);not null;index:idx_kwrun_scope"`
	WorkflowSnapshot []KnowledgeWorkflowStep    `json:"workflow_snapshot" gorm:"serializer:json;type:jsonb;not null"`
	OwnerID          string                     `json:"owner_id" gorm:"type:varchar(64);not null;default:''"`
	Trigger          string                     `json:"trigger" gorm:"type:varchar(32);not null"`
	IdempotencyKey   string                     `json:"idempotency_key" gorm:"type:varchar(160);not null;uniqueIndex:idx_kwrun_idempotency"`
	Status           string                     `json:"status" gorm:"type:varchar(24);not null;index"`
	CurrentStepID    string                     `json:"current_step_id,omitempty" gorm:"type:varchar(128);not null;default:''"`
	Error            string                     `json:"error,omitempty" gorm:"type:text;not null;default:''"`
	StartedAt        *time.Time                 `json:"started_at,omitempty"`
	FinishedAt       *time.Time                 `json:"finished_at,omitempty"`
	NextRunAt        *time.Time                 `json:"next_run_at,omitempty"`
	LeaseToken       string                     `json:"-" gorm:"type:varchar(36);not null;default:''"`
	CreatedAt        time.Time                  `json:"created_at"`
	UpdatedAt        time.Time                  `json:"updated_at"`
	Steps            []KnowledgeWorkflowStepRun `json:"steps,omitempty" gorm:"-"`
}

func (KnowledgeWorkflowRun) TableName() string { return "knowledge_workflow_runs" }

type KnowledgeWorkflowStepRun struct {
	ID              string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64     `json:"tenant_id" gorm:"not null;index:idx_kwstep_scope"`
	KnowledgeBaseID string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_kwstep_scope"`
	RunID           string     `json:"run_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kwstep_once"`
	StepID          string     `json:"step_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_kwstep_once"`
	StepType        string     `json:"step_type" gorm:"type:varchar(32);not null"`
	Status          string     `json:"status" gorm:"type:varchar(24);not null;index"`
	Attempt         int        `json:"attempt" gorm:"not null;default:0"`
	Input           JSON       `json:"input,omitempty" gorm:"type:jsonb"`
	Output          JSON       `json:"output,omitempty" gorm:"type:jsonb"`
	Error           string     `json:"error,omitempty" gorm:"type:text;not null;default:''"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (KnowledgeWorkflowStepRun) TableName() string { return "knowledge_workflow_step_runs" }

// KnowledgeWorkflowCitation pins a report citation to source content as it
// existed when the run completed, rather than relying on a mutable URL alone.
type KnowledgeWorkflowCitation struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	KnowledgeID string `json:"knowledge_id,omitempty"`
	EvidenceID  string `json:"evidence_id,omitempty"`
	Title       string `json:"title,omitempty"`
	URL         string `json:"url,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Quote       string `json:"quote,omitempty"`
	Role        string `json:"role,omitempty"`
}

type KnowledgeWorkflowReport struct {
	ExcludedMissingPublishedAtCount int                         `json:"excluded_missing_published_at_count" gorm:"not null;default:0"`
	ID                              string                      `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID                        uint64                      `json:"tenant_id" gorm:"not null;index:idx_kwreport_scope"`
	KnowledgeBaseID                 string                      `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_kwreport_scope"`
	WorkflowID                      string                      `json:"workflow_id" gorm:"type:varchar(36);not null;index"`
	RunID                           string                      `json:"run_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kwreport_step"`
	StepID                          string                      `json:"step_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_kwreport_step"`
	Title                           string                      `json:"title" gorm:"type:varchar(512);not null"`
	Markdown                        string                      `json:"markdown" gorm:"type:text;not null"`
	Citations                       []KnowledgeWorkflowCitation `json:"citations" gorm:"serializer:json;type:jsonb;not null"`
	TimeFrom                        *time.Time                  `json:"time_from,omitempty"`
	TimeTo                          *time.Time                  `json:"time_to,omitempty"`
	CreatedAt                       time.Time                   `json:"created_at"`
}

func (KnowledgeWorkflowReport) TableName() string { return "knowledge_workflow_reports" }

type KnowledgeWorkflowTaskPayload struct {
	RunID       string        `json:"run_id"`
	Initiator   TaskInitiator `json:"initiator"`
	SystemAdmin bool          `json:"system_admin,omitempty"`
}

type KnowledgeWorkflowDecision struct {
	Decision string `json:"decision"` // approve or reject
}
