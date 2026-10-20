package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required; read it from configs/secrets.local.toml and pass it explicitly")
		os.Exit(2)
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("连接数据库失败:", err)
	}
	defer db.Close()

	fmt.Println("=== 赢客专家验收数据验证 ===")

	// 1. 验证租户
	var tenantID int
	var tenantName string
	err = db.QueryRow("SELECT id, name FROM tenants WHERE id = 9001").Scan(&tenantID, &tenantName)
	if err == sql.ErrNoRows {
		fmt.Println("❌ 租户 9001 不存在")
	} else if err != nil {
		log.Fatal("查询租户失败:", err)
	} else {
		fmt.Printf("✅ 租户: %d - %s\n", tenantID, tenantName)
	}

	// 2. 验证测试账号
	var employeeCount int
	err = db.QueryRow("SELECT COUNT(*) FROM employees WHERE tenant_id = 9001 AND deleted_at IS NULL").Scan(&employeeCount)
	if err != nil {
		log.Fatal("查询员工失败:", err)
	}
	fmt.Printf("✅ 测试账号数量: %d\n", employeeCount)

	// 3. 验证客户
	var customerCount int
	err = db.QueryRow("SELECT COUNT(*) FROM customers WHERE tenant_id = 9001 AND id BETWEEN 6001 AND 6008").Scan(&customerCount)
	if err != nil {
		log.Fatal("查询客户失败:", err)
	}
	fmt.Printf("✅ 验收客户 (6001-6008): %d\n", customerCount)

	// 4. 验证 Encounter
	var encounterCount int
	err = db.QueryRow("SELECT COUNT(*) FROM encounters WHERE tenant_id = 9001 AND id BETWEEN 8001 AND 8008").Scan(&encounterCount)
	if err != nil {
		log.Fatal("查询 Encounter 失败:", err)
	}
	fmt.Printf("✅ 验收 Encounter (8001-8008): %d\n", encounterCount)

	// 5. 验证录音
	var recordingCount int
	err = db.QueryRow("SELECT COUNT(*) FROM recordings WHERE tenant_id = 9001 AND id BETWEEN 7001 AND 7008").Scan(&recordingCount)
	if err != nil {
		log.Fatal("查询录音失败:", err)
	}
	fmt.Printf("✅ 验收录音 (7001-7008): %d\n", recordingCount)

	// 6. 验证录音与 Encounter 关联
	var linkedCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM recordings
		WHERE tenant_id = 9001 AND id BETWEEN 7001 AND 7008
		AND encounter_id IS NOT NULL
	`).Scan(&linkedCount)
	if err != nil {
		log.Fatal("查询录音关联失败:", err)
	}
	fmt.Printf("✅ 录音关联 Encounter: %d/%d\n", linkedCount, recordingCount)

	// 7. 详细列表
	fmt.Println("\n=== E001-E008 详细信息 ===")
	rows, err := db.Query(`
		SELECT r.id, r.file_name, e.name, c.name, r.confirmed_deal_status, r.converted_amount
		FROM recordings r
		LEFT JOIN employees e ON r.employee_id = e.id
		LEFT JOIN customers c ON r.customer_id = c.id
		WHERE r.tenant_id = 9001 AND r.id BETWEEN 7001 AND 7008
		ORDER BY r.id
	`)
	if err != nil {
		log.Fatal("查询详细列表失败:", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var fileName, employeeName sql.NullString
		var customerName, dealStatus sql.NullString
		var amount sql.NullString
		err := rows.Scan(&id, &fileName, &employeeName, &customerName, &dealStatus, &amount)
		if err != nil {
			log.Fatal("读取行失败:", err)
		}

		fileNameStr := fileName.String
		employeeNameStr := employeeName.String
		customerNameStr := "未识别客户"
		if customerName.Valid {
			customerNameStr = customerName.String
		}
		dealStatusStr := "unknown"
		if dealStatus.Valid {
			dealStatusStr = dealStatus.String
		}
		amountStr := "0"
		if amount.Valid {
			amountStr = amount.String
		}
		fmt.Printf("%d | %s | %s | %s | %s | %s元\n", id, fileNameStr, employeeNameStr, customerNameStr, dealStatusStr, amountStr)
	}

	fmt.Println("\n✅ 验证完成！")
}
