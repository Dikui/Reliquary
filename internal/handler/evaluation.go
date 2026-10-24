package handler

import (
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
	"strconv"
	"time"

	apperrors "reliquary/internal/errors"

	"github.com/gin-gonic/gin"
)

type EvaluationHandler struct {
	service *service.EvaluationService
	records *service.SearchRecorder
}

func NewEvaluationHandler(s *service.EvaluationService, records *service.SearchRecorder) *EvaluationHandler {
	return &EvaluationHandler{service: s, records: records}
}

func evaluationJSON(c *gin.Context, data any, err error) {
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func evaluationBody(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if err := decodeSearchJSON(c, target); err != nil {
		c.Error(apperrors.NewBadRequestError(err.Error()))
		return false
	}
	return true
}

func evaluationPage(c *gin.Context) (int, int, bool) {
	limit, offset := 50, 0
	for key, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if raw := c.Query(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 || (key == "limit" && (n == 0 || n > 200)) {
				c.Error(apperrors.NewBadRequestError("invalid pagination"))
				return 0, 0, false
			}
			*target = n
		}
	}
	return limit, offset, true
}

func (h *EvaluationHandler) Register(v1 *gin.RouterGroup) {
	v1.GET("/search/records", h.ListRecords)
	v1.GET("/evaluation/vector-sets", func(c *gin.Context) {
		data, err := h.service.ListVectorSets(c.Request.Context())
		evaluationJSON(c, data, err)
	})
	v1.GET("/evaluation/sets", func(c *gin.Context) {
		limit, offset, ok := evaluationPage(c)
		if !ok {
			return
		}
		data, err := h.service.ListSets(c.Request.Context(), limit, offset)
		evaluationJSON(c, data, err)
	})
	v1.POST("/evaluation/sets", h.SaveSet)
	v1.GET("/evaluation/sets/:set_id", func(c *gin.Context) {
		data, err := h.service.GetSet(c.Request.Context(), c.Param("set_id"))
		evaluationJSON(c, data, err)
	})
	v1.PUT("/evaluation/sets/:set_id", h.SaveSet)
	v1.DELETE("/evaluation/sets/:set_id", func(c *gin.Context) {
		err := h.service.DeleteSet(c.Request.Context(), c.Param("set_id"))
		evaluationJSON(c, nil, err)
	})
	v1.GET("/evaluation/sets/:set_id/questions", h.ListQuestions)
	v1.POST("/evaluation/sets/:set_id/questions", h.SaveQuestion)
	v1.GET("/evaluation/sets/:set_id/questions/:question_id", func(c *gin.Context) {
		data, err := h.service.GetQuestion(c.Request.Context(), c.Param("set_id"), c.Param("question_id"))
		evaluationJSON(c, data, err)
	})
	v1.PUT("/evaluation/sets/:set_id/questions/:question_id", h.SaveQuestion)
	v1.DELETE("/evaluation/sets/:set_id/questions/:question_id", func(c *gin.Context) {
		err := h.service.DeleteQuestion(c.Request.Context(), c.Param("set_id"), c.Param("question_id"))
		evaluationJSON(c, nil, err)
	})
	v1.POST("/evaluation/sets/:set_id/from-record", h.FromRecord)
	v1.POST("/evaluation/sets/:set_id/candidates", h.GenerateCandidates)
	v1.POST("/evaluation/sets/:set_id/questions/:question_id/confirm", h.ConfirmCandidate)
	v1.POST("/evaluation/sets/:set_id/runs", func(c *gin.Context) {
		var input types.EvaluationRunOptions
		if !evaluationBody(c, &input) {
			return
		}
		data, err := h.service.Run(c.Request.Context(), c.Param("set_id"), input)
		evaluationJSON(c, data, err)
	})
	v1.GET("/evaluation/runs", func(c *gin.Context) {
		limit, offset, ok := evaluationPage(c)
		if !ok {
			return
		}
		data, err := h.service.ListRuns(c.Request.Context(), c.Query("set_id"), limit, offset)
		evaluationJSON(c, data, err)
	})
	v1.GET("/evaluation/runs/:run_id", func(c *gin.Context) {
		data, err := h.service.GetRun(c.Request.Context(), c.Param("run_id"))
		evaluationJSON(c, data, err)
	})
	v1.GET("/evaluation/compare", func(c *gin.Context) {
		if c.Query("before") == "" || c.Query("after") == "" {
			c.Error(apperrors.NewBadRequestError("before and after run IDs required"))
			return
		}
		data, err := h.service.Compare(c.Request.Context(), c.Query("before"), c.Query("after"))
		evaluationJSON(c, data, err)
	})
}

