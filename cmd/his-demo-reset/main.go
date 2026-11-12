// his-demo-reset 清理指定租户的 HIS 演示业务数据（默认仅今天），保留主数据。
//
// 用法：
//
//	HIS_DSN='postgres://...' go run ./cmd/his-demo-reset              # 统计（不删除）
//	HIS_DSN='postgres://...' go run ./cmd/his-demo-reset -yes         # 执行
//	HIS_DSN='postgres://...' go run ./cmd/his-demo-reset -yes -all    # 清空该租户全部 HIS 业务数据
//	HIS_DSN='postgres://...' go run ./cmd/his-demo-reset -yes -with-schedules
//
// 清理范围：挂号、队列、就诊(encounters, source_type=his)、病历与质检、处方、订单/支付/退费、
// 检查/检验申请、治疗医嘱/执行、文书与签名；可选同时清理当天排班与号源。
// 保留：员工/角色/角色模块、医生档案、科室/诊室、药品/收费项目/套餐、库存、开放接口客户端、编号流水。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	var (
		tenant        = flag.Int64("tenant", 1, "租户 ID")
		date          = flag.String("date", time.Now().Format("2006-01-02"), "日期（YYYY-MM-DD），默认今天")
		all           = flag.Bool("all", false, "清理该租户全部 HIS 业务数据（忽略 -date）")
		withSchedules = flag.Bool("with-schedules", false, "同时清理排班与号源")
		yes           = flag.Bool("yes", false, "确认执行（不加则只统计）")
	)
	flag.Parse()

	dsn := strings.TrimSpace(os.Getenv("HIS_DSN"))
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "缺少 HIS_DSN 环境变量，例如：HIS_DSN='postgres://user:pass@host:5432/lingce_dev?sslmode=disable'")
		os.Exit(1)
	}
	dateFilter := *date
	if *all {
		dateFilter = ""
	} else if _, err := time.Parse("2006-01-02", dateFilter); err != nil {
		fmt.Fprintln(os.Stderr, "-date 需要 YYYY-MM-DD:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接数据库失败:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	enc := `(SELECT id FROM encounters WHERE tenant_id=$1 AND source_type='his' AND ($2='' OR started_at::date=$2::date))`
	reg := `(SELECT id FROM his_registrations WHERE tenant_id=$1 AND ($2='' OR visit_date=$2::date))`
	rx := `(SELECT id FROM his_prescriptions WHERE encounter_id IN ` + enc + `)`
	ord := `(SELECT id FROM his_orders WHERE encounter_id IN ` + enc + `)`
	doc := `(SELECT id FROM his_documents WHERE encounter_id IN ` + enc + `)`
	rec := `(SELECT id FROM emr_records WHERE encounter_id IN ` + enc + `)`
	trt := `(SELECT id FROM his_treatment_orders WHERE encounter_id IN ` + enc + `)`
	sched := `(SELECT id FROM his_schedules WHERE tenant_id=$1 AND ($2='' OR work_date=$2::date))`

	type item struct {
		label string
		count string
		del   string
	}
	items := []item{
		{"文书签名", `SELECT COUNT(*) FROM his_signatures WHERE document_id IN ` + doc, `DELETE FROM his_signatures WHERE document_id IN ` + doc},
		{"文书", `SELECT COUNT(*) FROM his_documents WHERE encounter_id IN ` + enc, `DELETE FROM his_documents WHERE encounter_id IN ` + enc},
		{"治疗执行", `SELECT COUNT(*) FROM his_treatment_executions WHERE treatment_order_id IN ` + trt, `DELETE FROM his_treatment_executions WHERE treatment_order_id IN ` + trt},
		{"治疗医嘱", `SELECT COUNT(*) FROM his_treatment_orders WHERE encounter_id IN ` + enc, `DELETE FROM his_treatment_orders WHERE encounter_id IN ` + enc},
		{"检查检验申请(旧)", `SELECT COUNT(*) FROM his_service_requests WHERE encounter_id IN ` + enc, `DELETE FROM his_service_requests WHERE encounter_id IN ` + enc},
		{"检查报告", `SELECT COUNT(*) FROM his_exam_reports WHERE request_id IN (SELECT id FROM his_exam_requests WHERE encounter_id IN ` + enc + `)`, `DELETE FROM his_exam_reports WHERE request_id IN (SELECT id FROM his_exam_requests WHERE encounter_id IN ` + enc + `)`},
		{"检查申请", `SELECT COUNT(*) FROM his_exam_requests WHERE encounter_id IN ` + enc, `DELETE FROM his_exam_requests WHERE encounter_id IN ` + enc},
		{"检验结果", `SELECT COUNT(*) FROM his_lab_result_items WHERE request_id IN (SELECT id FROM his_lab_requests WHERE encounter_id IN ` + enc + `)`, `DELETE FROM his_lab_result_items WHERE request_id IN (SELECT id FROM his_lab_requests WHERE encounter_id IN ` + enc + `)`},
		{"检验申请", `SELECT COUNT(*) FROM his_lab_requests WHERE encounter_id IN ` + enc, `DELETE FROM his_lab_requests WHERE encounter_id IN ` + enc},
		{"处方明细", `SELECT COUNT(*) FROM his_prescription_items WHERE prescription_id IN ` + rx, `DELETE FROM his_prescription_items WHERE prescription_id IN ` + rx},
		{"处方", `SELECT COUNT(*) FROM his_prescriptions WHERE encounter_id IN ` + enc, `DELETE FROM his_prescriptions WHERE encounter_id IN ` + enc},
		{"退费", `SELECT COUNT(*) FROM his_refunds WHERE order_id IN ` + ord, `DELETE FROM his_refunds WHERE order_id IN ` + ord},
		{"支付", `SELECT COUNT(*) FROM his_payments WHERE order_id IN ` + ord, `DELETE FROM his_payments WHERE order_id IN ` + ord},
		{"订单明细", `SELECT COUNT(*) FROM his_order_items WHERE order_id IN ` + ord, `DELETE FROM his_order_items WHERE order_id IN ` + ord},
		{"收费单", `SELECT COUNT(*) FROM his_orders WHERE encounter_id IN ` + enc, `DELETE FROM his_orders WHERE encounter_id IN ` + enc},
		{"候诊队列", `SELECT COUNT(*) FROM his_queue WHERE registration_id IN ` + reg, `DELETE FROM his_queue WHERE registration_id IN ` + reg},
		{"挂号", `SELECT COUNT(*) FROM his_registrations WHERE id IN ` + reg, `DELETE FROM his_registrations WHERE id IN ` + reg},
		{"号源回滚", `SELECT COUNT(*) FROM his_slots WHERE schedule_id IN ` + sched + ` AND registration_id IS NOT NULL`, `UPDATE his_slots SET status='free', registration_id=NULL WHERE schedule_id IN ` + sched + ` AND registration_id IS NOT NULL`},
		{"质检结果", `SELECT COUNT(*) FROM emr_quality_results WHERE run_id IN (SELECT id FROM emr_quality_runs WHERE record_id IN ` + rec + `)`, `DELETE FROM emr_quality_results WHERE run_id IN (SELECT id FROM emr_quality_runs WHERE record_id IN ` + rec + `)`},
		{"质检批次", `SELECT COUNT(*) FROM emr_quality_runs WHERE record_id IN ` + rec, `DELETE FROM emr_quality_runs WHERE record_id IN ` + rec},
		{"病历评分", `SELECT COUNT(*) FROM emr_record_score_current WHERE record_id IN ` + rec, `DELETE FROM emr_record_score_current WHERE record_id IN ` + rec},
		{"病历", `SELECT COUNT(*) FROM emr_records WHERE encounter_id IN ` + enc, `DELETE FROM emr_records WHERE encounter_id IN ` + enc},
		{"就诊(encounters)", `SELECT COUNT(*) FROM encounters WHERE id IN ` + enc, `DELETE FROM encounters WHERE id IN ` + enc},
	}
	if *withSchedules {
		items = append(items,
			item{"号源", `SELECT COUNT(*) FROM his_slots WHERE schedule_id IN ` + sched, `DELETE FROM his_slots WHERE schedule_id IN ` + sched},
			item{"排班", `SELECT COUNT(*) FROM his_schedules WHERE id IN ` + sched, `DELETE FROM his_schedules WHERE id IN ` + sched},
		)
	}

	fmt.Printf("租户 %d · 日期 %s（all=%v, with-schedules=%v）\n", *tenant, displayDate(dateFilter), *all, *withSchedules)
	blocked := 0
	_ = conn.QueryRow(ctx, `SELECT COUNT(*) FROM recordings WHERE encounter_id IN `+enc, *tenant, dateFilter).Scan(&blocked)
	if blocked > 0 {
		fmt.Printf("⚠️  有 %d 条 recordings 关联这些就诊，删除 encounters 会失败，请先处理。\n", blocked)
	}

	if !*yes {
		for _, it := range items {
			var n int64
			if err := conn.QueryRow(ctx, it.count, *tenant, dateFilter).Scan(&n); err != nil {
				fmt.Fprintf(os.Stderr, "  统计 %s 失败: %v\n", it.label, err)
				continue
			}
			fmt.Printf("  %-16s %d\n", it.label, n)
		}
		fmt.Println("\n以上为统计模式，未删除任何数据。确认执行请加 -yes。")
		return
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "开启事务失败:", err)
		os.Exit(1)
	}
	defer tx.Rollback(ctx)
	for _, it := range items {
		tag, err := tx.Exec(ctx, it.del, *tenant, dateFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "删除 %s 失败，已回滚: %v\n", it.label, err)
			os.Exit(1)
		}
		fmt.Printf("  %-16s 已删除 %d\n", it.label, tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "提交失败:", err)
		os.Exit(1)
	}
	fmt.Println("\n清理完成（事务已提交）。")
}

func displayDate(d string) string {
	if d == "" {
		return "全部"
	}
	return d
}
