CREATE TABLE attendance_records (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id    uuid NOT NULL REFERENCES schools(id),
    student_id   uuid NOT NULL REFERENCES students(id),
    section_id   uuid NOT NULL REFERENCES sections(id),
    date         date NOT NULL,
    status       text NOT NULL CHECK (status IN ('present','absent','late','leave')),
    marked_by    uuid NOT NULL REFERENCES user_profiles(id),
    marked_at    timestamptz NOT NULL DEFAULT now(),
    edited_at    timestamptz,
    edited_by    uuid REFERENCES user_profiles(id),
    UNIQUE(student_id, date)
);
ALTER TABLE attendance_records ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_attendance_school_date ON attendance_records(school_id, date);
