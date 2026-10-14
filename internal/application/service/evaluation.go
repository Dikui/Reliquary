package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// GraphEvaluationSearch must invoke the graph service's public search entry
// point, including its RecordSearch adapter, when ikb-14 is integrated.
type GraphEvaluationSearch func(context.Context, types.EntrySearchRequest) (*types.EntrySearchResponse, error)
type EvaluationService struct {
	db          *gorm.DB
	search      interfaces.SearchService
	models      interfaces.ModelService
	GraphSearch GraphEvaluationSearch
}

func NewEvaluationService(db *gorm.DB, search interfaces.SearchService, models interfaces.ModelService) *EvaluationService {
	return &EvaluationService{db: db, search: search, models: models}
}
func evaluationScope(ctx context.Context, db *gorm.DB) (*gorm.DB, error) {
	id, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace required")
	}
	return db.WithContext(ctx).Where("space_id=?", id), nil
}
func evaluationError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.NewNotFoundError("evaluation resource not found")
	}
	return err
}
func (s *EvaluationService) ListSets(ctx context.Context, limit, offset int) ([]types.EvaluationSet, error) {
	q, err := evaluationScope(ctx, s.db)
	if err != nil {
		return nil, err
	}
	rows := []types.EvaluationSet{}
	err = q.Order("created_at DESC,id").Limit(pageLimit(limit)).Offset(max(offset, 0)).Find(&rows).Error
	return rows, err
}
func (s *EvaluationService) GetSet(ctx context.Context, id string) (*types.EvaluationSet, error) {
	q, err := evaluationScope(ctx, s.db)
	if err != nil {
		return nil, err
	}
	var set types.EvaluationSet
	if err = q.Where("id=?", id).First(&set).Error; err != nil {
		return nil, evaluationError(err)
	}
	return &set, nil
}
func (s *EvaluationService) SaveSet(ctx context.Context, id, name, description string) (*types.EvaluationSet, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 500 || len(description) > 10000 {
		return nil, apperrors.NewBadRequestError("name required, maximum 500 bytes; description maximum 10000 bytes")
	}
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace required")
	}
	if id == "" {
		set := &types.EvaluationSet{ID: uuid.NewString(), SpaceID: spaceID, Name: name, Description: description}
		return set, s.db.WithContext(ctx).Create(set).Error
	}
	set, err := s.GetSet(ctx, id)
	if err != nil {
		return nil, err
	}
	set.Name = name
	set.Description = description
	return set, s.db.WithContext(ctx).Save(set).Error
}
func (s *EvaluationService) DeleteSet(ctx context.Context, id string) error {
	set, err := s.GetSet(ctx, id)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(set).Error
}
func (s *EvaluationService) ListQuestions(ctx context.Context, setID, status string, limit, offset int) ([]types.EvaluationQuestion, error) {
	if _, err := s.GetSet(ctx, setID); err != nil {
		return nil, err
	}
	q, err := evaluationScope(ctx, s.db)
	if err != nil {
		return nil, err
	}
	if status != "" {
		q = q.Where("status=?", status)
	}
	rows := []types.EvaluationQuestion{}
	err = q.Where("set_id=?", setID).Preload("Answers", func(db *gorm.DB) *gorm.DB { return db.Order("entry_id") }).Order("created_at,id").Limit(pageLimit(limit)).Offset(max(offset, 0)).Find(&rows).Error
	return rows, err
}
func (s *EvaluationService) GetQuestion(ctx context.Context, setID, id string) (*types.EvaluationQuestion, error) {
	q, err := evaluationScope(ctx, s.db)
	if err != nil {
		return nil, err
	}
	var question types.EvaluationQuestion
	err = q.Where("id=? AND set_id=?", id, setID).Preload("Answers", func(db *gorm.DB) *gorm.DB { return db.Order("entry_id") }).First(&question).Error
	if err != nil {
		return nil, evaluationError(err)
	}
	return &question, nil
}
func validateQuestion(ctx context.Context, db *gorm.DB, question *types.EvaluationQuestion) error {
	if question.Method != "ordinary" && question.Method != "graph" {
		return apperrors.NewBadRequestError("method must be ordinary or graph")
	}
	validationRequest := question.Request
	if question.Method == "graph" {
		validationRequest.GraphParameters = nil
	}
	if _, err := normalizeEntrySearch(validationRequest); err != nil {
		return err
	}
	if question.Status != "pending" && question.Status != "confirmed" {
		return apperrors.NewBadRequestError("status must be pending or confirmed")
	}
	if len(question.Answers) > 1000 {
		return apperrors.NewBadRequestError("at most 1000 answers")
	}
	// Resolve directories to stable IDs for the stored question and answer scope.
	libraries := []types.Library{}
	if err := db.WithContext(ctx).Where("space_id=? AND (id IN ? OR directory IN ?)", question.SpaceID, question.Request.Libraries, question.Request.Libraries).Find(&libraries).Error; err != nil {
		return err
	}
	ids := []string{}
	for _, requested := range question.Request.Libraries {
		found := false
		for _, lib := range libraries {
			if lib.ID == requested || lib.Directory == requested {
				found = true
			}
		}
		if !found {
			return apperrors.NewBadRequestError("unknown library: " + requested)
		}
	}
	for _, lib := range libraries {
		ids = append(ids, lib.ID)
	}
	question.Request.Libraries = ids
	seen := map[string]bool{}
	for i := range question.Answers {
		a := &question.Answers[i]
		if a.EntryID == "" || seen[a.EntryID] || a.Grade < 0 || a.Grade > 10 {
			return apperrors.NewBadRequestError("answers require unique entry_id and grade 0 to 10")
		}
		seen[a.EntryID] = true
		a.QuestionID = question.ID
		var entry types.Entry
		read := db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id=? AND space_id=? AND library_id IN ?", a.EntryID, question.SpaceID, ids).Find(&entry)
		if read.Error != nil {
			return read.Error
		}
		if read.RowsAffected != 1 {
			return apperrors.NewBadRequestError("answer must reference an existing document in the selected libraries")
		}
	}
	return nil
}

