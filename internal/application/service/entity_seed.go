package service

import (
	"reliquary/internal/types"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Institution names and institution aliases from the AIHOT taxonomy. Product
// and model names are deliberately excluded from the institution directory.
var presetInstitutions = []struct {
	name    string
	aliases []string
}{
	{"World Labs", nil},
	{"Thinking Machines Lab", []string{"Thinking Machines"}},
	{"AMD", []string{"Advanced Micro Devices"}},
	{"OpenAI", []string{"OpenAI Developers"}},
	{
		"Anthropic",
		nil,
	},
	{
		"Google",
		[]string{
			"谷歌",
			"Google DeepMind",
			"DeepMind",
			"Google Research",
			"Google AI",
			"Google Labs",
			"Google Cloud",
		},
	},

	{"DeepSeek", []string{"深度求索"}},
	{"Alibaba", []string{"阿里", "阿里巴巴", "阿里云", "Alibaba Cloud", "通义实验室", "Qwen Team"}},
	{"Moonshot AI", []string{"月之暗面", "Moonshot"}},
	{"MiniMax", []string{"稀宇科技"}},
	{
		"Zhipu AI",
		[]string{"智谱", "智谱AI", "Zhipu", "Z.ai"},
	},
	{"xAI", []string{"SpaceX Super Intelligence", "SpaceXAI", "SpaceXSI"}},
	{"Meta", []string{"Meta AI", "Meta Superintelligence Labs", "AI at Meta"}},
	{"Microsoft", []string{"微软", "Microsoft Research", "Microsoft AI"}},
	{"NVIDIA", []string{"英伟达"}},
	{"Hugging Face", []string{"HuggingFace"}},
	{"Anysphere", nil},
	{"OpenRouter", nil},
}

func ensurePresetEntities(tx *gorm.DB, spaceID uint64) error {
	if err := lockEntitySpace(tx, spaceID); err != nil {
		return err
	}
	var count int64
	if err := tx.Table("entity_seed_imports").Where("space_id=?", spaceID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, preset := range presetInstitutions {
		entity := types.Entity{
			ID:             uuid.NewString(),
			SpaceID:        spaceID,
			CanonicalName:  preset.name,
			NormalizedName: types.NormalizeEntityKey(preset.name),
			EntityType:     "organization",
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := tx.Create(&entity).Error; err != nil {
			return err
		}
		for _, alias := range preset.aliases {
			if err := saveEntityAlias(tx, entity, alias, "seed"); err != nil {
				return err
			}
		}
	}
	return tx.Exec("INSERT INTO entity_seed_imports(space_id,imported_at) VALUES (?,?)", spaceID, time.Now()).Error
}
