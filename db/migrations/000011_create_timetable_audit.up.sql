CREATE TABLE timetable_slots (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid NOT NULL REFERENCES schools(id),
    section_id        uuid NOT NULL REFERENCES sections(id),
    subject_id        uuid NOT NULL REFERENCES subjects(id),
    teacher_id        uuid NOT NULL REFERENCES user_profiles(id),
    academic_year_id  uuid NOT NULL REFERENCES academic_years(id),
    day_of_week       int NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
    period_number     int NOT NULL,
    UNIQUE(section_id, day_of_week, period_number, academic_year_id),
    UNIQUE(teacher_id, day_of_week, period_number, academic_year_id)
);
ALTER TABLE timetable_slots ENABLE ROW LEVEL SECURITY;

CREATE TABLE substitutions (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id               uuid NOT NULL REFERENCES schools(id),
    timetable_slot_id       uuid NOT NULL REFERENCES timetable_slots(id),
    date                    date NOT NULL,
    absent_teacher_id       uuid NOT NULL REFERENCES user_profiles(id),
    substitute_teacher_id   uuid REFERENCES user_profiles(id),
    created_by              uuid NOT NULL REFERENCES user_profiles(id),
    created_at              timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE substitutions ENABLE ROW LEVEL SECURITY;

CREATE TABLE audit_log (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id     uuid NOT NULL REFERENCES schools(id),
    actor_id      uuid REFERENCES user_profiles(id),
    action        text NOT NULL,
    table_name    text NOT NULL,
    record_id     uuid NOT NULL,
    metadata      jsonb,
    created_at    timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;

-- Per Database Schema doc: audit_log is written only by the Go service
-- role, never directly by an end-user request.
CREATE POLICY audit_no_client_write ON audit_log
    FOR ALL USING (false) WITH CHECK (false);
