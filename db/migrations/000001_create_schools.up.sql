CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE schools (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                text NOT NULL,
    slug                text NOT NULL UNIQUE,
    board               text NOT NULL,
    city                text,
    state               text,
    phone               text,
    subscription_tier   text NOT NULL DEFAULT 'pilot',
    subscription_status text NOT NULL DEFAULT 'trial',
    pilot_started_at    timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE schools ENABLE ROW LEVEL SECURITY;
