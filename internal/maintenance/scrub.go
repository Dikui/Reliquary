package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Scrub operates only on a disposable database clone used for a portable dump.
// The caller must never pass the live application's database.
func Scrub(ctx context.Context, db *gorm.DB) (*Requirements, error) {
	if err := ValidateClassification(ctx, db); err != nil {
		return nil, err
	}
	columns, err := Schema(ctx, db)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, column := range columns {
		present[column.Table] = true
	}
	result := &Requirements{Required: []Requirement{}, PausedModels: []string{}, PausedSources: []string{}}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		needles, err := credentialNeedles(ctx, tx)
		if err != nil {
			return err
		}
		for _, table := range sortedTables() {
			if !present[table] {
				continue
			}
			if table == "schema_migrations" || table == "maintenance_state" {
				continue
			}
			if table == "chatgpt_login_attempts" {
				required, err := scrubLoginAttemptMetadata(ctx, tx, needles)
				if err != nil {
					return err
				}
				result.Required = append(result.Required, required...)
				continue
			}
			var encoded []string
			if err := tx.Raw("SELECT to_jsonb(t)::text FROM public." + quote(table) + " t").Scan(&encoded).Error; err != nil {
				return err
			}
			for _, data := range encoded {
				row, err := jsonRow([]byte(data))
				if err != nil {
					return err
				}
				clean, removed := sanitizeRow(table, row)
				clean = redactCredentials(clean, needles).(map[string]any)
				redacted := false
				for column, value := range clean {
					before, _ := json.Marshal(row[column])
					after, _ := json.Marshal(value)
					if !bytes.Equal(before, after) {
						redacted = true
					}
				}
				if table == "models" && row["source"] == "chatgpt" {
					removed = append(removed, "chatgpt_login")
				}
				if table == "chatgpt_accounts" {
					removed = append(removed, "chatgpt_login")
				}
				if table == "tenant_api_keys" {
					removed = append(removed, "api_key")
				}
				if len(removed) == 0 && !redacted {
					continue
				}
				if table == "models" && len(removed) > 0 {
					markModelRequirements(clean, removed)
				}
				if table == "data_sources" && len(removed) > 0 {
					markSourceRequirements(clean, removed)
				}
				id := fmt.Sprint(row["id"])
				name := fmt.Sprint(clean["name"])
				if name == "<nil>" {
					name = fmt.Sprint(clean["email"])
				}
				if inactiveCredentialTable(table) {
					result.Required = append(result.Required, inactiveRequirement(table, clean))
				} else if len(removed) > 0 {
					result.Required = append(result.Required, Requirement{Kind: table, ID: id, Name: name, Fields: removed, Reason: "enter credentials again in the new environment"})
				}
				updates := map[string]any{}
				for column, value := range clean {
					before, _ := json.Marshal(row[column])
					after, _ := json.Marshal(value)
					if string(before) == string(after) {
						continue
					}
					if strings.HasPrefix(string(before), "{") || strings.HasPrefix(string(before), "[") || column == "value" {
						updates[column] = gorm.Expr("?::jsonb", string(after))
					} else {
						updates[column] = value
					}
				}
				for _, column := range strings.Fields(SecretColumns[table]) {
					if _, ok := row[column]; ok {
						switch table {
						case "auth_tokens", "resource_access_grants":
							updates[column] = excludedCredentialPrefix + id
						case "tenant_api_keys":
							if column == "key_hash" {
								updates[column] = "disabled-" + id
							} else {
								updates[column] = ""
							}
						case "tenants":
							updates[column] = gorm.Expr("'{}'::jsonb")
						default:
							updates[column] = ""
						}
					}
				}
				if len(removed) > 0 {
					switch table {
					case "models":
						updates["status"] = "needs_configuration"
						result.PausedModels = append(result.PausedModels, id)
					case "data_sources":
						updates["status"] = "paused"
						result.PausedSources = append(result.PausedSources, id)
					case "chatgpt_accounts":
						updates["status"] = "needs_login"
						updates["expires_at"] = nil
						updates["earliest_refresh_at"] = nil
					}
				}
				if len(updates) == 0 {
					continue
				}
				query := tx.Table("public." + quote(table))
				if _, ok := row["id"]; ok {
					query = query.Where("id=?", id)
				} else {
					// Composite identities are explicit; never issue a whole-table
					// update while sanitizing task or association metadata.
					switch table {
					case "task_queue_counters":
						query = query.Where("queue=? AND day=?", row["queue"], row["day"])
					case "follow_feed_states":
						query = query.Where("tenant_id=?", row["tenant_id"])
					default:
						return fmt.Errorf("cannot sanitize credential-bearing row without identity in %s", table)
					}
				}
				if err := query.Updates(updates).Error; err != nil {
					return fmt.Errorf("scrub %s: %w", table, err)
				}
			}
		}
		return nil
	})
	return result, err
}

func Models(ctx context.Context, db *gorm.DB) ([]map[string]any, error) {
	var encoded []string
	if err := db.WithContext(ctx).Raw("SELECT to_jsonb(t)::text FROM public.models t WHERE deleted_at IS NULL ORDER BY id").Scan(&encoded).Error; err != nil {
		return nil, err
	}
	models := []map[string]any{}
	for _, data := range encoded {
		row, err := jsonRow([]byte(data))
		if err != nil {
			return nil, err
		}
		clean, _ := sanitizeRow("models", row)
		if parameters, ok := clean["parameters"].(map[string]any); ok {
			for _, key := range []string{"base_url", "dimension", "embedding_parameters", "supports_images"} {
				if value, ok := parameters[key]; ok {
					clean[key] = value
				}
			}
		}
		models = append(models, clean)
	}
	return models, nil
}
