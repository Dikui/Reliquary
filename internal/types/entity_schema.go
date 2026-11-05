package types

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	EntityModeExtracted = "extracted"
	EntityModeManaged   = "managed"
)

type EntitySchema struct {
	Kinds  []EntityKind            `json:"kinds"`
	Fields []EntityFieldDefinition `json:"fields"`
}

type EntityKind struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	ParentKinds []string `json:"parent_kinds"`
}

type EntityFieldDefinition struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Unit     string   `json:"unit,omitempty"`
	Options  []string `json:"options,omitempty"`
	Kinds    []string `json:"kinds"`
	Archived bool     `json:"archived"`
}

func (l *Library) ManagesEntities() bool { return l != nil && l.EntityMode == EntityModeManaged }

func (s EntitySchema) Kind(key string) (EntityKind, bool) {
	for _, k := range s.Kinds {
		if k.Key == key {
			return k, true
		}
	}
	return EntityKind{}, false
}

var entitySchemaKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func ValidateEntitySchema(l *Library) error {
	if l.EntityMode != "" && l.EntityMode != EntityModeExtracted && l.EntityMode != EntityModeManaged {
		return fmt.Errorf("entity_mode must be extracted or managed")
	}
	if len(l.EntitySchema.Kinds) > 100 || len(l.EntitySchema.Fields) > 200 {
		return fmt.Errorf("entity schema exceeds 100 kinds or 200 fields")
	}
	kinds := map[string]bool{}
	for _, k := range l.EntitySchema.Kinds {
		if !entitySchemaKey.MatchString(k.Key) || kinds[k.Key] || strings.TrimSpace(k.Name) == "" || len([]rune(k.Name)) > 100 {
			return fmt.Errorf("entity kinds require unique keys and names of 1-100 characters")
		}
		kinds[k.Key] = true
	}
	validateKinds := func(keys []string) error {
		seen := map[string]bool{}
		for _, key := range keys {
			if !kinds[key] || seen[key] {
				return fmt.Errorf("unknown or repeated entity kind: %s", key)
			}
			seen[key] = true
		}
		return nil
	}
	for _, k := range l.EntitySchema.Kinds {
		if err := validateKinds(k.ParentKinds); err != nil {
			return err
		}
	}
	fields := map[string]bool{}
	for _, f := range l.EntitySchema.Fields {
		if !entitySchemaKey.MatchString(f.Key) || fields[f.Key] || strings.TrimSpace(f.Name) == "" || len([]rune(f.Name)) > 100 || len([]rune(f.Unit)) > 40 {
			return fmt.Errorf("entity fields require unique keys and names of 1-100 characters")
		}
		fields[f.Key] = true
		if !slices.Contains([]string{"text", "number", "boolean", "date", "select"}, f.Type) {
			return fmt.Errorf("invalid field type: %s", f.Type)
		}
		if err := validateKinds(f.Kinds); err != nil {
			return err
		}
		if f.Type == "select" && (len(f.Options) == 0 || len(f.Options) > 100) {
			return fmt.Errorf("select fields require 1-100 options")
		}
		seen := map[string]bool{}
		for _, option := range f.Options {
			if strings.TrimSpace(option) == "" || len([]rune(option)) > 200 || seen[option] {
				return fmt.Errorf("field options must be nonblank and unique")
			}
			seen[option] = true
		}
	}
	hierarchies := 0
	for _, r := range l.RelationTypes {
		if !l.ManagesEntities() && (r.Hierarchical || len(r.SubjectKinds) > 0 || len(r.ObjectKinds) > 0) {
			return fmt.Errorf("relation kind rules require a managed library")
		}
		if err := validateKinds(r.SubjectKinds); err != nil {
			return err
		}
		if err := validateKinds(r.ObjectKinds); err != nil {
			return err
		}
		if r.Hierarchical && r.Enabled {
			hierarchies++
		}
	}
	if hierarchies > 1 {
		return fmt.Errorf("only one enabled hierarchy relation is allowed")
	}
	return nil
}

func (s EntitySchema) ValidateAttributes(kind string, attributes map[string]any) error {
	if _, ok := s.Kind(kind); !ok {
		return fmt.Errorf("unknown entity kind: %s", kind)
	}
	fields := make(map[string]EntityFieldDefinition, len(s.Fields))
	for _, f := range s.Fields {
		fields[f.Key] = f
	}
	for key, value := range attributes {
		f, ok := fields[key]
		if !ok || (len(f.Kinds) > 0 && !slices.Contains(f.Kinds, kind)) {
			return fmt.Errorf("field %s does not apply to entity kind %s", key, kind)
		}
		if value == nil {
			return fmt.Errorf("remove field %s instead of storing null", key)
		}
		valid := false
		switch f.Type {
		case "text":
			v, ok := value.(string)
			valid = ok && len([]rune(v)) <= 20000
		case "select":
			v, ok := value.(string)
			valid = ok && slices.Contains(f.Options, v)
		case "date":
			v, ok := value.(string)
			_, err := time.Parse("2006-01-02", v)
			valid = ok && err == nil
		case "boolean":
			_, valid = value.(bool)
		case "number":
			v, err := json.Marshal(value)
			var number float64
			valid = err == nil && json.Unmarshal(v, &number) == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
		}
		if !valid {
			return fmt.Errorf("invalid %s value for field %s", f.Type, key)
		}
	}
	return nil
}

type ManagedEntityInput struct {
	Name        string         `json:"name"`
	Kind        string         `json:"kind"`
	Code        string         `json:"code"`
	Description string         `json:"description"`
	Aliases     []string       `json:"aliases"`
	Attributes  map[string]any `json:"attributes"`
}

type ManagedEntityPatch struct {
	Revision    int64           `json:"revision"`
	Name        *string         `json:"name"`
	Kind        *string         `json:"kind"`
	Code        *string         `json:"code"`
	Description *string         `json:"description"`
	Aliases     *[]string       `json:"aliases"`
	Attributes  *map[string]any `json:"attributes"`
	Archived    *bool           `json:"archived"`
}

type ManagedRelationInput struct {
	Version      int64  `json:"version"`
	SubjectID    string `json:"subject_id"`
	RelationType string `json:"relation_type"`
	ObjectID     string `json:"object_id"`
}
