CREATE TABLE ai_generations (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid NOT NULL REFERENCES schools(id),
    teacher_id        uuid NOT NULL REFERENCES user_profiles(id),
    feature           text NOT NULL CHECK (feature IN ('lesson_plan','question_paper','report_card_remark')),
    prompt_version    text NOT NULL DEFAULT 'v1.0',
    input_hash        text NOT NULL,
    input_json        jsonb NOT NULL,
    output_json       jsonb NOT NULL,
    language          text NOT NULL CHECK (language IN ('hi','en')),
    model             text NOT NULL DEFAULT 'claude-sonnet-4-6',
    cost_inr          numeric(10,4) NOT NULL,
    generated_at      timestamptz NOT NULL DEFAULT now(),
    cache_expires_at  timestamptz NOT NULL
);
ALTER TABLE ai_generations ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_ai_generations_cache_lookup ON ai_generations(school_id, input_hash, feature);

CREATE TABLE ai_usage_quota (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id           uuid NOT NULL REFERENCES schools(id),
    academic_year_id    uuid NOT NULL REFERENCES academic_years(id),
    month               date NOT NULL,
    generations_used    int NOT NULL DEFAULT 0,
    generations_limit   int NOT NULL DEFAULT 500,
    UNIQUE(school_id, month)
);
ALTER TABLE ai_usage_quota ENABLE ROW LEVEL SECURITY;