// SaveQuestion is a full replacement. Origin and candidate status are server
// controlled; a pending AI question enters a run only through ConfirmCandidate.
func (s *EvaluationService) SaveQuestion(ctx context.Context, setID, id string, input types.EvaluationQuestion) (*types.EvaluationQuestion, error) {
	set, err := s.GetSet(ctx, setID)
	if err != nil {
		return nil, err
	}
	input.ID = id
	input.SetID = setID
	input.SpaceID = set.SpaceID
	if input.Method == "" {
		input.Method = "ordinary"
	}
	input.Source = "manual"
	input.Status = "confirmed"
	input.SourceRecordID = ""
	input.SuggestedResults = []types.RankedEntry{}
	input.AnswerDeleted = false
	input.Revision = 1
	if id == "" {
		input.ID = uuid.NewString()
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id != "" {
			var old types.EvaluationQuestion
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND set_id=? AND space_id=?", id, setID, set.SpaceID).First(&old).Error; err != nil {
				return evaluationError(err)
			}
			input.Source = old.Source
			input.Status = old.Status
			input.SourceRecordID = old.SourceRecordID
			input.SuggestedResults = old.SuggestedResults
			input.CreatedAt = old.CreatedAt
			input.Revision = old.Revision + 1
		}
		return saveEvaluationQuestion(ctx, tx, &input, id != "")
	})
	if err != nil {
		return nil, err
	}
	return s.GetQuestion(ctx, setID, input.ID)
}
func saveEvaluationQuestion(ctx context.Context, tx *gorm.DB, q *types.EvaluationQuestion, replace bool) error {
	if err := validateQuestion(ctx, tx, q); err != nil {
		return err
	}
	if replace {
		if err := tx.Where("question_id=?", q.ID).Delete(&types.EvaluationAnswer{}).Error; err != nil {
			return err
		}
		if err := tx.Omit("Answers").Save(q).Error; err != nil {
			return err
		}
	} else if err := tx.Omit("Answers").Create(q).Error; err != nil {
		return err
	}
	if len(q.Answers) > 0 {
		return tx.Create(&q.Answers).Error
	}
	return nil
}
func (s *EvaluationService) DeleteQuestion(ctx context.Context, setID, id string) error {
	q, err := s.GetQuestion(ctx, setID, id)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Select(clause.Associations).Delete(q).Error
}
func (s *EvaluationService) QuestionFromRecord(ctx context.Context, setID, recordID string, answers []types.EvaluationAnswer) (*types.EvaluationQuestion, error) {
	set, err := s.GetSet(ctx, setID)
	if err != nil {
		return nil, err
	}
	var record types.SearchRecord
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", recordID, set.SpaceID).First(&record).Error; err != nil {
		return nil, evaluationError(err)
	}
	var request types.EntrySearchRequest
	if err := json.Unmarshal(record.Request, &request); err != nil {
		return nil, err
	}
	request.Query = record.Query
	request.Libraries = record.Libraries
	if record.Method == "graph" {
		request.GraphParameters = append(json.RawMessage{}, record.Request...)
	}
	q := types.EvaluationQuestion{ID: uuid.NewString(), SpaceID: set.SpaceID, SetID: setID, Method: record.Method, Request: request, Source: "search_record", Status: "confirmed", SourceRecordID: recordID, SuggestedResults: record.Results, Answers: answers, Revision: 1}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return saveEvaluationQuestion(ctx, tx, &q, false) })
	if err != nil {
		return nil, err
	}
	return s.GetQuestion(ctx, setID, q.ID)
}
func (s *EvaluationService) ConfirmCandidate(ctx context.Context, setID, id string, answers []types.EvaluationAnswer) (*types.EvaluationQuestion, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scope, err := evaluationScope(ctx, tx)
		if err != nil {
			return err
		}
		var q types.EvaluationQuestion
		if err := scope.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND set_id=?", id, setID).Preload("Answers").First(&q).Error; err != nil {
			return evaluationError(err)
		}
		if q.Source != "ai" || q.Status != "pending" {
			return apperrors.NewBadRequestError("only pending AI candidates can be confirmed")
		}
		if answers != nil {
			q.Answers = answers
		}
		q.Status = "confirmed"
		q.AnswerDeleted = false
		q.Revision++
		return saveEvaluationQuestion(ctx, tx, &q, true)
	})
	if err != nil {
		return nil, err
	}
	return s.GetQuestion(ctx, setID, id)
}

// detachEvaluationContext allows terminal run state to persist after HTTP
// cancellation without keeping the caller waiting indefinitely.
func detachEvaluationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}
