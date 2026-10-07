package service

import (
	"context"

	"gorm.io/gorm"

	"reliquary/internal/types"
)

const BuiltinAIKnowledgeDomainID = "c0f4804a-2c5b-4f24-a950-000000000001"

// BuiltinAIKnowledgeDomain supplies initial data through code, never SQL seeds.
func BuiltinAIKnowledgeDomain() *types.KnowledgeDomain {
	domain := &types.KnowledgeDomain{
		ID: BuiltinAIKnowledgeDomainID, TenantID: types.SingleTenantID, Name: "AI 行业",
		Description: "AI 公司、模型、产品、人物和组织的事实与变化。", IsBuiltin: true,
		EntityTypes: []string{"公司", "模型", "产品", "人物", "组织"},
		Fields: []types.DomainField{
			{Key: "context_length", Label: "上下文长度", Aliases: []string{"context length", "context window", "context_window", "max context tokens", "上下文长度", "上下文窗口"}, ValueType: "number", UnitFamily: "tokens"},
			{Key: "local_deployment", Label: "本地部署", Aliases: []string{"local deployment", "offline deployment", "offline_deployment", "本地部署", "离线部署"}, ValueType: "boolean"},
			{Key: "license", Label: "许可证", Aliases: []string{"licence", "许可证", "许可协议"}, ValueType: "text"},
			{Key: "released_on", Label: "发布日期", Aliases: []string{"release date", "release_date", "发布日期"}, ValueType: "date"},
		},
		Relations: []types.DomainRelation{
			{Key: "developed_by", Label: "开发者", Aliases: []string{"developed by", "开发者", "开发机构"}},
			{Key: "successor_of", Label: "前一版本", Aliases: []string{"successor of", "previous version", "前一版本", "上一版本"}},
		},
		UnitFamilies: []types.DomainUnitFamily{{Key: "tokens", CanonicalUnit: "token", Units: []types.DomainUnit{
			{Name: "token", Aliases: []string{"tokens", "令牌"}, Factor: 1},
			{Name: "k tokens", Aliases: []string{"ktokens", "k token", "千token", "千 tokens"}, Factor: 1000},
			{Name: "M tokens", Aliases: []string{"mtokens", "m token", "百万token"}, Factor: 1000000},
		}}},
		ExtractionHints: "Model versions are distinct objects; a family and its version are not aliases. Preserve deployment conditions, limitations, and conflicting claims in the verbatim quote. Keep numeric values and units exactly as stated; do not convert or infer them. Other supported fields may retain their source names.",
	}
	_ = validateKnowledgeDomain(domain)
	return domain
}

// EnsureBuiltinKnowledgeDomains preserves administrator edits on later starts.
func EnsureBuiltinKnowledgeDomains(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651002)).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&types.KnowledgeDomain{}).Where("tenant_id = ? AND is_builtin = ?", types.SingleTenantID, true).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		return tx.Create(BuiltinAIKnowledgeDomain()).Error
	})
}
