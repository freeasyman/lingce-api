package content

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FreeCheckupLead 是一条免费体检线索（独立产品，不挂真实机构）。
type FreeCheckupLead struct {
	ID         int64     `json:"id"`
	RunID      int64     `json:"run_id"`
	OrgName    string    `json:"org_name"`
	Region     string    `json:"region"`
	Question   string    `json:"question"`
	DoctorName string    `json:"doctor_name"`
	Phone      string    `json:"phone"`
	ContactName  string  `json:"contact_name"`
	ContactTitle string  `json:"contact_title"`
	Scene      string    `json:"scene"`
	Status     string    `json:"status"`
	Note       string    `json:"note"`
	DeviceID   string    `json:"device_id"`
	IP         string    `json:"ip"`
	IPCity     string    `json:"ip_city"`
	TenantID   int64     `json:"tenant_id"`
	ConvertedAt *time.Time `json:"converted_at"`
	CampaignID  int64     `json:"campaign_id"`
	ChannelID   int64     `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	SalesID     int64     `json:"sales_id"`
	SalesName   string    `json:"sales_name"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	// 关联 run 的摘要（列表页用）
	RunStatus string `json:"run_status,omitempty"`
	Progress  int    `json:"progress,omitempty"`
	Total     int    `json:"total,omitempty"`
}

const freeCheckupLeadSelect = `
	SELECT l.id, l.run_id, l.org_name, l.region, l.question, l.doctor_name, l.phone, l.scene, l.status, l.note, l.created_at, l.updated_at, l.ip, l.ip_city, l.tenant_id, l.converted_at, l.contact_name, l.contact_title, l.campaign_id, l.channel_id, l.channel_name, l.sales_id, l.sales_name,
	       COALESCE(r.status,''), COALESCE(r.progress,0), COALESCE(r.total,0)
	FROM free_checkup_leads l
	LEFT JOIN content_ai_visibility_runs r ON r.id = l.run_id AND r.tenant_id = $1`

