BEGIN;

CREATE TABLE public.maintenance_state (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    paused boolean NOT NULL DEFAULT false,
    pause_mode text NOT NULL DEFAULT '' CHECK (pause_mode IN ('', 'drain', 'snapshot')),
    pause_reason text NOT NULL DEFAULT '',
    paused_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    backup_at timestamptz,
    backup_status text NOT NULL DEFAULT 'never' CHECK (backup_status IN ('never', 'running', 'success', 'failed', 'skipped')),
    backup_path text NOT NULL DEFAULT '',
    backup_size_bytes bigint NOT NULL DEFAULT 0 CHECK (backup_size_bytes >= 0),
    backup_error text NOT NULL DEFAULT '',
    drill_at timestamptz,
    drill_status text NOT NULL DEFAULT 'never' CHECK (drill_status IN ('never', 'running', 'success', 'failed', 'skipped')),
    drill_error text NOT NULL DEFAULT '',
    backup_schedule text NOT NULL DEFAULT '0 3 * * 0',
    drill_schedule text NOT NULL DEFAULT '0 4 1 * *',
    schedule_timezone text NOT NULL DEFAULT 'Asia/Shanghai',
    backup_directory text NOT NULL DEFAULT '../reliquary-backups',
    automatic_enabled boolean NOT NULL DEFAULT true,
    backup_due_at timestamptz,
    drill_due_at timestamptz
);

INSERT INTO public.maintenance_state (id) VALUES (true);

COMMENT ON TABLE public.maintenance_state IS 'Singleton maintenance pause, schedule and latest backup and restore drill results; contains no credentials';

COMMIT;
