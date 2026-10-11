package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/container"
	"reliquary/internal/database"
	applogger "reliquary/internal/logger"
	"reliquary/internal/maintenance"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types"
	"sort"
)

func maintenanceDatabase() (*gorm.DB, string, error) {
	if driver := strings.TrimSpace(os.Getenv("DB_DRIVER")); driver != "" && driver != "postgres" {
		return nil, "", fmt.Errorf("unsupported database driver %s", driver)
	}
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), User: url.UserPassword(os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD")), Path: "/" + os.Getenv("DB_NAME")}
	query := u.Query()
	query.Set("sslmode", "disable")
	query.Set("connect_timeout", "10")
	u.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	return db, u.String(), err
}

func runMaintenance(args []string, input io.Reader, output io.Writer) error {
	applogger.SetOutput(os.Stderr)
	if len(args) == 0 {
		return errors.New("usage: server maintenance flush|manifest|verify|scrub|models|model-test|credentials|chatgpt-login|chatgpt-complete|secret-scan|reindex|migrate|rebuild|pause|resume|status|record|configure|due")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	db, dsn, err := maintenanceDatabase()
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	if args[0] == "migrate" {
		return runMaintenanceDatabase(ctx, db, dsn, args, input, output)
	}
	return maintenance.WithCurrentNames(db, func(current *gorm.DB) error {
		return runMaintenanceDatabase(ctx, current, dsn, args, input, output)
	})
}

func runMaintenanceDatabase(ctx context.Context, db *gorm.DB, dsn string, args []string, input io.Reader, output io.Writer) error {
	emit := func(value any) error {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
	root := strings.TrimSpace(os.Getenv("VAULT_ROOT"))
	if root == "" {
		root = "/data/vault"
	}
	uploads := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	vault := func() (*service.VaultService, error) {
		return service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: root, HeaderDebounce: 5 * time.Second}})
	}
	flush := func(ctx context.Context) error {
		v, err := vault()
		if err != nil {
			return err
		}
		return v.Flush(ctx)
	}
	switch args[0] {
	case "credentials":
		return runMaintenanceCredentials(ctx, db, args[1:], input, output)
	case "chatgpt-login", "chatgpt-complete":
		return runMaintenanceChatGPT(ctx, db, args, input, output, nil)
	case "secret-scan":
		flags := flag.NewFlagSet("maintenance secret-scan", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		directory := flags.String("directory", "", "package directory to scan without disclosing credential values")
		stdin := flags.Bool("input", false, "scan the decoded PostgreSQL archive from stdin")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || (*stdin && *directory != "") || (!*stdin && *directory == "") {
			return errors.New("secret-scan requires --directory PATH or --input")
		}
		if *stdin {
			if err := maintenance.ScanCredentialReader(ctx, db, input); err != nil {
				return err
			}
		} else {
			if err := maintenance.ScanCredentialFiles(ctx, db, *directory); err != nil {
				return err
			}
		}
		return emit(map[string]bool{"ok": true})
	case "pause", "resume", "status", "record", "configure", "due":
		return maintenanceops.RunCLI(ctx, db, args, output, flush)
	case "flush":
		if err := flush(ctx); err != nil {
			return err
		}
		return emit(map[string]bool{"flushed": true})
	case "manifest":
		manifest, err := maintenance.BuildManifest(ctx, db, root, uploads)
		if err != nil {
			return err
		}
		return emit(manifest)
	case "verify":
		flags := flag.NewFlagSet("maintenance verify", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		repair := flags.Bool("repair", false, "repair database-owned files")
		manifestPath := flags.String("manifest", "", "compare a portable snapshot manifest")
		vectors := flags.Bool("vectors", false, "compare regenerated vector coverage and profiles")
		allowModelChange := flags.Bool("allow-model-change", false, "allow the selected embedding configuration to change after recovery")
		allowOperationLogAppend := flags.Bool("allow-operation-log-append", false, "allow new operation logs while retaining every historical row")
		allowReconfigurationAppend := flags.Bool("allow-reconfiguration-append", false, "allow new login, resource grant, OAuth attempt and operation log metadata while preserving every old record")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if (*allowModelChange || *allowOperationLogAppend || *allowReconfigurationAppend) && *manifestPath == "" {
			return errors.New("verification allowances require --manifest")
		}
		v, err := vault()
		if err != nil {
			return err
		}
		result, err := v.Verify(ctx, *repair)
		if err != nil {
			return err
		}
		if *manifestPath != "" {
			data, err := os.ReadFile(*manifestPath)
			if err != nil {
				return err
			}
			var wanted maintenance.Manifest
			if err := json.Unmarshal(data, &wanted); err != nil {
				return err
			}
			if *allowReconfigurationAppend {
				if err := maintenance.PopulateRecoveryAPIKeyRows(&wanted, filepath.Join(filepath.Dir(*manifestPath), "vault")); err != nil {
					return err
				}
			}
			actual, err := maintenance.BuildManifest(ctx, db, root, uploads)
			if err != nil {
				return err
			}
			result.Differences = append(result.Differences, maintenance.CompareManifestWithSettings(&wanted, actual, maintenance.CompareOptions{AllowModelChange: *allowModelChange, AllowOperationLogAppend: *allowOperationLogAppend, AllowReconfigurationAppend: *allowReconfigurationAppend})...)
			if *vectors && !*allowModelChange {
				result.Differences = append(result.Differences, maintenance.CompareVectors(&wanted, actual)...)
			}
			result.OK = len(result.Differences) == 0
		}
		if *vectors && (*manifestPath == "" || *allowModelChange) {
			for kind, count := range result.PendingVectors {
				if count > 0 {
					result.Differences = append(result.Differences, fmt.Sprintf("%d %s vectors remain pending", count, kind))
				}
			}
			result.OK = len(result.Differences) == 0
		}
		if *vectors {
			differences, err := maintenance.CheckVectorDimensions(ctx, db)
			if err != nil {
				return err
			}
			result.Differences = append(result.Differences, differences...)
			result.OK = len(result.Differences) == 0
		}
		if err := emit(result); err != nil {
			return err
		}
		if !result.OK {
			return fmt.Errorf("verification failed: %d differences; maintenance remains paused", len(result.Differences))
		}
		return nil
	case "scrub":
		result, err := maintenance.Scrub(ctx, db)
		if err != nil {
			return err
		}
		return emit(result)
	case "models":
		models, err := maintenance.Models(ctx, db)
		if err != nil {
			return err
		}
		var kb types.KnowledgeBase
		if err := db.Table("knowledge_bases").Where("deleted_at IS NULL").First(&kb).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		selected := map[string]any{"id": kb.EmbeddingModelID}
		for _, model := range models {
			if model["id"] == kb.EmbeddingModelID {
				selected["profile_hash"] = maintenance.ModelProfile(model)
			}
		}
		ids := map[string]bool{}
		generation := map[string]bool{}
		for _, id := range []string{kb.EmbeddingModelID, kb.SummaryModelID, kb.ModelTiers.Light, kb.ModelTiers.EntityResolution, kb.ModelTiers.Generation, kb.ModelTiers.Embedding, kb.ModelTiers.Rerank} {
			if id != "" {
				ids[id] = true
			}
		}
		for _, id := range []string{kb.SummaryModelID, kb.ModelTiers.Light, kb.ModelTiers.EntityResolution, kb.ModelTiers.Generation} {
			if id != "" {
				generation[id] = true
			}
		}
		var fallback string
		_ = db.Table("chatgpt_policy").Select("fallback_model_id").Scan(&fallback).Error
		if fallback != "" {
			ids[fallback] = true
			generation[fallback] = true
		}
		selectedIDs := []string{}
		for id := range ids {
			selectedIDs = append(selectedIDs, id)
		}
		sort.Strings(selectedIDs)
		generationIDs := []string{}
		for id := range generation {
			generationIDs = append(generationIDs, id)
		}
		sort.Strings(generationIDs)
		return emit(map[string]any{"models": models, "selected_embedding": selected, "selected_model_ids": selectedIDs, "selected_generation_ids": generationIDs})
	case "model-test":
		flags := flag.NewFlagSet("model-test", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		id := flags.String("id", "", "model ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		result, err := container.CheckMaintenanceModels(ctx, db, *id)
		if err != nil {
			return err
		}
		return emit(result)
	case "reindex":
		for _, index := range []string{"chunks_search_idx", "knowledges_summary_search_idx"} {
			if err := db.WithContext(ctx).Exec("REINDEX INDEX public." + index).Error; err != nil {
				return err
			}
		}
		if err := db.WithContext(ctx).Exec("ANALYZE public.knowledges,public.chunks").Error; err != nil {
			return err
		}
		return emit(map[string]bool{"reindexed": true})
	case "migrate":
		if err := database.RunMigrations(dsn); err != nil {
			return err
		}
		return emit(map[string]bool{"migrated": true})
	case "rebuild":
		result, err := container.RebuildVaultWithDatabase(ctx, db)
		if err != nil {
			return err
		}
		return emit(result)
	default:
		return fmt.Errorf("unknown maintenance command %s", args[0])
	}
}
