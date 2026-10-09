package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/service"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type ModelRoutingHandler struct {
	db                     *gorm.DB
	models                 interfaces.ModelService
	nameEmbeddingRebuilder service.ContextNameEmbeddingRebuildTrigger
}

func (h *ModelRoutingHandler) SetNameEmbeddingRebuilder(rebuilder service.ContextNameEmbeddingRebuildTrigger) {
	h.nameEmbeddingRebuilder = rebuilder
}

func NewModelRoutingHandler(db *gorm.DB, models interfaces.ModelService) *ModelRoutingHandler {
	return &ModelRoutingHandler{db: db, models: models}
}

// Get returns global model defaults or the selected scope's effective models.
// @Summary 获取模型分级默认配置
// @Tags 空间
// @Produce json
// @Param domain_id query string false "领域 ID"
// @Param knowledge_base_id query string false "知识库 ID"
// @Success 200 {object} map[string]interface{}
// @Router /space/model-tiers [get]
func (h *ModelRoutingHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := types.MustTenantIDFromContext(ctx)
	var space types.Tenant
	if err := h.db.WithContext(ctx).Select("model_tiers").Where("id = ?", tenantID).First(&space).Error; err != nil {
		c.Error(err)
		return
	}
	overrides := space.ModelTiers
	var kb *types.KnowledgeBase
	if id := c.Query("knowledge_base_id"); id != "" {
		kb = &types.KnowledgeBase{}
		if err := h.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(kb).Error; err != nil {
			c.Error(apperrors.NewNotFoundError("knowledge base not found"))
			return
		}
		overrides = kb.ModelTiers
	} else if id := c.Query("domain_id"); id != "" {
		var domain types.KnowledgeDomain
		if err := h.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&domain).Error; err != nil {
			c.Error(apperrors.NewNotFoundError("domain not found"))
			return
		}
		kb = &types.KnowledgeBase{TenantID: tenantID, DomainID: domain.ID}
		overrides = domain.ModelTiers
	}
	view, err := service.ModelRoutingForScope(ctx, h.db, kb, overrides)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// Update saves global model defaults.
// @Summary 保存模型分级全局默认
// @Tags 空间
// @Accept json
// @Produce json
// @Param request body types.ModelTierConfig true "全局默认模型"
// @Success 200 {object} map[string]interface{}
// @Router /space/model-tiers [put]
func (h *ModelRoutingHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := types.MustTenantIDFromContext(ctx)
	var config types.ModelTierConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid model tiers"))
		return
	}
	if err := service.ValidateModelTierConfig(ctx, h.db, config); err != nil {
		c.Error(apperrors.NewBadRequestError(err.Error()))
		return
	}
	changed := false
	if err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous types.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("model_tiers").Where("id = ?", tenantID).First(&previous).Error; err != nil {
			return err
		}
		changed = previous.ModelTiers.ModelID(types.ModelTierEmbedding) != config.ModelID(types.ModelTierEmbedding)
		return tx.Model(&types.Tenant{}).Where("id = ?", tenantID).Update("model_tiers", config).Error
	}); err != nil {
		c.Error(err)
		return
	}
	if h.nameEmbeddingRebuilder != nil && changed {
		if err := h.nameEmbeddingRebuilder.MarkAndEnqueue(ctx, tenantID, "global_embedding_profile_changed"); err != nil {
			c.Error(apperrors.NewInternalServerError("global settings saved but name embedding rebuild could not be queued").WithDetails(map[string]any{"settings_saved": true, "retry_action": "name_embedding_rebuild"}))
			return
		}
	}
	h.Get(c)
}
