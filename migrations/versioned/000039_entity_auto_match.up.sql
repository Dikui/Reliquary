ALTER TABLE entities ADD COLUMN auto_match jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(auto_match)='object');
