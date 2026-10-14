DROP INDEX public.graph_pending_sync_unfinished;
ALTER TABLE public.graph_pending_sync DROP COLUMN completed_at;
