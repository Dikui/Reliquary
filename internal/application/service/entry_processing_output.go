package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	"reliquary/internal/types"
)

type extractedRelation struct {
	Subject     string                   `json:"subject"`
	Type        string                   `json:"type"`
	Object      string                   `json:"object"`
	Speaker     string                   `json:"speaker,omitempty"`
	SubjectType string                   `json:"subject_type,omitempty"`
	ObjectType  string                   `json:"object_type,omitempty"`
	EvidenceIDs []int                    `json:"evidence_ids,omitempty"`
	Evidence    []types.RelationEvidence `json:"evidence,omitempty"`
}
type extractedImage struct {
	Index       int    `json:"index"`
	Description string `json:"description"`
}

func libraryProcessingPrompt(l *types.Library, k *types.Entry) string {
	catalog, _ := json.Marshal(l.ContentTypes)
	prompt := articleProcessingPrompt + "\n所属库：" + l.Name + "\ncontent_types=" + string(catalog) + `
先从 content_types 的 enabled=true 项选择 content_type，拿不准或清单外选其他。有其他时使用“其他”，否则使用 other。`
	switch {
	case l.SystemRole == "media":
		prompt += "\n本库保存个人观点。摘要保留作者、观点、判断条件与第一手经验的归属；不得把作者评价改写成已核实的客观事实。"
	case l.Template == types.TemplateExternalDurable:
		prompt += "\n本库保存长期参考文档。摘要优先保留定义、方法、适用条件和必要步骤；不补写未提供的代码或章节。"
	case l.Template == types.TemplateOwn:
		prompt += "\n本库保存个人工作资料。摘要保留本人的决定、经验和约束，不推断外部权威背书。"
	default:
		prompt += "\n本库保存机构发布。摘要保留发布主体、具体变化、时间、条件和研究结论；宣传用语不能替代事实。"
	}
	if k.ContentTypeCorrected {
		name, _ := json.Marshal(k.ContentType)
		prompt += "\n用户已经修正 content_type=" + string(name) + "，必须使用该类型，不再重新分类。"
	}
	if !l.ExtractsEntities() {
		return prompt + "\n本库不抽取实体、实体短介绍或实体间关系。entities 和 relations 必须返回空数组，不受 graph_override 或内容类型开关影响。"
	}
	relations := []string{}
	for _, row := range l.RelationTypes {
		if row.Enabled {
			relations = append(relations, row.Name)
		}
	}
	names, _ := json.Marshal(relations)
	graphOverride := k.Graph
	if k.ContentTypeCorrected {
		value := libraryGraphEnabled(l, k.ContentType, k.Graph)
		graphOverride = &value
	}
	override, _ := json.Marshal(graphOverride)
	return prompt + "\n" + articleEntityProcessingPrompt + "\nrelation_types=" + string(names) + "\ngraph_override=" + string(override) + `
先定类型再决定是否抽关系：graph_override=true 总是抽，false 总是不抽，null 时依照所选类型的 extract_graph，其他默认不抽。关闭时 relations=[]。
关系只保留原文明示、对本文有价值的联系；subject、object 必须是本次 entities 中的 name。evidence_ids 引用足以支持主体、关系含义和客体的段落，可多选以补充指代上下文。先确定原文表达的具体联系，再选 relation_types 中语义一致的名称；没有一致名称就使用精确的新名称，不能为套用清单而改写原意。每条边必须能从所选段落直接读出，不能依赖常识或“这个产品总归由它发布过”的推断。特别是“发布”必须有发布/宣布/推出该对象的动作证据：“甲的产品乙”“第一方部署如乙”“甲的安全框架乙”均不是发布证据，只能按原文明示的隶属等含义处理或不抽关系。仅同文出现不构成关系。基准比较应写“基准比较”或“基准优于”，不能改成商业竞争。原文有限定的比较应保留其范围，不扩展为全方面优于。
逐条按“subject type object”读回核对方向：“隶属”是下级/产品指向上级/所属组织，“发布”是发布者指向发布对象，“客户”是客户指向供应方或所用产品，“投资”是投资方指向被投资方；“甲的产品乙”应为“乙 隶属 甲”，不能反过来。
关系两端可以是本篇新识别或已有的合格实体，但不能为补关系而额外创建不合格对象。具体型号的发布、成绩或比较不能扩大为家族整体的事实，子团队动作不能上提给母公司；无法准确表达时不建该边，具体事实留在摘要与原文中。
媒体信息库每条关系必须输出 speaker：原文明示发言人时，使用证据段落中实际出现的姓名，并将包含姓名及发言归属的段落纳入 evidence_ids；姓名不要求出现在 entities，也不为发言人建档。没有明确引述他人时使用输入 author，不将被评论对象当作发言人；不能确定归属就不抽该关系。
附加输出格式："relations":[{"subject":"实体名称","type":"合作","object":"另一实体名称","evidence_ids":[1],"speaker":"发言人"}]。`
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
	if !l.ExtractsEntities() {
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

// Number existing Markdown blocks without changing stored text or coordinates.
func articleEvidenceBody(body string) (string, []types.SourceLocation) {
	runes := []rune(body)
	locations := bodyRangeLocations(body, 0, len(runes), "exact")
	var numbered strings.Builder
	for i, location := range locations {
		fmt.Fprintf(&numbered, "[P%d]\n%s\n", i+1, string(runes[location.Start:location.End]))
	}
	return numbered.String(), locations
}

func articleEvidenceLocations(ids []int, blocks []types.SourceLocation) []types.SourceLocation {
	if len(ids) == 0 {
		return nil
	}
	locations := []types.SourceLocation{}
	seen := map[int]bool{}
	for _, id := range ids {
		if id < 1 || id > len(blocks) {
			return nil
		}
		if !seen[id] {
			locations = append(locations, blocks[id-1])
			seen[id] = true
		}
	}
	return locations
}

func eligibleProcessingEntity(entity articleProcessingEntity) bool {
	if entity.Type == nil || (entity.Relevance != "central" && entity.Relevance != "participant") {
		return false
	}
	switch types.NormalizeEntityType(*entity.Type) {
	case "company", "product", "organization":
		return true
	}
	return false
}

func processingRelationSpeaker(author, name, body string, locations []types.SourceLocation) *types.ProcessingSpeaker {
	author, name = strings.TrimSpace(author), strings.TrimSpace(name)
	if name == "" || normalizeEntityName(name) == normalizeEntityName(author) {
		if author == "" {
			return nil
		}
		return &types.ProcessingSpeaker{Name: author, Selection: "author"}
	}
	if normalizeEntityName(name) == "" || len([]rune(name)) > 512 {
		return nil
	}
	pattern := "(?i)" + regexp.QuoteMeta(name)
	letters := []rune(name)
	if unicode.Is(unicode.Latin, letters[0]) || unicode.IsDigit(letters[0]) {
		pattern = `(^|[^\p{Latin}\p{N}_])` + pattern
	}
	if last := letters[len(letters)-1]; unicode.Is(unicode.Latin, last) || unicode.IsDigit(last) {
		pattern += `($|[^\p{Latin}\p{N}_])`
	}
	mention := regexp.MustCompile(pattern)
	text := []rune(body)
	for _, location := range locations {
		if location.Start >= 0 && location.End > location.Start && location.End <= len(text) && mention.MatchString(string(text[location.Start:location.End])) {
			return &types.ProcessingSpeaker{Name: name, Selection: "extracted_person"}
		}
	}
	return nil
}

func qualifySegmentOutput(l *types.Library, k *types.Entry, body string, p types.ProcessingSegment, result *articleProcessingResult) {
	if !l.ExtractsEntities() {
		result.Entities = []articleProcessingEntity{}
		result.Relations = []extractedRelation{}
	}
	ct := libraryContentType(l, *result.ContentType)
	if k.ContentTypeCorrected {
		ct = libraryContentType(l, k.ContentType)
	}
	result.ContentType = &ct
	runes := []rune(body)
	_, blocks := articleEvidenceBody(string(runes[p.Start:p.End]))
	accepted := []articleProcessingEntity{}
	for _, entity := range result.Entities {
		if !eligibleProcessingEntity(entity) {
			continue
		}
		entity.Aliases = nil
		entity.Description = strings.TrimSpace(entity.Description)
		locations := articleEvidenceLocations(entity.EvidenceIDs, blocks)
		if len(locations) == 0 {
			continue
		}
		entity.Evidence = []types.SourceLocation{}
		for _, location := range locations {
			entity.Evidence = append(entity.Evidence, bodyRangeLocations(body, p.Start+location.Start, p.Start+location.End, "exact")...)
		}
		accepted = append(accepted, entity)
	}
	result.Entities = accepted
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
			if !okA || !okB || ambiguous[normalizeEntityName(rel.Subject)] || ambiguous[normalizeEntityName(rel.Object)] || strings.TrimSpace(rel.Type) == "" || len([]rune(rel.Type)) > 100 {
				continue
			}
			locations := articleEvidenceLocations(rel.EvidenceIDs, blocks)
			if len(locations) == 0 {
				continue
			}
			global := []types.SourceLocation{}
			for _, location := range locations {
				global = append(global, bodyRangeLocations(body, p.Start+location.Start, p.Start+location.End, "exact")...)
			}
			rel.SubjectType, rel.ObjectType = types.NormalizeEntityType(*a.Type), types.NormalizeEntityType(*b.Type)
			// An organization cannot belong to its product or project. Reject
			// reversed ownership edges rather than inventing a corrected claim.
			if strings.TrimSpace(rel.Type) == "隶属" && (rel.SubjectType == "company" || rel.SubjectType == "organization") && (rel.ObjectType == "product" || rel.ObjectType == "project") {
				continue
			}
			rel.Evidence = []types.RelationEvidence{{Locations: global}}
			if l.SystemRole == "media" {
				source, _ := entrySourceInfo(k)
				speaker := processingRelationSpeaker(source.Author, rel.Speaker, body, global)
				if speaker == nil {
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
func (s *entryService) canonicalRelationTypes(ctx context.Context, l *types.Library, raw []string) (map[string]string, error) {
	result := map[string]string{}
	if len(raw) == 0 {
		return result, nil
	}
	// Serialize catalog additions with relation-type merges.
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
		position := 0
		for _, row := range rows {
			position = max(position, row.Position+1)
		}
		for _, name := range raw {
			if _, ok := result[name]; ok {
				continue
			}
			chosen := matchingRelationType(name, names)
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
func (s *entryService) saveProcessingOutput(ctx context.Context, l *types.Library, k *types.Entry, body string, result *articleProcessingResult, resolved []types.Entity) error {
	ctx = types.WithModelCallEntry(ctx, k.ID)
	ids := map[string]string{}
	for _, e := range resolved {
		for _, name := range append([]string{e.CanonicalName}, e.Aliases...) {
			ids[entityNameKey(e.EntityType, name)] = e.ID
		}
	}
	for key, id := range result.ResolvedEntities {
		ids[key] = id
	}
	// Historical aliases must not restore endpoints excluded from this extraction.
	allowed := map[string]bool{}
	for _, entity := range result.Entities {
		if eligibleProcessingEntity(entity) {
			allowed[entityNameKey(*entity.Type, entity.Name)] = true
		}
	}
	for key := range ids {
		if !allowed[key] {
			delete(ids, key)
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
		if name == "" || a == "" || b == "" || a == b {
			continue
		}
		if name == "隶属" && (r.SubjectType == "company" || r.SubjectType == "organization") && (r.ObjectType == "product" || r.ObjectType == "project") {
			continue
		}
		evidence := r.Evidence
		if l.SystemRole == "media" {
			for i := range evidence {
				name := r.Speaker
				if evidence[i].Speaker != nil {
					name = evidence[i].Speaker.Name
				}
				speaker := processingRelationSpeaker(source.Author, name, body, evidence[i].Locations)
				if speaker == nil {
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

func matchingRelationType(name string, names []string) string {
	for _, old := range names {
		if strings.TrimSpace(name) == old {
			return old
		}
	}
	return ""
}
