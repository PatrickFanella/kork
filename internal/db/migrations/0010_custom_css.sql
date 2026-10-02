-- Custom CSS per page (Pro). Holds the sanitized, scoped CSS the server
-- produced on save, not the raw input. Pages apply it only while the owner's
-- plan includes custom_css; it is kept when Pro ends.
ALTER TABLE pages ADD COLUMN custom_css text NOT NULL DEFAULT ''
    CHECK (length(custom_css) <= 65536);
