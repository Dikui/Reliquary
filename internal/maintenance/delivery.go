package maintenance

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// PrepareDelivery removes installation identity and log history from an
// already scrubbed, disposable backup clone.
func PrepareDelivery(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		columns, err := Schema(ctx, tx)
		if err != nil {
			return err
		}
		present := map[string]bool{}
		for _, column := range columns {
			present[column.Table] = true
			present[column.Table+"."+column.Name] = true
		}
		for _, table := range []string{
			"chatgpt_login_attempts", "chatgpt_accounts", "chatgpt_hosts",
			"api_keys", "auth_tokens", "resource_access_grants", "operation_logs",
			"model_call_contents", "model_call_content_deletions", "model_calls",
		} {
			if present[table] {
				if err := tx.Exec("DELETE FROM public." + quote(table)).Error; err != nil {
					return fmt.Errorf("prepare delivery: clear %s: %w", table, err)
				}
			}
		}
		// Match the user repository's selection of the first live owner.
		if err := tx.Exec(`DELETE FROM public.users WHERE id IS DISTINCT FROM
			(SELECT id FROM public.users WHERE space_id=1 AND deleted_at IS NULL ORDER BY created_at ASC, id LIMIT 1)`).Error; err != nil {
			return fmt.Errorf("prepare delivery: retain sole user: %w", err)
		}
		if present["entries.ingest_api_key_id"] {
			if err := tx.Exec(`UPDATE public.entries SET ingest_api_key_id=0, ingest_api_key_name='',
				last_ingest_api_key_id=0, last_ingest_api_key_name=''
				WHERE ingest_api_key_id<>0 OR ingest_api_key_name<>'' OR last_ingest_api_key_id<>0 OR last_ingest_api_key_name<>''`).Error; err != nil {
				return fmt.Errorf("prepare delivery: clear entry API key attribution: %w", err)
			}
		}
		// Retain generation model IDs and their references. The maintenance
		// pause set by Scrub is lifted after a customer account is selected.
		if err := tx.Exec(`UPDATE public.models SET parameters=parameters #- '{extra_config,chatgpt_account_id}'
			WHERE parameters #> '{extra_config,chatgpt_account_id}' IS NOT NULL`).Error; err != nil {
			return fmt.Errorf("prepare delivery: detach ChatGPT accounts: %w", err)
		}
		// A fallback must be a local generation model. Keep its selection even
		// if Scrub paused it pending credentials, and clear prior quota usage.
		if err := tx.Exec(`UPDATE public.chatgpt_policy AS p SET daily_used=0, day='', paused=false, reason='', retry_at=NULL,
			fallback_model_id=CASE WHEN EXISTS (
				SELECT 1 FROM public.models WHERE id=p.fallback_model_id AND source='local'
			AND type='KnowledgeQA' AND deleted_at IS NULL
			) THEN p.fallback_model_id ELSE '' END`).Error; err != nil {
			return fmt.Errorf("prepare delivery: reset ChatGPT policy: %w", err)
		}
		return nil
	})
}
