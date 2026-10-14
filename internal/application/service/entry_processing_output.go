package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

type extractedRelation struct {
	Subject     string                   `json:"subject"`
	Type        string                   `json:"type"`
	Object      string                   `json:"object"`
	Start       int                      `json:"start"`
	End         int                      `json:"end"`
	Speaker     string                   `json:"speaker,omitempty"`
	SubjectType string                   `json:"subject_type,omitempty"`
	ObjectType  string                   `json:"object_type,omitempty"`
	Evidence    []types.RelationEvidence `json:"evidence,omitempty"`
}
type extractedImage struct {
	Index       int    `json:"index"`
	Description string `json:"description"`
}

func libraryProcessingPrompt(l *types.Library, k *types.Entry) string {
	catalog, _ := json.Marshal(l.ContentTypes)
	relations := []string{}
	for _, row := range l.RelationTypes {
		if row.Enabled {
			relations = append(relations, row.Name)
		}
	}
	names, _ := json.Marshal(relations)
	graphOverride := k.Graph
	corrected := ""
	if k.ContentTypeCorrected {
		value := libraryGraphEnabled(l, k.ContentType, k.Graph)
		graphOverride = &value
		name, _ := json.Marshal(k.ContentType)
		corrected = "\n用户已经修正 content_type=" + string(name) + "，必须使用该类型，不再重新分类；关系开关已按此类型设置。"
	}
	override, _ := json.Marshal(graphOverride)
	return articleProcessingPrompt + "\n所属库：" + l.Name + "\ncontent_types=" + string(catalog) + "\nrelation_types=" + string(names) + "\ngraph_override=" + string(override) + corrected + `
先从 content_types 的 enabled=true 项选择 content_type，拿不准或清单外选其他。有其他时使用“其他”，否则使用 other。先定类型再决定是否抽关系：graph_override=true 总是抽，false 总是不抽，null 时依照所选类型的 extract_graph，其他默认不抽。关闭时 relations=[]。
关系优先使用 relation_types 中的名称，无相近类型才提出新名称。relations 的 subject、object 必须是本次 entities 中的 name；start/end 是证据在本段 body 内的 Unicode 字符范围 [start,end)，不要用字节或 context_before 的位置。
人物观点库每条关系必须输出 speaker：有明确发言人时使用本次 entities 的人物 name，否则使用输入 author，不将被评论对象当作发言人。
为 images 中每一张可读图片输出 image_descriptions，一图一段文字，结合上下文说明图中文字、数字及图表含义，index 使用输入图片的 index。不可读图片不猜测。
附加输出格式："relations":[{"subject":"实体名称","type":"合作","object":"另一实体名称","start":0,"end":20,"speaker":"发言人"}],"image_descriptions":[{"index":1,"description":"图片说明"}]。`
}

func libraryContentType(l *types.Library, value string) string {
	value = strings.TrimSpace(value)
	for _, row := range l.ContentTypes {
		if row.Enabled && row.Name == value {
			return value
		}
	}
	for _, row := range l.ContentTypes {
		if row.Enabled && row.Name == "其他" {
			return "其他"
		}
	}
	return types.ContentTypeOther
}
func libraryGraphEnabled(l *types.Library, value string, override *bool) bool {
	if l.Template == types.TemplateGenerated {
		return false
	}
	if override != nil {
		return *override
	}
	for _, row := range l.ContentTypes {
		if row.Enabled && row.Name == value {
			return row.ExtractGraph
		}
	}
	return false
}
func bodyRangeLocations(body string, start, end int, mapping string) []types.SourceLocation {
	r := []rune(body)
	if start < 0 || end <= start || end > len(r) {
		return []types.SourceLocation{}
	}
	c := &types.Chunk{Content: string(r[start:end]), StartAt: start, EndAt: end}
	types.AssignSourceLocations(body, []*types.Chunk{c})
	for i := range c.SourceLocations {
		c.SourceLocations[i].Mapping = mapping
	}
	return c.SourceLocations
}

