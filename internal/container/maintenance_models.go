package container

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reliquary/internal/config"
	"strings"
	"time"

	"github.com/panjf2000/ants/v2"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/chatgpt"
	"reliquary/internal/maintenance"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

type maintenanceModelCheck struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Ready     bool   `json:"ready"`
	Dimension int    `json:"dimension,omitempty"`
	Images    bool   `json:"images,omitempty"`
	Error     string `json:"error,omitempty"`
}

// CheckMaintenanceModels calls the production adapters, so credentials stay
// inside the server and the checks use the configured provider and SSRF policy.
func CheckMaintenanceModels(ctx context.Context, db *gorm.DB, modelID string) (any, error) {
	return checkMaintenanceModels(ctx, db, modelID, nil)
}

func checkMaintenanceModels(ctx context.Context, db *gorm.DB, modelID string, transport *http.Client) (any, error) {
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, types.SingleSpaceID)
	pool, err := ants.NewPool(1)
	if err != nil {
		return nil, err
	}
	defer pool.Release()
	ollama, err := initOllamaService()
	if err != nil {
		return nil, err
	}
	models := service.NewModelServiceWithChatGPT(repository.NewModelRepository(db), repository.NewLibraryRepository(db), ollama,
		embedding.NewBatchEmbedder(pool), service.NewSpaceService(repository.NewSpaceRepository(db)), db,
		chatgpt.NewManager(db), service.NewChatGPTPolicyService(db))
	if transport != nil {
		models.(interface{ SetChatGPTTransport(*http.Client) }).SetChatGPTTransport(transport)
	}
	configured, err := models.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	checks := make([]maintenanceModelCheck, 0, len(configured))
	var failures []error
	for _, model := range configured {
		if modelID != "" && model.ID != modelID {
			continue
		}
		check := maintenanceModelCheck{ID: model.ID, Name: model.Name}
		probeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		probe := func() error {
			if model.Status != types.ModelStatusActive {
				return errors.New("model remains paused; enter and test its required credentials before enabling it")
			}
			switch model.Type {
			case types.ModelTypeEmbedding:
				adapter, err := models.GetEmbeddingModel(probeCtx, model.ID)
				if err != nil {
					return err
				}
				check.Dimension = adapter.GetDimensions()
				vector, err := adapter.Embed(probeCtx, "Reliquary maintenance model check")
				if err != nil {
					return err
				}
				if err := validateMaintenanceVector(vector, check.Dimension); err != nil {
					return err
				}
				if model.Parameters.EmbeddingParameters.SupportsImages {
					imageAdapter, ok := adapter.(embedding.ImageEmbedder)
					if !ok || !imageAdapter.SupportsImages() {
						return errors.New("configured image embedding adapter is unavailable")
					}
					vector, err = imageAdapter.EmbedImage(probeCtx, "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=")
					if err != nil {
						return err
					}
					if err := validateMaintenanceVector(vector, check.Dimension); err != nil {
						return err
					}
					check.Images = true
				}
			case types.ModelTypeGeneration:
				adapter, err := models.GetChatModel(probeCtx, model.ID)
				if err != nil {
					return err
				}
				response, err := adapter.Chat(probeCtx, []chat.Message{{Role: "user", Content: config.Prompt("model_maintenance_probe")}}, &chat.ChatOptions{MaxTokens: 8})
				if err != nil {
					return err
				}
				if response == nil || strings.TrimSpace(response.Content) == "" {
					return errors.New("generation model returned an empty response")
				}
			case types.ModelTypeRerank:
				adapter, err := models.GetRerankModel(probeCtx, model.ID)
				if err != nil {
					return err
				}
				result, err := adapter.Rerank(probeCtx, "maintenance", []string{"Maintenance keeps recoverable information."})
				if err != nil {
					return err
				}
				if len(result) != 1 || math.IsNaN(result[0].RelevanceScore) || math.IsInf(result[0].RelevanceScore, 0) {
					return errors.New("rerank model returned invalid results")
				}
			default:
				return fmt.Errorf("unsupported configured model type %q", model.Type)
			}
			return nil
		}
		var probeErr error
		paused := false
		if model.Source == types.ModelSourceChatGPT {
			// The task 14 schema has no pause table. This maintenance-only
			// helper runs with that application offline, so probes still need
			// exact accounting audits before the old snapshot can be resumed.
			paused = true
			if db.Migrator().HasTable("maintenance_state") {
				paused, probeErr = maintenanceops.SnapshotPaused(probeCtx, db)
			}
		}
		if probeErr == nil {
			if paused {
				probeErr = maintenance.WithChatGPTPolicyProbeAudit(probeCtx, db, model.ID, probe)
			} else {
				probeErr = probe()
			}
		}
		cancel()
		if probeErr != nil {
			check.Error = maintenanceModelError(probeErr, model)
			failures = append(failures, fmt.Errorf("model %s: %s", model.ID, check.Error))
		} else {
			check.Ready = true
		}
		checks = append(checks, check)
	}
	if modelID != "" && len(checks) == 0 {
		return nil, fmt.Errorf("configured model %s was not found", modelID)
	}
	return map[string]any{"models": checks, "ready": len(failures) == 0}, errors.Join(failures...)
}

func validateMaintenanceVector(vector []float32, dimension int) error {
	if dimension <= 0 || len(vector) != dimension {
		return fmt.Errorf("model returned dimension %d, expected %d", len(vector), dimension)
	}
	norm := 0.0
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("model returned a non-finite vector")
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return errors.New("model returned a zero vector")
	}
	return nil
}

func maintenanceModelError(err error, model *types.Model) string {
	message := err.Error()
	credentials := []string{model.Parameters.APIKey, model.Parameters.AppSecret}
	for _, value := range model.Parameters.CustomHeaders {
		credentials = append(credentials, value)
	}
	for _, value := range credentials {
		if value != "" {
			message = strings.ReplaceAll(message, value, "[redacted]")
		}
	}
	if len(message) > 600 {
		message = message[:600]
	}
	return message
}
