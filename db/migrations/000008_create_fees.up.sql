CREATE TABLE fee_structures (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid NOT NULL REFERENCES schools(id),
    academic_year_id  uuid NOT NULL REFERENCES academic_years(id),
    class_id          uuid NOT NULL REFERENCES classes(id),
    component         text NOT NULL,
    amount            numeric(10,2) NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE(class_id, academic_year_id, component)
);
ALTER TABLE fee_structures ENABLE ROW LEVEL SECURITY;

CREATE TABLE fee_payments (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid NOT NULL REFERENCES schools(id),
    student_id        uuid NOT NULL REFERENCES students(id),
    academic_year_id  uuid NOT NULL REFERENCES academic_years(id),
    amount            numeric(10,2) NOT NULL,
    payment_mode      text NOT NULL CHECK (payment_mode IN ('cash','upi','cheque','bank_transfer')),
    payment_date      date NOT NULL,
    recorded_by       uuid NOT NULL REFERENCES user_profiles(id),
    note              text,
    created_at        timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE fee_payments ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_fee_payments_school_student ON fee_payments(school_id, student_id);

CREATE TABLE fee_reminders_log (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id             uuid NOT NULL REFERENCES schools(id),
    student_id            uuid NOT NULL REFERENCES students(id),
    whatsapp_message_id   uuid,
    sent_at               timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE fee_reminders_log ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_fee_reminders_student_sent ON fee_reminders_log(student_id, sent_at);
