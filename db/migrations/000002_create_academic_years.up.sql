CREATE TABLE academic_years (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid NOT NULL REFERENCES schools(id),
    label       text NOT NULL,
    start_date  date NOT NULL,
    end_date    date NOT NULL,
    is_current  boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE academic_years ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_academic_years_school ON academic_years(school_id);
