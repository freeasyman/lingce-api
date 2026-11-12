package his

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

// 统计概览的时间区间约束：默认最近 7 天（含今天），最长 366 天。
const (
	statsDefaultRangeDays = 7
	statsMaxRangeDays     = 366
)

// ErrInvalidDateRange 表示统计区间不合法（格式 / 顺序 / 长度）。
var ErrInvalidDateRange = errors.New("时间区间不合法：date_from 不能晚于 date_to，且最长 366 天")

// 挂号状态分布固定口径：reserved（预约未到）不计入门诊量。
var statsVisitStatuses = []string{"registered", "visited", "cancelled", "refunded", "no_show"}

// 支付方式固定顺序，保证前端图例稳定；未列出的方式追加在末尾。
var statsPaymentMethodOrder = []string{"wechat", "alipay", "cash", "insurance"}

const statsDateLayout = "2006-01-02"

// StatsKpis 统计概览 KPI。
type StatsKpis struct {
	Visits       int64   `json:"visits"`
	ChargeCount  int64   `json:"charge_count"`
	Revenue      float64 `json:"revenue"`
	RefundAmount float64 `json:"refund_amount"`
	AvgTicket    float64 `json:"avg_ticket"`
	Outstanding  float64 `json:"outstanding"`
}

// StatsDailyRevenue 按日实收（缺失日期补 0）。
type StatsDailyRevenue struct {
	Date    string  `json:"date"`
	Revenue float64 `json:"revenue"`
}

// StatsPaymentMethod 支付方式金额与笔数。
type StatsPaymentMethod struct {
	Method string  `json:"method"`
	Amount float64 `json:"amount"`
	Count  int64   `json:"count"`
}

// StatsTopItem 收费项目 Top N（按收费单行名称汇总）。
type StatsTopItem struct {
	Name     string  `json:"name"`
	Amount   float64 `json:"amount"`
	Quantity float64 `json:"quantity"`
}

// StatsVisitStatus 挂号状态分布。
type StatsVisitStatus struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

// StatsDepartmentVisit 门诊量按科室分布。
type StatsDepartmentVisit struct {
	DepartmentID   *int64 `json:"department_id"`
	DepartmentName string `json:"department_name"`
	Count          int64  `json:"count"`
}

// StatsOverview GET /his/stats/overview 的响应。
type StatsOverview struct {
	DateFrom           string                 `json:"date_from"`
	DateTo             string                 `json:"date_to"`
	Kpis               StatsKpis              `json:"kpis"`
	RevenueByDay       []StatsDailyRevenue    `json:"revenue_by_day"`
	PaymentMethods     []StatsPaymentMethod   `json:"payment_methods"`
	TopItems           []StatsTopItem         `json:"top_items"`
	VisitByStatus      []StatsVisitStatus     `json:"visit_by_status"`
	VisitsByDepartment []StatsDepartmentVisit `json:"visits_by_department"`
}

// resolveStatsRange 解析统计区间：两个都为空取最近 7 天；只给一端时另一端取今天/默认跨度。
func resolveStatsRange(dateFrom, dateTo string, now time.Time) (time.Time, time.Time, error) {
	location := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	dateFrom = strings.TrimSpace(dateFrom)
	dateTo = strings.TrimSpace(dateTo)

	var from, to time.Time
	var err error
	switch {
	case dateFrom == "" && dateTo == "":
		to = today
		from = to.AddDate(0, 0, -(statsDefaultRangeDays - 1))
	case dateFrom == "":
		if to, err = parseStatsDate(dateTo, location); err != nil {
			return time.Time{}, time.Time{}, err
		}
		from = to.AddDate(0, 0, -(statsDefaultRangeDays - 1))
	case dateTo == "":
		if from, err = parseStatsDate(dateFrom, location); err != nil {
			return time.Time{}, time.Time{}, err
		}
		to = today
	default:
		if from, err = parseStatsDate(dateFrom, location); err != nil {
			return time.Time{}, time.Time{}, err
		}
		if to, err = parseStatsDate(dateTo, location); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}

	if from.After(to) {
		return time.Time{}, time.Time{}, ErrInvalidDateRange
	}
	if statsRangeDays(from, to) > statsMaxRangeDays {
		return time.Time{}, time.Time{}, ErrInvalidDateRange
	}
	return from, to, nil
}

func parseStatsDate(value string, location *time.Location) (time.Time, error) {
	parsed, err := time.ParseInLocation(statsDateLayout, value, location)
	if err != nil {
		return time.Time{}, ErrInvalidDateRange
	}
	return parsed, nil
}

// statsRangeDays 返回区间天数（含首尾）。
func statsRangeDays(from, to time.Time) int {
	return int(math.Round(to.Sub(from).Hours()/24)) + 1
}

// fillRevenueByDay 把稀疏的按日实收补零成完整区间，保证图表 X 轴连续。
func fillRevenueByDay(from, to time.Time, values map[string]float64) []StatsDailyRevenue {
	days := statsRangeDays(from, to)
	out := make([]StatsDailyRevenue, 0, days)
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		key := day.Format(statsDateLayout)
		out = append(out, StatsDailyRevenue{Date: key, Revenue: roundMoney(values[key])})
	}
	return out
}