func (s *Store) InsertFreeCheckupLead(ctx context.Context, l *FreeCheckupLead) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO free_checkup_leads (run_id, org_name, region, question, doctor_name, phone, scene, status, note, device_id, ip, ip_city, user_agent, contact_name, contact_title, campaign_id, channel_id, channel_name, sales_id, sales_name)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20) RETURNING id`,
		l.RunID, l.OrgName, l.Region, l.Question, l.DoctorName, l.Phone, l.Scene, l.Status, l.Note, l.DeviceID, l.IP, l.IPCity, l.UserAgent, l.ContactName, l.ContactTitle, l.CampaignID, l.ChannelID, l.ChannelName, l.SalesID, l.SalesName).Scan(&id)
	return id, err
}

// ListFreeCheckupLeads 运营控制台列表：按状态筛选、分页，附带 run 摘要。
func (s *Store) ListFreeCheckupLeads(ctx context.Context, status, q string, limit, offset int) ([]FreeCheckupLead, int, error) {
	conds := []string{}
	args := []any{PublicCheckupTenantID}
	if status != "" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("l.status = $%d", len(args)))
	}
	if strings.TrimSpace(q) != "" {
		args = append(args, "%"+strings.TrimSpace(q)+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf("(l.org_name ILIKE $%d OR l.phone ILIKE $%d OR l.contact_name ILIKE $%d OR l.question ILIKE $%d)", n, n, n, n))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	countArgs := []any{}
	countWhere := ""
	if status != "" {
		countArgs = append(countArgs, status)
		countWhere = " WHERE l.status = $1"
	}
	if strings.TrimSpace(q) != "" {
		countArgs = append(countArgs, "%"+strings.TrimSpace(q)+"%")
		n := len(countArgs)
		cond := fmt.Sprintf("(l.org_name ILIKE $%d OR l.phone ILIKE $%d OR l.contact_name ILIKE $%d OR l.question ILIKE $%d)", n, n, n, n)
		if countWhere == "" {
			countWhere = " WHERE " + cond
		} else {
			countWhere += " AND " + cond
		}
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM free_checkup_leads l"+countWhere, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := freeCheckupLeadSelect + where + fmt.Sprintf(" ORDER BY l.created_at DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rs, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rs.Close()
	var rows []FreeCheckupLead
	for rs.Next() {
		var l FreeCheckupLead
		if err := rs.Scan(&l.ID, &l.RunID, &l.OrgName, &l.Region, &l.Question, &l.DoctorName, &l.Phone, &l.Scene, &l.Status, &l.Note, &l.CreatedAt, &l.UpdatedAt, &l.IP, &l.IPCity, &l.TenantID, &l.ConvertedAt, &l.ContactName, &l.ContactTitle, &l.CampaignID, &l.ChannelID, &l.ChannelName, &l.SalesID, &l.SalesName, &l.RunStatus, &l.Progress, &l.Total); err != nil {
			return nil, 0, err
		}
		rows = append(rows, l)
	}
	return rows, total, rs.Err()
}

func (s *Store) GetFreeCheckupLead(ctx context.Context, id int64) (*FreeCheckupLead, error) {
	var l FreeCheckupLead
	err := s.pool.QueryRow(ctx, freeCheckupLeadSelect+` WHERE l.id = $2`, PublicCheckupTenantID, id).
		Scan(&l.ID, &l.RunID, &l.OrgName, &l.Region, &l.Question, &l.DoctorName, &l.Phone, &l.Scene, &l.Status, &l.Note, &l.CreatedAt, &l.UpdatedAt, &l.IP, &l.IPCity, &l.TenantID, &l.ConvertedAt, &l.ContactName, &l.ContactTitle, &l.CampaignID, &l.ChannelID, &l.ChannelName, &l.SalesID, &l.SalesName, &l.RunStatus, &l.Progress, &l.Total)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// FreeCheckupFunnel 销售链路漏斗：扫码/体检/线索/成交/缓存命中（今日、近7天、累计）。
type FreeCheckupFunnel struct {
	Scans    int `json:"scans"`
	Checks   int `json:"checks"`
	Leads    int `json:"leads"`
	Converted int `json:"converted"`
	Cached   int `json:"cached"`
}

func (s *Store) FreeCheckupFunnel(ctx context.Context) (today, last7d, total FreeCheckupFunnel, err error) {
	todayStart := "date_trunc('day', now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'"
	last7Cond := "now() - interval '7 days'"
	type q struct{ scans, checks, leads, converted, cached string }
	queries := map[string]q{
		"today": {scans: "created_at >= " + todayStart, checks: "created_at >= " + todayStart, leads: "created_at >= " + todayStart, converted: "created_at >= " + todayStart, cached: "created_at >= " + todayStart},
		"last7": {scans: "created_at >= " + last7Cond, checks: "created_at >= " + last7Cond, leads: "created_at >= " + last7Cond, converted: "created_at >= " + last7Cond, cached: "created_at >= " + last7Cond},
	}
	for name, w := range queries {
		f := FreeCheckupFunnel{}
		_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_scans WHERE `+w.scans).Scan(&f.Scans)
		_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_usage WHERE `+w.checks).Scan(&f.Checks)
		_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_leads WHERE `+w.leads).Scan(&f.Leads)
		_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_leads WHERE status='converted' AND `+w.converted).Scan(&f.Converted)
		_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_usage WHERE cached=true AND `+w.cached).Scan(&f.Cached)
		switch name {
		case "today":
			today = f
		case "last7":
			last7d = f
		}
	}
	// 累计（无时间条件）
	total = FreeCheckupFunnel{}
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_scans`).Scan(&total.Scans)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_usage`).Scan(&total.Checks)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_leads`).Scan(&total.Leads)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_leads WHERE status='converted'`).Scan(&total.Converted)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_usage WHERE cached=true`).Scan(&total.Cached)
	return today, last7d, total, nil
}

// GetFreeCheckupLeadByTenantID 机构档案"来源"：按 tenant_id 反查转化线索（最近一条）。
func (s *Store) GetFreeCheckupLeadByTenantID(ctx context.Context, tenantID int64) (*FreeCheckupLead, error) {
	row := s.pool.QueryRow(ctx, freeCheckupLeadSelect+" WHERE l.tenant_id=$2 ORDER BY l.converted_at DESC NULLS LAST LIMIT 1", PublicCheckupTenantID, tenantID)
	var l FreeCheckupLead
	err := row.Scan(&l.ID, &l.RunID, &l.OrgName, &l.Region, &l.Question, &l.DoctorName, &l.Phone, &l.Scene, &l.Status, &l.Note, &l.CreatedAt, &l.UpdatedAt, &l.IP, &l.IPCity, &l.TenantID, &l.ConvertedAt, &l.ContactName, &l.ContactTitle, &l.CampaignID, &l.ChannelID, &l.ChannelName, &l.SalesID, &l.SalesName, &l.RunStatus, &l.Progress, &l.Total)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, err
	}
	return &l, nil
}

// ConvertFreeCheckupLead 创建机构成功后回写：记 tenant_id、置 converted、记转化时间。
// 已转化过的线索不会重复写（tenant_id 已非 0 时返回 false）。
func (s *Store) ConvertFreeCheckupLead(ctx context.Context, leadID, tenantID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE free_checkup_leads
		SET tenant_id=$2, status='converted', converted_at=now(), updated_at=now()
		WHERE id=$1 AND tenant_id=0`, leadID, tenantID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) UpdateFreeCheckupLeadStatus(ctx context.Context, id int64, status, note string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE free_checkup_leads SET status=$1, note=$2, updated_at=NOW() WHERE id=$3`, status, note, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// InsertFreeCheckupUsage 记录一次体检使用（无手机号也记，用于成本与使用量统计）。
func (s *Store) InsertFreeCheckupUsage(ctx context.Context, runID int64, sid, deviceID, ip, ipCity, ua, scene, orgName, region, question string, cached bool, campaignID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO free_checkup_usage (run_id, sid, device_id, ip, ip_city, user_agent, scene, org_name, region, question, cached, campaign_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		runID, sid, deviceID, ip, ipCity, ua, scene, orgName, region, question, cached, campaignID)
	return err
}

// FindFreeCheckupCachedRun 24 小时内、同机构+同问题+同地区的已完成任务，直接复用。
func (s *Store) FindFreeCheckupCachedRun(ctx context.Context, orgName, region, question string) (int64, bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM content_ai_visibility_runs
		WHERE tenant_id=$1 AND status='done'
		  AND org_name=$2 AND region=$3 AND question=$4
		  AND created_at > now() - interval '24 hours'
		ORDER BY id DESC LIMIT 1`, PublicCheckupTenantID, orgName, region, question).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return 0, false, nil
		}
		return 0, false, err
	}
	return id, true, nil
}

