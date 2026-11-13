// HIS 种子数据工具：为指定租户初始化派特生物（妇科+中医科门诊部）P1 基线数据。
// 用法：go run ./cmd/his-seed -tenant-id <id> [-db-url <dsn>] [-employee-id <id>]
// 幂等：重复执行不产生重复数据（ON CONFLICT / 查重跳过）。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/freeasyman/lingce-api/internal/his"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	tenantID := flag.Int64("tenant-id", 0, "目标租户 ID（必填）")
	dbURL := flag.String("db-url", "", "PostgreSQL DSN（默认用 LINGCE_DEV_DB_URL 环境变量）")
	employeeID := flag.Int64("employee-id", 0, "可选：绑定示例医生的员工 ID")
	flag.Parse()
	if *tenantID <= 0 {
		log.Fatal("tenant-id 必填")
	}
	if *dbURL == "" {
		*dbURL = os.Getenv("LINGCE_DEV_DB_URL")
	}
	if *dbURL == "" {
		log.Fatal("db-url 或 LINGCE_DEV_DB_URL 必填")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer pool.Close()

	// 房间
	rooms := []struct{ name, typ string }{
		{"诊室一", "diagnose"}, {"诊室二", "diagnose"}, {"治疗室一", "treatment"}, {"药房", "pharmacy"},
	}
	for _, r := range rooms {
		_, err := pool.Exec(ctx, `
			INSERT INTO his_rooms (tenant_id, name, type) VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id, name) DO NOTHING`, *tenantID, r.name, r.typ)
		if err != nil {
			log.Fatalf("房间 %s: %v", r.name, err)
		}
	}

	// 药品字典（西药/中成药/中药饮片）
	drugs := []struct {
		code, name, spec, unit, drugType string
		retail, purchase                 float64
	}{
		{"W001", "甲硝唑片", "0.2g*21片", "盒", "western", 6.5, 4.2},
		{"W002", "阿奇霉素片", "0.25g*6片", "盒", "western", 18, 12},
		{"W003", "头孢克肟胶囊", "0.1g*12粒", "盒", "western", 22, 15},
		{"Z001", "保妇康栓", "1.74g*8粒", "盒", "chinese_patent", 28, 19},
		{"Z002", "妇科千金片", "0.32g*144片", "瓶", "chinese_patent", 25, 17},
		{"H001", "当归", "统装", "g", "chinese_herb", 0.5, 0.3},
		{"H002", "川芎", "统装", "g", "chinese_herb", 0.4, 0.25},
		{"H003", "白芍", "统装", "g", "chinese_herb", 0.5, 0.3},
		{"H004", "熟地黄", "统装", "g", "chinese_herb", 0.6, 0.4},
		{"H005", "党参", "统装", "g", "chinese_herb", 0.8, 0.5},
		{"H006", "黄芪", "统装", "g", "chinese_herb", 0.9, 0.6},
		{"H007", "茯苓", "统装", "g", "chinese_herb", 0.5, 0.3},
		{"H008", "白术", "统装", "g", "chinese_herb", 0.7, 0.45},
		{"H009", "甘草", "统装", "g", "chinese_herb", 0.3, 0.2},
		{"H010", "益母草", "统装", "g", "chinese_herb", 0.4, 0.25},
		{"H011", "金银花", "统装", "g", "chinese_herb", 0.8, 0.5},
		{"H012", "连翘", "统装", "g", "chinese_herb", 0.7, 0.45},
	}
	for _, d := range drugs {
		pinyin, initials := his.PinyinOf(d.name)
		_, err := pool.Exec(ctx, `
			INSERT INTO his_drugs (tenant_id, code, name, spec, unit, manufacturer, drug_type,
			                       retail_price, purchase_price, is_rx, pinyin, initials)
			VALUES ($1, $2, $3, $4, $5, '', $6, $7, $8, true, $9, $10)
			ON CONFLICT (tenant_id, code) DO UPDATE
			SET pinyin = EXCLUDED.pinyin, initials = EXCLUDED.initials
			WHERE his_drugs.pinyin = '' OR his_drugs.initials = ''`,
			*tenantID, d.code, d.name, d.spec, d.unit, d.drugType, d.retail, d.purchase, pinyin, initials)
		if err != nil {
			log.Fatalf("药品 %s: %v", d.code, err)
		}
	}

	// 原子收费项目（妇科治疗室常见项目）
	items := []struct {
		code, name, category, unit string
		price                      float64
	}{
		{"C000", "挂号费", "挂号", "次", 10},
		{"C001", "诊查费", "诊查", "次", 20},
		{"C002", "妇科检查", "检查", "次", 50},
		{"C003", "红光治疗", "治疗", "次", 88},
		{"C004", "微波治疗", "治疗", "次", 120},
		{"C005", "阴道冲洗上药", "治疗", "次", 55},
		{"C006", "雾化治疗", "治疗", "次", 45},
		{"C007", "盆底治疗", "治疗", "次", 150},
		{"C008", "白带常规", "检验", "次", 30},
		{"C009", "血常规", "检验", "次", 25},
		{"C010", "妇科B超", "检查", "次", 80},
		{"C011", "中药代煎", "其他", "剂", 3},
	}
	for _, item := range items {
		pinyin, initials := his.PinyinOf(item.name)
		_, err := pool.Exec(ctx, `
			INSERT INTO his_charge_items (tenant_id, code, name, category, unit, price, pinyin, initials)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (tenant_id, code) DO UPDATE
			SET pinyin = EXCLUDED.pinyin, initials = EXCLUDED.initials
			WHERE his_charge_items.pinyin = '' OR his_charge_items.initials = ''`,
			*tenantID, item.code, item.name, item.category, item.unit, item.price, pinyin, initials)
		if err != nil {
			log.Fatalf("收费项目 %s: %v", item.code, err)
		}
	}

	// 打包产品：独立定价（妇科检查50+白带常规30=原子80 → 打包价 68；疗程卡 5 次 440 → 388）
	bundles := []struct {
		code, name string
		price      float64
		itemCodes  []string
	}{
		{"B001", "妇科检查+白带常规套餐", 68, []string{"C002", "C008"}},
		{"B002", "红光治疗疗程卡（5次）", 388, []string{"C003"}},
		{"B003", "冲洗上药+红光组合", 128, []string{"C005", "C003"}},
	}
	for _, b := range bundles {
		var bundleID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO his_bundles (tenant_id, code, name, price, description)
			VALUES ($1, $2, $3, $4, '')
			ON CONFLICT (tenant_id, code) DO UPDATE SET price = his_bundles.price
			RETURNING id`, *tenantID, b.code, b.name, b.price).Scan(&bundleID)
		if err != nil {
			log.Fatalf("打包产品 %s: %v", b.code, err)
		}
		for _, code := range b.itemCodes {
			_, err := pool.Exec(ctx, `
				INSERT INTO his_bundle_items (bundle_id, charge_item_id, quantity)
				SELECT $1, id, CASE WHEN code = 'C003' AND $2 = 'B002' THEN 5 ELSE 1 END
				FROM his_charge_items WHERE tenant_id = $3 AND code = $4
				ON CONFLICT (bundle_id, charge_item_id) DO NOTHING`,
				bundleID, b.code, *tenantID, code)
			if err != nil {
				log.Fatalf("打包组成 %s/%s: %v", b.code, code, err)
			}
		}
	}

	// 医生（可选绑定员工）
	if *employeeID > 0 {
		_, err := pool.Exec(ctx, `
			INSERT INTO his_doctors (tenant_id, employee_id, title, license_no, is_prescribing)
			VALUES ($1, $2, '主治医师', '', true)
			ON CONFLICT (tenant_id, employee_id) DO NOTHING`, *tenantID, *employeeID)
		if err != nil {
			log.Fatalf("医生绑定: %v", err)
		}
	}

	// 病历模板：妇科/中医 × 初诊/复诊（published + is_current，供工作站与听写病历共用）
	templates := []struct {
		code, name, documentType, visitType, specialty, printTitle string
	}{
		{"gyn_first", "妇科初诊病历", "门诊病历", "初诊", "妇科", "妇科门诊初诊病历"},
		{"gyn_follow", "妇科复诊病历", "门诊病历", "复诊", "妇科", "妇科门诊复诊病历"},
		{"tcm_first", "中医初诊病历", "门诊病历", "初诊", "中医", "中医门诊初诊病历"},
		{"tcm_follow", "中医复诊病历", "门诊病历", "复诊", "中医", "中医门诊复诊病历"},
	}
	for _, t := range templates {
		_, err := pool.Exec(ctx, `
			INSERT INTO emr_templates (tenant_id, code, name, version_no, document_type, visit_type,
				specialty, status, print_title, description, is_current)
			SELECT $1, $2, $3, 'v1', $4, $5, $6, 'published', $7, '派特生物种子模板', true
			WHERE NOT EXISTS (
				SELECT 1 FROM emr_templates
				WHERE COALESCE(tenant_id, 0) = COALESCE($1::bigint, 0) AND code = $2::varchar
			)`,
			*tenantID, t.code, t.name, t.documentType, t.visitType, t.specialty, t.printTitle)
		if err != nil {
			log.Fatalf("病历模板 %s: %v", t.code, err)
		}
	}

	// 模板字段：复用听写病历现有字段库（emr_fields），把通用模板的字段组复制给 HIS 专科模板。
	// 数据复用不改 EMR 代码；幂等（按 template_id+field_id 去重）。
	fieldSources := []struct{ source, target string }{
		{"gynecology", "gyn_first"},
		{"gynecology", "gyn_follow"},
		{"tcm", "tcm_first"},
		{"tcm", "tcm_follow"},
	}
	copied := 0
	for _, pair := range fieldSources {
		tag, err := pool.Exec(ctx, `
			INSERT INTO emr_template_fields (tenant_id, template_id, field_id, display_order, is_visible, is_required)
			SELECT tf.tenant_id, tgt.id, tf.field_id, tf.display_order, tf.is_visible, tf.is_required
			FROM emr_template_fields tf
			JOIN emr_templates src ON src.id = tf.template_id AND src.code = $1
			JOIN emr_templates tgt ON tgt.code = $2
			WHERE NOT EXISTS (
				SELECT 1 FROM emr_template_fields existing
				WHERE existing.template_id = tgt.id AND existing.field_id = tf.field_id
			)`, pair.source, pair.target)
		if err != nil {
			log.Fatalf("复制模板字段 %s→%s: %v", pair.source, pair.target, err)
		}
		copied += int(tag.RowsAffected())
	}

	fmt.Printf("种子完成：租户 %d\n  房间 %d 间、药品 %d 种、收费项目 %d 项、打包产品 %d 个、病历模板 %d 套、复制模板字段 %d 条\n",
		*tenantID, len(rooms), len(drugs), len(items), len(bundles), len(templates), copied)
}