// statsAvgTicket 客单价 = 实收 / 收费笔数（无收费时为 0）。
func statsAvgTicket(revenue float64, chargeCount int64) float64 {
	if chargeCount <= 0 {
		return 0
	}
	return roundMoney(revenue / float64(chargeCount))
}

func roundMoney(value float64) float64 {
	return math.Round(value*100) / 100
}

// normalizeStatsPaymentMethods 固定返回 wechat/alipay/cash/insurance（无数据补 0），其余方式追加。
func normalizeStatsPaymentMethods(rows []StatsPaymentMethod) []StatsPaymentMethod {
	byMethod := make(map[string]StatsPaymentMethod, len(rows))
	extras := make([]string, 0)
	for _, row := range rows {
		byMethod[row.Method] = StatsPaymentMethod{Method: row.Method, Amount: roundMoney(row.Amount), Count: row.Count}
	}
	known := make(map[string]bool, len(statsPaymentMethodOrder))
	out := make([]StatsPaymentMethod, 0, len(statsPaymentMethodOrder)+len(rows))
	for _, method := range statsPaymentMethodOrder {
		known[method] = true
		item, ok := byMethod[method]
		if !ok {
			item = StatsPaymentMethod{Method: method}
		}
		out = append(out, item)
	}
	for method := range byMethod {
		if !known[method] {
			extras = append(extras, method)
		}
	}
	sort.Strings(extras)
	for _, method := range extras {
		out = append(out, byMethod[method])
	}
	return out
}

// normalizeVisitStatuses 固定挂号状态顺序（无数据补 0）；KPI 门诊量与本分布同口径。
func normalizeVisitStatuses(rows []StatsVisitStatus) []StatsVisitStatus {
	byStatus := make(map[string]int64, len(rows))
	for _, row := range rows {
		byStatus[row.Status] = row.Count
	}
	out := make([]StatsVisitStatus, 0, len(statsVisitStatuses))
	for _, status := range statsVisitStatuses {
		out = append(out, StatsVisitStatus{Status: status, Count: byStatus[status]})
	}
	return out
}

