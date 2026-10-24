DROP TABLE public.graph_pending_sync;
DROP TABLE public.relation_sources;
DROP TABLE public.relation_suppressions;
DROP TABLE public.relation_type_merges;
DROP TABLE public.relations;
DROP TABLE public.library_relation_types;
ALTER TABLE public.entry_processing_outputs DROP COLUMN relations;
ALTER TABLE public.library_content_types DROP COLUMN extract_graph;
ALTER TABLE public.entries DROP COLUMN graph;

DELETE FROM public.task_pending_ops WHERE task_type IN ('graph:sync','graph:check','graph:rebuild');