// FreeCheckupQuotaUsage 今日已用次数：按 sid / 设备号 / IP 三种口径（Asia/Shanghai 自然日）。
func (s *Store) FreeCheckupQuotaUsage(ctx context.Context, sid, deviceID, ip string) (bySID, byDevice, byIP int, err error) {
	dayStart := "date_trunc('day', now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'"
	if sid != "" {
		if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_usage WHERE sid=$1 AND created_at >= `+dayStart, sid).Scan(&bySID); err != nil {
			return
		}
	}
	if deviceID != "" {
		if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_usage WHERE device_id=$1 AND created_at >= `+dayStart, deviceID).Scan(&byDevice); err != nil {
			return
		}
	}
	if ip != "" {
		if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_usage WHERE ip=$1 AND created_at >= `+dayStart, ip).Scan(&byIP); err != nil {
			return
		}
	}
	return
}

type FreeCheckupUsage struct {
	ID        int64     `json:"id"`
	RunID     int64     `json:"run_id"`
	DeviceID  string    `json:"device_id"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Scene     string    `json:"scene"`
	OrgName   string    `json:"org_name"`
	Region    string    `json:"region"`
	Question  string    `json:"question"`
	CreatedAt time.Time `json:"created_at"`
	RunStatus string    `json:"run_status"`
	HasLead      bool   `json:"has_lead"`
	Phone        string `json:"phone"`
	IPCity       string `json:"ip_city"`
	TenantID     int64  `json:"tenant_id"`
	ConvertedAt  *time.Time `json:"converted_at"`
	ContactName  string `json:"contact_name"`
	ContactTitle string `json:"contact_title"`
}

// ListFreeCheckupUsage 运营控制台：体检使用记录（含未留手机号）。
func (s *Store) ListFreeCheckupUsage(ctx context.Context, leadFilter, q string, limit, offset int) ([]FreeCheckupUsage, int, error) {
	conds := []string{}
	switch leadFilter {
	case "lead":
		conds = append(conds, "l.id IS NOT NULL")
	case "nolead":
		conds = append(conds, "l.id IS NULL")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	countSQL := `SELECT count(*) FROM free_checkup_usage u LEFT JOIN free_checkup_leads l ON l.run_id = u.run_id` + where
	if err := s.pool.QueryRow(ctx, countSQL).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.run_id, u.device_id, u.ip, u.ip_city, u.user_agent, u.scene, u.org_name, u.region, u.question, u.created_at,
		       COALESCE(r.status,''), (l.id IS NOT NULL), COALESCE(l.phone,''), COALESCE(l.contact_name,''), COALESCE(l.contact_title,'')
		FROM free_checkup_usage u
		LEFT JOIN content_ai_visibility_runs r ON r.id = u.run_id AND r.tenant_id = $3
		LEFT JOIN free_checkup_leads l ON l.run_id = u.run_id`+where+`
		ORDER BY u.created_at DESC LIMIT $1 OFFSET $2`, limit, offset, PublicCheckupTenantID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []FreeCheckupUsage
	for rows.Next() {
		var u FreeCheckupUsage
		if err := rows.Scan(&u.ID, &u.RunID, &u.DeviceID, &u.IP, &u.IPCity, &u.UserAgent, &u.Scene, &u.OrgName, &u.Region, &u.Question, &u.CreatedAt, &u.RunStatus, &u.HasLead, &u.Phone, &u.ContactName, &u.ContactTitle); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

func (s *Store) FreeCheckupUsageStats(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	var total, today int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_usage`).Scan(&total); err == nil {
		out["usage_total"] = total
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_usage WHERE created_at >= CURRENT_DATE`).Scan(&today); err == nil {
		out["usage_today"] = today
	}
	return out, nil
}

func (s *Store) FreeCheckupLeadStats(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM free_checkup_leads GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	var today int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_leads WHERE created_at >= CURRENT_DATE`).Scan(&today); err == nil {
		out["today"] = today
	}
	return out, rows.Err()
}
