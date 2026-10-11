package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

type maintenanceCredentialInput struct {
	APIKey             *string           `json:"api_key,omitempty"`
	AppSecret          *string           `json:"app_secret,omitempty"`
	BaseURL            *string           `json:"base_url,omitempty"`
	CustomHeaders      map[string]string `json:"custom_headers,omitempty"`
	ExtraConfig        map[string]string `json:"extra_config,omitempty"`
	Credentials        map[string]string `json:"credentials,omitempty"`
	ParserEngineConfig map[string]string `json:"parser_engine_config,omitempty"`
}

// Credentials are read only from stdin or an owner-only file. The result never
// contains values, and this command does not create tables or start workers.
func runMaintenanceCredentials(ctx context.Context, db *gorm.DB, args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("maintenance credentials", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	kind := flags.String("kind", "model", "model, data-source or tenant")
	id := flags.String("id", "", "existing configuration ID")
	path := flags.String("input", "-", "owner-only JSON file or - for stdin")
	if err := flags.Parse(args); err != nil {
		return errors.New("invalid credential command options")
	}
	if *id == "" || flags.NArg() != 0 || (*kind != "model" && *kind != "data-source" && *kind != "tenant") {
		return errors.New("credentials requires --kind model|data-source|tenant --id ID and JSON input")
	}
	if utils.GetAESKey() == nil {
		return errors.New("credentials requires a new 32-byte SYSTEM_AES_KEY; values were not changed")
	}
	if *path != "-" {
		file, err := os.Open(*path)
		if err != nil {
			return errors.New("cannot open credential input file")
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("credential input file must be a regular owner-only file, such as mode 0600")
		}
		input = file
	}
	var request maintenanceCredentialInput
	data, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("credential JSON must be readable and no larger than 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return errors.New("invalid credential JSON; values were not changed")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("credential input must contain exactly one JSON object")
	}
	status := ""
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if *kind == "tenant" {
			if request.APIKey != nil || request.AppSecret != nil || request.BaseURL != nil || len(request.CustomHeaders) != 0 || len(request.ExtraConfig) != 0 || len(request.Credentials) != 0 || len(request.ParserEngineConfig) != 1 {
				return errors.New("tenant input requires only parser_engine_config.mineru_api_key")
			}
			value, ok := request.ParserEngineConfig["mineru_api_key"]
			if !ok || strings.TrimSpace(value) == "" || value == types.RedactedSecretPlaceholder {
				return errors.New("tenant parser credential must be a nonempty new mineru_api_key")
			}
			var tenant struct {
				Config types.JSON `gorm:"column:parser_engine_config"`
				Status string
			}
			if err := tx.Table("tenants").Select("parser_engine_config,status").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at IS NULL", *id).Take(&tenant).Error; err != nil {
				return errors.New("tenant not found or unavailable")
			}
			config := map[string]any{}
			if len(tenant.Config) != 0 && string(tenant.Config) != "null" {
				if err := json.Unmarshal(tenant.Config, &config); err != nil || config == nil {
					return errors.New("tenant parser configuration is invalid")
				}
			}
			// The existing parser adapter expects this field in its legacy
			// format. Only this secret changes; all public JSON and profile
			// timestamps stay exact. Export always excludes this field.
			config["mineru_api_key"] = value
			encoded, err := json.Marshal(config)
			if err != nil {
				return errors.New("cannot encode tenant parser configuration")
			}
			if err := tx.Table("tenants").Where("id=?", *id).UpdateColumn("parser_engine_config", gorm.Expr("?::jsonb", string(encoded))).Error; err != nil {
				return errors.New("cannot save tenant parser credential; values were not changed")
			}
			status = tenant.Status
			return nil
		}
		if len(request.ParserEngineConfig) != 0 {
			return errors.New("parser_engine_config requires --kind tenant")
		}
		if *kind == "model" {
			if len(request.Credentials) != 0 {
				return errors.New("model credentials must use api_key, app_secret, base_url, custom_headers or extra_config")
			}
			var model types.Model
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at IS NULL", *id).Take(&model).Error; err != nil {
				return errors.New("model not found or unavailable")
			}
			if model.Source == types.ModelSourceChatGPT {
				return errors.New("ChatGPT requires a new sign-in; token JSON is not accepted and the legacy application must remain offline")
			}
			if request.APIKey != nil {
				model.Parameters.APIKey = *request.APIKey
			}
			if request.AppSecret != nil {
				model.Parameters.AppSecret = *request.AppSecret
			}
			if request.BaseURL != nil {
				model.Parameters.BaseURL = *request.BaseURL
			}
			if model.Parameters.CustomHeaders == nil {
				model.Parameters.CustomHeaders = map[string]string{}
			}
			for name, value := range request.CustomHeaders {
				model.Parameters.CustomHeaders[name] = value
			}
			if model.Parameters.ExtraConfig == nil {
				model.Parameters.ExtraConfig = map[string]string{}
			}
			for name, value := range request.ExtraConfig {
				if strings.HasPrefix(name, "maintenance_") {
					return errors.New("maintenance provenance cannot be changed by credential input")
				}
				model.Parameters.ExtraConfig[name] = value
			}
			if _, err := service.CompleteMaintenanceModelState(ctx, tx, &model); err != nil {
				return errors.New("cannot check model credential completion")
			}
			// ModelParameters.Value uses the current AES key for APIKey and
			// AppSecret. Custom headers retain the legacy model storage format.
			if err := tx.Model(&types.Model{}).Where("id=?", model.ID).Updates(map[string]any{"parameters": model.Parameters, "status": model.Status, "updated_at": time.Now().UTC()}).Error; err != nil {
				return errors.New("cannot save model credentials; values were not changed")
			}
			status = string(model.Status)
			return nil
		}
		if request.APIKey != nil || request.AppSecret != nil || request.BaseURL != nil || len(request.CustomHeaders) != 0 || len(request.ExtraConfig) != 0 || len(request.Credentials) == 0 {
			return errors.New("data-source input requires a credentials object of string values")
		}
		var source types.DataSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at IS NULL", *id).Take(&source).Error; err != nil {
			return errors.New("data source not found or unavailable")
		}
		var config map[string]any
		if err := json.Unmarshal(source.Config, &config); err != nil || config == nil {
			return errors.New("data source configuration is invalid")
		}
		credentials, ok := config["credentials"].(map[string]any)
		if !ok {
			credentials = map[string]any{}
		}
		for name, value := range request.Credentials {
			if value == "" {
				return errors.New("data-source credential values must be nonempty")
			}
			encrypted, err := utils.EncryptAESGCM(value, utils.GetAESKey())
			if err != nil {
				return errors.New("cannot encrypt data-source credentials")
			}
			credentials[name] = encrypted
		}
		config["credentials"] = credentials
		encoded, err := json.Marshal(config)
		if err != nil {
			return errors.New("cannot encode data-source configuration")
		}
		if err := tx.Model(&types.DataSource{}).Where("id=?", source.ID).Updates(map[string]any{"config": types.JSON(encoded), "updated_at": time.Now().UTC()}).Error; err != nil {
			return errors.New("cannot save data-source credentials; values were not changed")
		}
		status = source.Status
		return nil
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]any{"kind": *kind, "id": *id, "status": status, "saved": true, "note": fmt.Sprintf("%s credentials saved; run setup-models and verify before resuming", *kind)})
}
