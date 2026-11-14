-- 019：员工多部门 + 科室拍平（Phase 0 · 部门拍平）
--
-- 目标：
--   1) 建 employee_departments 多对多表（一人多部门）。
--   2) 把现有 employees.department_id 迁入关联表。
--   3) 把 employees.medical_department_code 对应到 clinical 科室部门并关联。
--   4) 拍平：clinical 科室不再挂「医疗部」父级。

-- 1) 多对多表
CREATE TABLE IF NOT EXISTS employee_departments (
    id            bigserial PRIMARY KEY,
    tenant_id     bigint      NOT NULL,
    employee_id   bigint      NOT NULL REFERENCES employees(id),
    department_id bigint      NOT NULL REFERENCES departments(id),
    is_primary    boolean     NOT NULL DEFAULT FALSE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_employee_departments UNIQUE (tenant_id, employee_id, department_id)
);

CREATE INDEX IF NOT EXISTS idx_employee_departments_employee
    ON employee_departments (employee_id) WHERE tenant_id IS NOT NULL;

-- 2) 迁移现有单部门 → 多对多（作为主部门）
INSERT INTO employee_departments (tenant_id, employee_id, department_id, is_primary)
SELECT e.tenant_id, e.id, e.department_id, TRUE
FROM employees e
WHERE e.department_id IS NOT NULL
  AND e.deleted_at IS NULL
ON CONFLICT (tenant_id, employee_id, department_id) DO NOTHING;

-- 3) medical_department_code → clinical 科室部门关联（通过字典映射 code）
INSERT INTO employee_departments (tenant_id, employee_id, department_id, is_primary)
SELECT e.tenant_id, e.id, d.id, FALSE
FROM employees e
JOIN clinical_department_dict cdd ON cdd.code = e.medical_department_code
JOIN departments d
  ON d.tenant_id = e.tenant_id
 AND d.type = 'clinical'
 AND d.deleted_at IS NULL
 AND (d.code = cdd.his_code OR d.code = cdd.code)
WHERE e.medical_department_code IS NOT NULL
  AND e.medical_department_code <> ''
  AND e.deleted_at IS NULL
ON CONFLICT (tenant_id, employee_id, department_id) DO NOTHING;

-- 4) 拍平：clinical 科室不再挂父级
UPDATE departments
SET parent_id = NULL
WHERE type = 'clinical' AND parent_id IS NOT NULL AND deleted_at IS NULL;