func (h *EvaluationHandler) SaveSet(c *gin.Context) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !evaluationBody(c, &input) {
		return
	}
	data, err := h.service.SaveSet(c.Request.Context(), c.Param("set_id"), input.Name, input.Description)
	evaluationJSON(c, data, err)
}

func (h *EvaluationHandler) SaveQuestion(c *gin.Context) {
	var input struct {
		Method  string                   `json:"method"`
		Request types.EntrySearchRequest `json:"request"`
		Answers []types.EvaluationAnswer `json:"answers"`
	}
	if !evaluationBody(c, &input) {
		return
	}
	data, err := h.service.SaveQuestion(c.Request.Context(), c.Param("set_id"), c.Param("question_id"), types.EvaluationQuestion{Method: input.Method, Request: input.Request, Answers: input.Answers})
	evaluationJSON(c, data, err)
}

func (h *EvaluationHandler) ListQuestions(c *gin.Context) {
	limit, offset, ok := evaluationPage(c)
	if !ok {
		return
	}
	status := c.Query("status")
	if status != "" && status != "confirmed" && status != "pending" {
		c.Error(apperrors.NewBadRequestError("status must be confirmed or pending"))
		return
	}
	data, err := h.service.ListQuestions(c.Request.Context(), c.Param("set_id"), status, limit, offset)
	evaluationJSON(c, data, err)
}

func (h *EvaluationHandler) FromRecord(c *gin.Context) {
	var input struct {
		RecordID string                   `json:"record_id"`
		Answers  []types.EvaluationAnswer `json:"answers"`
	}
	if !evaluationBody(c, &input) {
		return
	}
	data, err := h.service.QuestionFromRecord(c.Request.Context(), c.Param("set_id"), input.RecordID, input.Answers)
	evaluationJSON(c, data, err)
}

func (h *EvaluationHandler) GenerateCandidates(c *gin.Context) {
	var input service.EvaluationGenerateRequest
	if !evaluationBody(c, &input) {
		return
	}
	data, err := h.service.GenerateCandidates(c.Request.Context(), c.Param("set_id"), input)
	evaluationJSON(c, data, err)
}

func (h *EvaluationHandler) ConfirmCandidate(c *gin.Context) {
	var input struct {
		Answers []types.EvaluationAnswer `json:"answers"`
	}
	if !evaluationBody(c, &input) {
		return
	}
	data, err := h.service.ConfirmCandidate(c.Request.Context(), c.Param("set_id"), c.Param("question_id"), input.Answers)
	evaluationJSON(c, data, err)
}

func (h *EvaluationHandler) ListRecords(c *gin.Context) {
	limit, offset, ok := evaluationPage(c)
	if !ok {
		return
	}
	input := service.SearchRecordFilter{Source: c.Query("source"), Method: c.Query("method"), Library: c.Query("library"), Query: c.Query("query"), RunID: c.Query("run_id"), Limit: limit, Offset: offset}
	if raw := c.Query("include_evaluation"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			c.Error(apperrors.NewBadRequestError("invalid include_evaluation"))
			return
		}
		input.IncludeEvaluation = v
	}
	for key, target := range map[string]**time.Time{"from": &input.From, "to": &input.To} {
		if raw := c.Query(key); raw != "" {
			v, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				c.Error(apperrors.NewBadRequestError("from and to require RFC3339"))
				return
			}
			*target = &v
		}
	}
	if input.From != nil && input.To != nil && input.From.After(*input.To) {
		c.Error(apperrors.NewBadRequestError("from must precede to"))
		return
	}
	if input.Source != "" && input.Source != "web" && input.Source != "http" && input.Source != "mcp" && input.Source != "evaluation" {
		c.Error(apperrors.NewBadRequestError("invalid source"))
		return
	}
	if input.Method != "" && input.Method != "ordinary" {
		c.Error(apperrors.NewBadRequestError("invalid method"))
		return
	}
	data, err := h.records.List(c.Request.Context(), input)
	evaluationJSON(c, data, err)
}
