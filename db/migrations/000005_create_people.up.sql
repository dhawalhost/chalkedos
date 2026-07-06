CREATE TABLE students (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id           uuid NOT NULL REFERENCES schools(id),
    section_id          uuid NOT NULL REFERENCES sections(id),
    admission_number    text NOT NULL,
    full_name           text NOT NULL,
    date_of_birth       date,
    gender              text,
    is_active           boolean NOT NULL DEFAULT true,
    withdrawn_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE(school_id, admission_number)
);
ALTER TABLE students ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_students_school_section ON students(school_id, section_id);

CREATE TABLE guardians (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id            uuid NOT NULL REFERENCES schools(id),
    student_id           uuid NOT NULL REFERENCES students(id),
    full_name            text NOT NULL,
    relation             text,
    phone                text NOT NULL,
    phone_verified       boolean NOT NULL DEFAULT false,
    language_pref        text NOT NULL DEFAULT 'hi',
    is_primary_contact   boolean NOT NULL DEFAULT true,
    created_at           timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE guardians ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_guardians_student ON guardians(student_id);

CREATE TABLE teacher_assignments (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid NOT NULL REFERENCES schools(id),
    teacher_id        uuid NOT NULL REFERENCES user_profiles(id),
    section_id        uuid NOT NULL REFERENCES sections(id),
    subject_id        uuid NOT NULL REFERENCES subjects(id),
    academic_year_id  uuid NOT NULL REFERENCES academic_years(id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE(teacher_id, section_id, subject_id, academic_year_id)
);
ALTER TABLE teacher_assignments ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_teacher_assignments_teacher ON teacher_assignments(teacher_id);
CREATE INDEX idx_teacher_assignments_section ON teacher_assignments(section_id);
