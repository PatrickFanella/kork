-- Optional visibility window for blocks. NULL bounds are open.
ALTER TABLE blocks
    ADD COLUMN starts_at timestamptz,
    ADD COLUMN ends_at   timestamptz,
    ADD CONSTRAINT blocks_schedule_order CHECK (starts_at IS NULL OR ends_at IS NULL OR ends_at > starts_at);
