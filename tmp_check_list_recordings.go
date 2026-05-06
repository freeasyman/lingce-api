package main

import (
  "context"
  "fmt"
  "github.com/freeasyman/lingce-api/internal/store"
)

func run(pool interface{ QueryRow(context.Context, string, ...any) any }, q string, args ...any) int {
  var c int
  // pgxpool.Pool has QueryRow returning Row with Scan
  row := pool.QueryRow(context.Background(), q, args...)
  _ = row.Scan(&c)
  return c
}

func main() {
  ctx := context.Background()
  pool, err := store.NewPostgresPool(ctx, "postgresql://lince:password@localhost:5432/lince_medical?sslmode=disable")
  if err != nil { panic(err) }
  defer pool.Close()

  qBase := `SELECT COUNT(*) FROM recordings r
WHERE r.tenant_id=$1 AND r.employee_id=$2
AND EXISTS (SELECT 1 FROM inst_employee_roles ier WHERE ier.tenant_id=r.tenant_id AND ier.employee_id=r.employee_id AND lower(ier.role_code)=ANY($3))
AND r.created_at >= $4 AND r.created_at <= $5
AND COALESCE(r.duration,0) >= 60`

  qRegex := qBase + `
AND (CASE
  WHEN COALESCE(r.analysis_result->>'segue_percent','') ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN (r.analysis_result->>'segue_percent')::double precision
  ELSE NULL END) >= $6
AND (CASE
  WHEN COALESCE(r.analysis_result->>'segue_percent','') ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN (r.analysis_result->>'segue_percent')::double precision
  ELSE NULL END) < $7`

  qNoRegex := qBase + `
AND NULLIF(COALESCE(r.analysis_result->>'segue_percent',''),'')::double precision >= $6
AND NULLIF(COALESCE(r.analysis_result->>'segue_percent',''),'')::double precision < $7`

  argsBase := []any{1, int64(4), []string{"doctor", "therapist", "doctor_assistant"}, "2026-04-18 00:00:00", "2026-04-18 23:59:59"}
  var c int
  if err := pool.QueryRow(ctx, qBase, argsBase...).Scan(&c); err != nil { panic(err) }
  fmt.Printf("base=%d\n", c)

  argsRange := []any{1, int64(4), []string{"doctor", "therapist", "doctor_assistant"}, "2026-04-18 00:00:00", "2026-04-18 23:59:59", 20.0, 30.0}
  if err := pool.QueryRow(ctx, qRegex, argsRange...).Scan(&c); err != nil { panic(err) }
  fmt.Printf("regex=%d\n", c)

  if err := pool.QueryRow(ctx, qNoRegex, argsRange...).Scan(&c); err != nil { panic(err) }
  fmt.Printf("no_regex=%d\n", c)
}