func qualifySegmentOutput(l *types.Library, k *types.Entry, body string, p types.ProcessingSegment, result *articleProcessingResult) {
	ct := libraryContentType(l, *result.ContentType)
	if k.ContentTypeCorrected {
		ct = libraryContentType(l, k.ContentType)
	}
	result.ContentType = &ct
	entities := map[string]articleProcessingEntity{}
	ambiguous := map[string]bool{}
	for _, e := range result.Entities {
		key := normalizeEntityName(e.Name)
		if prev, ok := entities[key]; ok && types.NormalizeEntityType(*prev.Type) != types.NormalizeEntityType(*e.Type) {
			ambiguous[key] = true
		}
		entities[key] = e
	}
	valid := []extractedRelation{}
	if libraryGraphEnabled(l, ct, k.Graph) {
		for _, rel := range result.Relations {
			a, okA := entities[normalizeEntityName(rel.Subject)]
			b, okB := entities[normalizeEntityName(rel.Object)]
			if !okA || !okB || ambiguous[normalizeEntityName(rel.Subject)] || ambiguous[normalizeEntityName(rel.Object)] || strings.TrimSpace(rel.Type) == "" || len([]rune(rel.Type)) > 100 || rel.Start < 0 || rel.End <= rel.Start || rel.End > p.End-p.Start {
				continue
			}
			rel.SubjectType, rel.ObjectType = types.NormalizeEntityType(*a.Type), types.NormalizeEntityType(*b.Type)
			rel.Evidence = []types.RelationEvidence{{Locations: bodyRangeLocations(body, p.Start+rel.Start, p.Start+rel.End, "exact")}}
			if l.SystemRole == "opinions" {
				source, _ := entrySourceInfo(k)
				speaker := &types.ProcessingSpeaker{Name: source.Author, Selection: "author"}
				if e, ok := entities[normalizeEntityName(rel.Speaker)]; ok && types.NormalizeEntityType(*e.Type) == "person" {
					speaker = &types.ProcessingSpeaker{Name: e.Name, Selection: "extracted_person"}
				}
				if speaker.Name == "" {
					continue
				}
				rel.Evidence[0].Speaker = speaker
			}
			valid = append(valid, rel)
		}
	}
	result.Relations = valid
}

// ReadEntryProcessingOutput supplies the latest accepted processing evidence.
// Scope and saved-body hash let consumers reject stale evidence after supplementation.
func ReadEntryProcessingOutput(ctx context.Context, db *gorm.DB, spaceID uint64, entryID string) (*types.EntryProcessingOutput, error) {
	var result types.EntryProcessingOutput
	err := db.WithContext(ctx).Where("space_id=? AND entry_id=?", spaceID, entryID).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func relationNameKey(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
}
func similarRelationNames(a, b string) bool {
	a, b = relationNameKey(a), relationNameKey(b)
	return a != "" && b != "" && (a == b || strings.Contains(a, b) || strings.Contains(b, a))
}

func (s *entryService) canonicalRelationTypes(ctx context.Context, l *types.Library, raw []string) (map[string]string, error) {
	result := map[string]string{}
	if len(raw) == 0 {
		return result, nil
	}
	// Lock the parent library so concurrent processing cannot add synonymous rows.
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent types.Library
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id=? AND space_id=?", l.ID, l.SpaceID).First(&parent).Error; err != nil {
			return err
		}
		var rows []types.LibraryRelationType
		if err := tx.Where("library_id=?", l.ID).Order("position,name").Find(&rows).Error; err != nil {
			return err
		}
		original := append([]string{}, raw...)
		raw = append([]string{}, raw...)
		for i, name := range raw {
			mapped, err := graph.MapType(tx, l.ID, name)
			if err != nil {
				return err
			}
			raw[i] = mapped
		}
		names := []string{}
		for _, row := range rows {
			if row.Enabled {
				names = append(names, row.Name)
			}
		}
		// Known names bypass vector lookup entirely. Novel names share one text batch.
		novel := false
		for _, name := range raw {
			if matchingRelationType(name, names, nil, 1) == "" {
				novel = true
			}
		}
		// No additional generative model call.
		vectors := map[string][]float32{}
		var embedder embedding.Embedder
		var embedErr error
		if novel && s.modelService != nil {
			embedder, embedErr = GetTierEmbeddingModel(ctx, tx, s.modelService, l)
		}
		if embedErr == nil && embedder != nil {
			inputs := append(append([]string{}, names...), raw...)
			values, err := embedder.BatchEmbed(types.WithLLMCallMetadata(ctx, "relation_type_embedding", ""), inputs)
			if err != nil {
				return fmt.Errorf("relation type embeddings: %w", err)
			}
			if len(values) != len(inputs) {
				return errors.New("relation type embedding count mismatch")
			}
			for i, name := range inputs {
				vectors[name] = values[i]
			}
		}
		threshold := 0.92
		if s.config != nil && s.config.ArticleProcessing != nil && s.config.ArticleProcessing.RelationTypeSimilarityThreshold > 0 {
			threshold = s.config.ArticleProcessing.RelationTypeSimilarityThreshold
		}
		position := 0
		for _, row := range rows {
			position = max(position, row.Position+1)
		}
		for _, name := range raw {
			if _, ok := result[name]; ok {
				continue
			}
			chosen := matchingRelationType(name, names, vectors, threshold)
			if chosen == "" {
				chosen = strings.TrimSpace(name)
				if relationNameKey(chosen) == "" {
					return errors.New("empty normalized relation type")
				}
				// Disabled exact names are not silently re-enabled or overwritten.
				for _, row := range rows {
					if row.Name == chosen && !row.Enabled {
						return fmt.Errorf("relation type %q is disabled", chosen)
					}
				}
				row := types.LibraryRelationType{LibraryID: l.ID, Name: chosen, Position: position, Enabled: true}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
				after, _ := json.Marshal(map[string]any{"library_id": l.ID, "name": chosen, "method": "automatic_processing"})
				if err := tx.Create(&types.OperationLog{ID: uuid.NewString(), SpaceID: l.SpaceID, Actor: "system:processing", OperationType: "add_relation_type", ObjectID: l.ID, Before: types.JSON(`null`), After: types.JSON(after), CreatedAt: time.Now()}).Error; err != nil {
					return err
				}
				names = append(names, chosen)
				rows = append(rows, row)
				position++
				vectors[chosen] = vectors[name]
			}
			result[name] = chosen
		}
		for i, name := range original {
			result[name] = result[raw[i]]
		}
		return nil
	})
	return result, err
}
func relationCosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return -1
	}
	var dot, aa, bb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		aa += float64(a[i]) * float64(a[i])
		bb += float64(b[i]) * float64(b[i])
	}
	if aa == 0 || bb == 0 {
		return -1
	}
	return dot / math.Sqrt(aa*bb)
}

