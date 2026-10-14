ALTER TABLE public.graph_pending_sync ADD COLUMN completed_at timestamptz;
CREATE INDEX graph_pending_sync_unfinished ON public.graph_pending_sync(space_id,entity_id,created_at,id) WHERE completed_at IS NULL;