// StatsOverview 统计概览：多查询聚合，单请求返回 KPI / 趋势 / 构成。
func (s *Store) StatsOverview(ctx context.Context, tenantID int64, from, to time.Time) (StatsOverview, error) {
	dateFrom := from.Format(statsDateLayout)
	dateTo := to.Format(statsDateLayout)
	result := StatsOverview{
		DateFrom:           dateFrom,
		DateTo:             dateTo,
		PaymentMethods:     normalizeStatsPaymentMethods(nil),
		TopItems:           make([]StatsTopItem, 0),
		VisitByStatus:      normalizeVisitStatuses(nil),
		VisitsByDepartment: make([]StatsDepartmentVisit, 0),
	}

	var kpis StatsKpis
	if err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM his_registrations
		   WHERE tenant_id = $1 AND visit_date BETWEEN $2::date AND $3::date
		     AND status IN ('registered','visited','cancelled','refunded','no_show')),
		  (SELECT COUNT(DISTINCT p.order_id) FROM his_payments p
		   JOIN his_orders o ON o.id = p.order_id AND o.tenant_id = p.tenant_id
		   WHERE p.tenant_id = $1 AND p.status = 'paid' AND p.paid_at::date BETWEEN $2::date AND $3::date),
		  (SELECT COALESCE(SUM(p.amount), 0) FROM his_payments p
		   JOIN his_orders o ON o.id = p.order_id AND o.tenant_id = p.tenant_id
		   WHERE p.tenant_id = $1 AND p.status = 'paid' AND p.paid_at::date BETWEEN $2::date AND $3::date),
		  (SELECT COALESCE(SUM(r.amount), 0) FROM his_refunds r
		   WHERE r.tenant_id = $1 AND r.status = 'done' AND r.created_at::date BETWEEN $2::date AND $3::date),
		  (SELECT COALESCE(SUM(o.payable_total - o.paid_total), 0) FROM his_orders o
		   WHERE o.tenant_id = $1 AND o.status IN ('pending','partial')
		     AND o.created_at::date BETWEEN $2::date AND $3::date)`,
		tenantID, dateFrom, dateTo).
		Scan(&kpis.Visits, &kpis.ChargeCount, &kpis.Revenue, &kpis.RefundAmount, &kpis.Outstanding); err != nil {
		return result, err
	}
	kpis.Revenue = roundMoney(kpis.Revenue)
	kpis.RefundAmount = roundMoney(kpis.RefundAmount)
	kpis.Outstanding = roundMoney(kpis.Outstanding)
	kpis.AvgTicket = statsAvgTicket(kpis.Revenue, kpis.ChargeCount)
	result.Kpis = kpis

	revenueRows, err := s.pool.Query(ctx, `
		SELECT p.paid_at::date::text, COALESCE(SUM(p.amount), 0)
		FROM his_payments p
		JOIN his_orders o ON o.id = p.order_id AND o.tenant_id = p.tenant_id
		WHERE p.tenant_id = $1 AND p.status = 'paid' AND p.paid_at::date BETWEEN $2::date AND $3::date
		GROUP BY 1`, tenantID, dateFrom, dateTo)
	if err != nil {
		return result, err
	}
	revenueByDate := make(map[string]float64)
	for revenueRows.Next() {
		var date string
		var amount float64
		if err := revenueRows.Scan(&date, &amount); err != nil {
			revenueRows.Close()
			return result, err
		}
		revenueByDate[date] = amount
	}
	revenueRows.Close()
	if err := revenueRows.Err(); err != nil {
		return result, err
	}
	result.RevenueByDay = fillRevenueByDay(from, to, revenueByDate)

	methodRows, err := s.pool.Query(ctx, `
		SELECT p.method, COALESCE(SUM(p.amount), 0), COUNT(*)
		FROM his_payments p
		JOIN his_orders o ON o.id = p.order_id AND o.tenant_id = p.tenant_id
		WHERE p.tenant_id = $1 AND p.status = 'paid' AND p.paid_at::date BETWEEN $2::date AND $3::date
		GROUP BY p.method`, tenantID, dateFrom, dateTo)
	if err != nil {
		return result, err
	}
	methodStats := make([]StatsPaymentMethod, 0)
	for methodRows.Next() {
		var item StatsPaymentMethod
		if err := methodRows.Scan(&item.Method, &item.Amount, &item.Count); err != nil {
			methodRows.Close()
			return result, err
		}
		methodStats = append(methodStats, item)
	}
	methodRows.Close()
	if err := methodRows.Err(); err != nil {
		return result, err
	}
	result.PaymentMethods = normalizeStatsPaymentMethods(methodStats)

	itemRows, err := s.pool.Query(ctx, `
		SELECT oi.name, COALESCE(SUM(oi.line_total), 0), COALESCE(SUM(oi.quantity), 0)
		FROM his_order_items oi
		JOIN his_orders o ON o.id = oi.order_id AND o.tenant_id = $1
		WHERE o.tenant_id = $1 AND o.status IN ('partial','paid','refunded')
		  AND o.created_at::date BETWEEN $2::date AND $3::date
		GROUP BY oi.name
		ORDER BY 2 DESC, 1 ASC
		LIMIT 10`, tenantID, dateFrom, dateTo)
	if err != nil {
		return result, err
	}
	for itemRows.Next() {
		var item StatsTopItem
		if err := itemRows.Scan(&item.Name, &item.Amount, &item.Quantity); err != nil {
			itemRows.Close()
			return result, err
		}
		item.Amount = roundMoney(item.Amount)
		result.TopItems = append(result.TopItems, item)
	}
	itemRows.Close()
	if err := itemRows.Err(); err != nil {
		return result, err
	}

	statusRows, err := s.pool.Query(ctx, `
		SELECT status, COUNT(*)
		FROM his_registrations
		WHERE tenant_id = $1 AND visit_date BETWEEN $2::date AND $3::date
		  AND status IN ('registered','visited','cancelled','refunded','no_show')
		GROUP BY status`, tenantID, dateFrom, dateTo)
	if err != nil {
		return result, err
	}
	statusStats := make([]StatsVisitStatus, 0)
	for statusRows.Next() {
		var item StatsVisitStatus
		if err := statusRows.Scan(&item.Status, &item.Count); err != nil {
			statusRows.Close()
			return result, err
		}
		statusStats = append(statusStats, item)
	}
	statusRows.Close()
	if err := statusRows.Err(); err != nil {
		return result, err
	}
	result.VisitByStatus = normalizeVisitStatuses(statusStats)

	deptRows, err := s.pool.Query(ctx, `
		SELECT r.department_id, COALESCE(d.name, '未分科'), COUNT(*)
		FROM his_registrations r
		LEFT JOIN billing_department_dict d ON d.id = r.department_id
		WHERE r.tenant_id = $1 AND r.visit_date BETWEEN $2::date AND $3::date
		  AND r.status IN ('registered','visited','cancelled','refunded','no_show')
		GROUP BY r.department_id, d.name
		ORDER BY 3 DESC, 2 ASC`, tenantID, dateFrom, dateTo)
	if err != nil {
		return result, err
	}
	for deptRows.Next() {
		var item StatsDepartmentVisit
		if err := deptRows.Scan(&item.DepartmentID, &item.DepartmentName, &item.Count); err != nil {
			deptRows.Close()
			return result, err
		}
		result.VisitsByDepartment = append(result.VisitsByDepartment, item)
	}
	deptRows.Close()
	if err := deptRows.Err(); err != nil {
		return result, err
	}

	return result, nil
}

// StatsOverview 统计概览（默认最近 7 天，租户隔离）。
func (s *Service) StatsOverview(ctx context.Context, tenantID int64, dateFrom, dateTo string) (StatsOverview, error) {
	from, to, err := resolveStatsRange(dateFrom, dateTo, time.Now())
	if err != nil {
		return StatsOverview{}, err
	}
	return s.store.StatsOverview(ctx, tenantID, from, to)
}