func (s *entryService) saveProcessingOutput(ctx context.Context, l *types.Library, k *types.Entry, body string, result *articleProcessingResult, resolved []types.Entity) error {
	ids := map[string]string{}
	for _, e := range resolved {
		for _, name := range append([]string{e.CanonicalName}, e.Aliases...) {
			ids[entityNameKey(e.EntityType, name)] = e.ID
		}
	}
	out := &types.EntryProcessingOutput{EntryID: k.ID, SpaceID: k.SpaceID, LibraryID: k.LibraryID, RunID: result.RunID, BodyHash: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), ContentType: k.ContentType, Relations: []types.ProcessingRelation{}, Images: result.Images, UpdatedAt: time.Now()}
	if out.Images == nil {
		out.Images = []types.ProcessingImage{}
	}
	raw := []string{}
	if libraryGraphEnabled(l, k.ContentType, k.Graph) {
		for _, r := range result.Relations {
			if ids[entityNameKey(r.SubjectType, r.Subject)] != "" && ids[entityNameKey(r.ObjectType, r.Object)] != "" {
				raw = append(raw, r.Type)
			}
		}
	}
	canonical, err := s.canonicalRelationTypes(ctx, l, raw)
	if err != nil {
		return err
	}
	index := map[string]int{}
	source, _ := entrySourceInfo(k)
	for _, r := range result.Relations {
		name := canonical[r.Type]
		a, b := ids[entityNameKey(r.SubjectType, r.Subject)], ids[entityNameKey(r.ObjectType, r.Object)]
		if name == "" || a == "" || b == "" {
			continue
		}
		evidence := r.Evidence
		if l.SystemRole == "opinions" {
			for i := range evidence {
				name := r.Speaker
				if evidence[i].Speaker != nil {
					name = evidence[i].Speaker.Name
				}
				speaker := &types.ProcessingSpeaker{Name: source.Author, Selection: "author"}
				for _, e := range result.Entities {
					if types.NormalizeEntityType(*e.Type) == "person" && normalizeEntityName(e.Name) == normalizeEntityName(name) {
						if id := ids[entityNameKey("person", e.Name)]; id != "" {
							speaker = &types.ProcessingSpeaker{EntityID: id, Name: e.Name, Selection: "extracted_person"}
						}
					}
				}
				if speaker.Name == "" {
					return errors.New("opinion relation has no identifiable speaker")
				}
				evidence[i].Speaker = speaker
			}
		}
		key := a + "\x00" + name + "\x00" + b
		if i, ok := index[key]; ok {
			out.Relations[i].Evidence = append(out.Relations[i].Evidence, evidence...)
		} else {
			index[key] = len(out.Relations)
			out.Relations = append(out.Relations, types.ProcessingRelation{SubjectID: a, Type: name, ObjectID: b, Evidence: evidence})
		}
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}}, UpdateAll: true}).Create(out).Error
}

func matchingRelationType(name string, names []string, vectors map[string][]float32, threshold float64) string {
	for _, old := range names {
		if name == old {
			return old
		}
	}
	for _, old := range names {
		if similarRelationNames(name, old) {
			return old
		}
	}
	chosen := ""
	best := threshold
	for _, old := range names {
		if score := relationCosine(vectors[name], vectors[old]); score > best {
			chosen, best = old, score
		}
	}
	return chosen
}
