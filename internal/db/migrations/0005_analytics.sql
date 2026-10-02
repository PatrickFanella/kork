-- Aggregate visit counts by referrer domain and by country. Rows hold only a
-- page, a day, a label and a count: no IPs, cookies or user agents.
CREATE TABLE stats_referrers (
    page_id  uuid NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    day      date NOT NULL,
    referrer text NOT NULL CHECK (length(referrer) <= 100),
    count    bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (page_id, day, referrer)
);
CREATE INDEX stats_referrers_day_idx ON stats_referrers (day);

CREATE TABLE stats_countries (
    page_id uuid NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    day     date NOT NULL,
    country text NOT NULL CHECK (length(country) <= 7),
    count   bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (page_id, day, country)
);
CREATE INDEX stats_countries_day_idx ON stats_countries (day);

CREATE INDEX stats_daily_day_idx ON stats_daily (day);

-- Weekly stats email: opt-in flag and the Monday of the last week a summary
-- was sent (or the week it was switched on), so each week sends at most once.
ALTER TABLE users
    ADD COLUMN weekly_stats_email     boolean NOT NULL DEFAULT false,
    ADD COLUMN weekly_stats_sent_week date;

-- Server-side keys, such as the one signing unsubscribe links.
CREATE TABLE app_keys (
    name       text PRIMARY KEY,
    key        bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
