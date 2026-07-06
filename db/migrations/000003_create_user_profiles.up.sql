-- References Supabase's auth.users; in a non-Supabase Postgres this FK
-- should be dropped and identity managed entirely by this table instead.
CREATE TABLE user_profiles (
    id             uuid PRIMARY KEY REFERENCES auth.users(id),
    school_id      uuid NOT NULL REFERENCES schools(id),
    role           text NOT NULL CHECK (role IN ('principal','admin','teacher')),
    full_name      text NOT NULL,
    phone          text NOT NULL UNIQUE,
    language_pref  text NOT NULL DEFAULT 'hi' CHECK (language_pref IN ('hi','en')),
    is_active      boolean NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE user_profiles ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_user_profiles_school ON user_profiles(school_id);
