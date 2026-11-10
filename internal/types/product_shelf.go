package types

import (
	"fmt"
	"slices"
	"strings"
)

// ShelfOption keys identify configured values across renames and reordering.
// Array order is authoritative, including the stage progression.
type ShelfOption struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type ShelfConfig struct {
	Parsers       []ShelfParser     `json:"parsers,omitempty"`
	DefaultParser string            `json:"default_parser,omitempty"`
	ParserRules   []ShelfParserRule `json:"parser_rules,omitempty"`
	MaxFileBytes  int64             `json:"max_file_bytes,omitempty"`
	Standards     []ShelfStandard   `json:"standards,omitempty"`
	ModuleTypes   []ShelfOption     `json:"module_types,omitempty"`
	Markets       []ShelfOption     `json:"markets,omitempty"`
	Regions       []ShelfOption     `json:"regions,omitempty"`
	Stages        []ShelfOption     `json:"stages,omitempty"`
}

func ProductShelfSchema() EntitySchema {
	return EntitySchema{Kinds: []EntityKind{
		{Key: "product_line", Name: "产品线", ParentKinds: []string{}},
		{Key: "platform", Name: "产品平台", ParentKinds: []string{"product_line"}},
		{Key: "product", Name: "产品名称", ParentKinds: []string{"platform"}},
		{Key: "model", Name: "产品型号", ParentKinds: []string{"product"}},
		{Key: "part", Name: "部件", ParentKinds: []string{}},
		{Key: "document_requirement", Name: "资料要求", ParentKinds: []string{}},
		{Key: "certification", Name: "认证记录", ParentKinds: []string{}},
		{Key: "document_file", Name: "资料文件", ParentKinds: []string{}},
	}, Fields: ProductShelfFields()}
}

func ProductShelfRelations() []LibraryRelationType {
	rules := []LibraryRelationType{
		{Name: "belongs_to", Hierarchical: true, SubjectKinds: []string{"platform", "product", "model"}, ObjectKinds: []string{"product_line", "platform", "product"}},
		{Name: "composed_of", SubjectKinds: []string{"model"}, ObjectKinds: []string{"part"}, Fields: []EntityFieldDefinition{{Key: "module_type", Name: "模块类型", Type: "text", Kinds: []string{}}}},
		{Name: "applies_to", SubjectKinds: []string{"document_requirement"}, ObjectKinds: []string{"product_line"}},
		{Name: "has_certification", SubjectKinds: []string{"platform", "product", "model", "part"}, ObjectKinds: []string{"certification"}},
		{Name: "has_document", SubjectKinds: []string{"platform", "product", "model", "part", "certification"}, ObjectKinds: []string{"document_file"}},
		{Name: "fulfills", SubjectKinds: []string{"document_file"}, ObjectKinds: []string{"document_requirement"}},
	}
	for i := range rules {
		rules[i].Enabled, rules[i].Position = true, i
		if rules[i].Fields == nil {
			rules[i].Fields = []EntityFieldDefinition{}
		}
	}
	return rules
}

func (c ShelfConfig) FileSizeLimit() int64 {
	if c.MaxFileBytes == 0 {
		return 32 << 20
	}
	return c.MaxFileBytes
}

func ValidateProductShelf(l *Library) error {
	if err := ValidateShelfParsers(l.ShelfConfig); err != nil {
		return err
	}
	if l.ShelfConfig.MaxFileBytes < 0 || l.ShelfConfig.MaxFileBytes > int64(^uint64(0)>>1)-(1<<20)-1 {
		return fmt.Errorf("invalid shelf file size limit")
	}
	if !l.ShelfEnabled && l.ShelfConfig.MaxFileBytes != 0 {
		return fmt.Errorf("shelf file size limit requires an enabled shelf")
	}

	if !l.ShelfEnabled {
		if len(l.ShelfConfig.ModuleTypes)+len(l.ShelfConfig.Markets)+len(l.ShelfConfig.Regions)+len(l.ShelfConfig.Stages)+len(l.ShelfConfig.Standards)+len(l.ShelfConfig.Parsers) != 0 {
			return fmt.Errorf("shelf configuration requires an enabled shelf")
		}
		return nil
	}
	if !l.ManagesEntities() || len(l.ProductLibraryIDs) > 0 {
		return fmt.Errorf("a shelf requires managed mode and cannot use automatic product matching")
	}
	expected := ProductShelfSchema()
	if len(l.EntitySchema.Kinds) != len(expected.Kinds) {
		return fmt.Errorf("shelf entity kinds are fixed")
	}
	for _, kind := range expected.Kinds {
		actual, ok := l.EntitySchema.Kind(kind.Key)
		if !ok || !slices.Equal(actual.ParentKinds, kind.ParentKinds) {
			return fmt.Errorf("shelf parent kinds are fixed: %s", kind.Key)
		}
	}
	for _, required := range ProductShelfFields() {
		found := false
		for _, field := range l.EntitySchema.Fields {
			if field.Key == required.Key {
				found = field.Type == required.Type && slices.Equal(field.Kinds, required.Kinds) && len(field.ProductLineIDs) == 0 && !field.Archived
			}
		}
		if !found {
			return fmt.Errorf("shelf field definition is fixed: %s", required.Key)
		}
	}
	rules := ProductShelfRelations()
	if len(l.RelationTypes) != len(rules) {
		return fmt.Errorf("shelf relationship types are fixed")
	}
	for _, rule := range rules {
		found := false
		for _, actual := range l.RelationTypes {
			if actual.Name == rule.Name {
				if actual.Name == "composed_of" {
					moduleField := false
					for _, field := range actual.Fields {
						if field.Key == "module_type" && field.Type == "text" && !field.Archived && len(field.Kinds) == 0 {
							moduleField = true
						}
					}
					if !moduleField {
						return fmt.Errorf("composition module_type field is fixed")
					}
				}
				objectKindsMatch := slices.Equal(actual.ObjectKinds, rule.ObjectKinds)
				// Existing shelves may retain the old schema without converting
				// their data. Requirement writes still enforce product lines only.
				if rule.Name == "applies_to" && slices.Equal(actual.ObjectKinds, []string{"product_line", "document_requirement"}) {
					objectKindsMatch = true
				}
				found = actual.Enabled && actual.Hierarchical == rule.Hierarchical && slices.Equal(actual.SubjectKinds, rule.SubjectKinds) && objectKindsMatch
			}
		}
		if !found {
			return fmt.Errorf("shelf relationship rule is fixed: %s", rule.Name)
		}
	}
	for _, list := range [][]ShelfOption{l.ShelfConfig.ModuleTypes, l.ShelfConfig.Regions, l.ShelfConfig.Markets, l.ShelfConfig.Stages} {
		if len(list) > 100 {
			return fmt.Errorf("shelf lists allow at most 100 values")
		}
		keys, names := map[string]bool{}, map[string]bool{}
		for _, item := range list {
			name := strings.ToLower(strings.TrimSpace(item.Name))
			if !entitySchemaKey.MatchString(item.Key) || keys[item.Key] || name == "" || len([]rune(item.Name)) > 100 || names[name] {
				return fmt.Errorf("shelf list values require unique keys and nonblank unique names of at most 100 characters")
			}
			keys[item.Key], names[name] = true, true
		}
	}
	return validateShelfStandards(l.ShelfConfig.Standards)
}
