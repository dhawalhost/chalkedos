CREATE TABLE classes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid NOT NULL REFERENCES schools(id),
    name        text NOT NULL,
    board       text,
    sort_order  int NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE classes ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_classes_school ON classes(school_id);

CREATE TABLE sections (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid NOT NULL REFERENCES schools(id),
    class_id          uuid NOT NULL REFERENCES classes(id),
    name              text NOT NULL,
    class_teacher_id  uuid REFERENCES user_profiles(id),
    created_at        timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE sections ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_sections_school ON sections(school_id);
CREATE INDEX idx_sections_class ON sections(class_id);

CREATE TABLE subjects (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid NOT NULL REFERENCES schools(id),
    name        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE subjects ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_subjects_school ON subjects(school_id);

CREATE TABLE class_subjects (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id   uuid NOT NULL REFERENCES schools(id),
    class_id    uuid NOT NULL REFERENCES classes(id),
    subject_id  uuid NOT NULL REFERENCES subjects(id),
    UNIQUE(class_id, subject_id)
);
ALTER TABLE class_subjects ENABLE ROW LEVEL SECURITY;
