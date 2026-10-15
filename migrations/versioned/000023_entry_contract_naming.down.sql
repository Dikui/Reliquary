BEGIN;
ALTER TABLE public.topic_backfill_jobs RENAME CONSTRAINT topic_backfill_jobs_failed_entries_check TO topic_backfill_jobs_failed_articles_check;
ALTER TABLE public.topic_backfill_batches RENAME CONSTRAINT topic_backfill_batches_entry_count_check TO topic_backfill_batches_article_count_check;
ALTER TABLE public.topic_backfill_jobs RENAME COLUMN failed_entries TO failed_articles;
ALTER TABLE public.topic_backfill_batches RENAME COLUMN entry_count TO article_count;
ALTER TABLE public.ingest_discards RENAME COLUMN entry_id TO article_id;
ALTER TABLE public.entries RENAME COLUMN related_entry_id TO related_article_id;
COMMIT;
