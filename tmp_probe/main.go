package main

import (
  "context"
  "fmt"

  iauth "github.com/freeasyman/lingce-api/internal/auth"
  istore "github.com/freeasyman/lingce-api/internal/store"
)

func main() {
  ctx := context.Background()
  pool, err := istore.NewPostgresPool(ctx, "postgresql://lince:password@localhost:5432/lince_medical?sslmode=disable")
  if err != nil { panic(err) }
  defer pool.Close()

  s := iauth.NewStore(pool)
  svc := iauth.NewService(s, nil, "test-secret", 24)

  tenant, err := s.GetTenantByID(ctx, 20)
  fmt.Println("GetTenantByID:", tenant, err)

  emp, err := s.GetEmployeeByUsername(ctx, "13011112222", 20)
  if err != nil {
    fmt.Println("GetEmployeeByUsername err:", err)
  } else {
    fmt.Printf("employee: id=%d tenant=%d username=%s phone=%s active=%v hash_len=%d\n", emp.ID, emp.TenantID, emp.Username, emp.Phone, emp.IsActive, len(emp.PasswordHash))
  }

  resp, err := svc.LoginEmployee(ctx, "13011112222", "123456", 20)
  fmt.Printf("LoginEmployee resp_nil=%v err=%v\n", resp == nil, err)
  if resp != nil {
    fmt.Printf("login user_id=%d tenant=%v username=%s\n", resp.UserID, resp.TenantID, resp.Username)
  }
}
