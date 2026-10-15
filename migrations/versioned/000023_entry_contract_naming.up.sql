BEGIN;
ALTER TABLE public.entries RENAME COLUMN related_article_id TO related_entry_id;
ALTER TABLE public.ingest_discards RENAME COLUMN article_id TO entry_id;
ALTER TABLE public.topic_backfill_batches RENAME COLUMN article_count TO entry_count;
ALTER TABLE public.topic_backfill_jobs RENAME COLUMN failed_articles TO failed_entries;
ALTER TABLE public.topic_backfill_batches RENAME CONSTRAINT topic_backfill_batches_article_count_check TO topic_backfill_batches_entry_count_check;
ALTER TABLE public.topic_backfill_jobs RENAME CONSTRAINT topic_backfill_jobs_failed_articles_check TO topic_backfill_jobs_failed_entries_check;
COMMIT;
