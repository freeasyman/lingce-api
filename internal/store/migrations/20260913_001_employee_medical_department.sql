ALTER TABLE employees
  ADD COLUMN IF NOT EXISTS medical_department_code VARCHAR(64);

ALTER TABLE employees
  DROP CONSTRAINT IF EXISTS employees_medical_department_code_check;

ALTER TABLE employees
  ADD CONSTRAINT employees_medical_department_code_check
  CHECK (
    medical_department_code IS NULL
    OR medical_department_code IN (
      'general_outpatient',
      'internal_medicine',
      'surgery',
      'orthopedics',
      'gynecology',
      'obstetrics',
      'pediatrics',
      'tcm',
      'dental',
      'dermatology',
      'ophthalmology',
      'otolaryngology',
      'rehabilitation',
      'emergency'
    )
  );

CREATE INDEX IF NOT EXISTS idx_employees_medical_department
  ON employees (tenant_id, medical_department_code)
  WHERE deleted_at IS NULL;
