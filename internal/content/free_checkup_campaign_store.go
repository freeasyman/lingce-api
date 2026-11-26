package content

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

)

// FreeCheckupCampaign 投放码：渠道 + 销售 + 场景 + 活动名 + 随机令牌。
type FreeCheckupCampaign struct {
	ID          int64      `json:"id"`
	Token       string     `json:"token"`
	Name        string     `json:"name"`
	ChannelID   int64      `json:"channel_id"`
	ChannelName string     `json:"channel_name"`
	SalesID     int64      `json:"sales_id"`
	SalesName   string     `json:"sales_name"`
	Scene       string     `json:"scene"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Remark      string     `json:"remark"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Scans       int        `json:"scans"`
	Checks      int        `json:"checks"`
	Leads       int        `json:"leads"`
	Converted   int        `json:"converted"`
	LandingURL  string     `json:"landing_url,omitempty"`
}

func newCheckupToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Store) CreateFreeCheckupCampaign(ctx context.Context, c *FreeCheckupCampaign) (*FreeCheckupCampaign, error) {
	if c.Token == "" {
		c.Token = newCheckupToken()
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO free_checkup_campaigns (token, name, channel_id, channel_name, sales_id, sales_name, scene, status, remark, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id, token, created_at, updated_at`,
		c.Token, c.Name, c.ChannelID, c.ChannelName, c.SalesID, c.SalesName, c.Scene, c.Status, c.Remark, 0).Scan(&c.ID, &c.Token, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ListFreeCheckupCampaigns(ctx context.Context, limit, offset int) ([]FreeCheckupCampaign, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM free_checkup_campaigns`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.token, c.name, c.channel_id, c.channel_name, c.sales_id, c.sales_name, c.scene, c.status, c.expires_at, c.remark, c.created_at, c.updated_at,
			(SELECT count(*) FROM free_checkup_scans s WHERE s.campaign_id=c.id),
			(SELECT count(*) FROM free_checkup_usage u WHERE u.campaign_id=c.id),
			(SELECT count(*) FROM free_checkup_leads l WHERE l.campaign_id=c.id),
			(SELECT count(*) FROM free_checkup_leads l WHERE l.campaign_id=c.id AND l.status='converted')
		FROM free_checkup_campaigns c ORDER BY c.id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []FreeCheckupCampaign
	for rows.Next() {
		var c FreeCheckupCampaign
		if err := rows.Scan(&c.ID, &c.Token, &c.Name, &c.ChannelID, &c.ChannelName, &c.SalesID, &c.SalesName, &c.Scene, &c.Status, &c.ExpiresAt, &c.Remark, &c.CreatedAt, &c.UpdatedAt, &c.Scans, &c.Checks, &c.Leads, &c.Converted); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (s *Store) GetFreeCheckupCampaignByToken(ctx context.Context, token string) (*FreeCheckupCampaign, error) {
	var c FreeCheckupCampaign
	err := s.pool.QueryRow(ctx, `
		SELECT id, token, name, channel_id, channel_name, sales_id, sales_name, scene, status, expires_at, remark, created_at, updated_at
		FROM free_checkup_campaigns WHERE token=$1`, token).
		Scan(&c.ID, &c.Token, &c.Name, &c.ChannelID, &c.ChannelName, &c.SalesID, &c.SalesName, &c.Scene, &c.Status, &c.ExpiresAt, &c.Remark, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) GetFreeCheckupCampaignByID(ctx context.Context, id int64) (*FreeCheckupCampaign, error) {
	var c FreeCheckupCampaign
	err := s.pool.QueryRow(ctx, `
		SELECT id, token, name, channel_id, channel_name, sales_id, sales_name, scene, status, expires_at, remark, created_at, updated_at
		FROM free_checkup_campaigns WHERE id=$1`, id).
		Scan(&c.ID, &c.Token, &c.Name, &c.ChannelID, &c.ChannelName, &c.SalesID, &c.SalesName, &c.Scene, &c.Status, &c.ExpiresAt, &c.Remark, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) UpdateFreeCheckupCampaignStatus(ctx context.Context, id int64, status string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE free_checkup_campaigns SET status=$2, updated_at=now() WHERE id=$1`, id, status)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RecordFreeCheckupScan 记一次访问；同一 sid 对同一投放码 30 分钟内去重。
func (s *Store) RecordFreeCheckupScan(ctx context.Context, campaignID int64, sid, deviceID, ip, ipCity, ua string) error {
	var exists bool
	_ = s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM free_checkup_scans
			WHERE campaign_id=$1 AND sid=$2 AND created_at > now() - interval '30 minutes')`,
		campaignID, sid).Scan(&exists)
	if exists {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO free_checkup_scans (campaign_id, sid, device_id, ip, ip_city, user_agent)
		VALUES ($1,$2,$3,$4,$5,$6)`, campaignID, sid, deviceID, ip, ipCity, ua)
	return err
}
