// Package customercontact keeps the contact information shared by customer
// lists, operating plans and tasks in one shape. Nicknames are contact clues,
// not unique identifiers for merging customers.
package customercontact

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Contact struct {
	ID           int64  `json:"id,omitempty"`
	ChannelType  string `json:"channel_type"`
	ExternalID   string `json:"external_id,omitempty"`
	ExternalName string `json:"external_name,omitempty"`
}

func NormalizeChannel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "微信", "wechat", "weixin", "朋友圈", "微信朋友圈", "moments":
		return "wechat"
	case "企业微信", "企微", "wecom":
		return "wecom"
	case "小红书", "xiaohongshu", "xhs":
		return "xiaohongshu"
	case "抖音", "douyin":
		return "douyin"
	case "视频号", "微信视频号", "shipinhao":
		return "shipinhao"
	case "未知", "无法判断", "unknown", "":
		return "unknown"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func IsInternal(channel string) bool {
	switch NormalizeChannel(channel) {
	case "patient", "__auto__":
		return true
	}
	return false
}

func ChannelAliases(channel string) []string {
	switch NormalizeChannel(channel) {
	case "wechat":
		return []string{"wechat", "weixin", "微信", "moments", "朋友圈", "微信朋友圈"}
	case "wecom":
		return []string{"wecom", "企业微信", "企微"}
	case "xiaohongshu":
		return []string{"xiaohongshu", "小红书", "xhs"}
	case "douyin":
		return []string{"douyin", "抖音"}
	case "shipinhao":
		return []string{"shipinhao", "视频号", "微信视频号"}
	}
	return []string{}
}

func (c Contact) CanContact() bool {
	if strings.TrimSpace(c.ExternalID) == "" && strings.TrimSpace(c.ExternalName) == "" {
		return false
	}
	switch NormalizeChannel(c.ChannelType) {
	case "wechat", "wecom", "xiaohongshu", "douyin", "shipinhao":
		return true
	}
	return false
}

type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Load uses one query for all customers and enforces tenant and live-customer
// boundaries through the parent customer. Internal legacy identifiers are not
// exposed as contact information.
func Load(ctx context.Context, db Querier, tenantID int64, ids []int64) (map[int64][]Contact, error) {
	result := make(map[int64][]Contact)
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := db.Query(ctx, `
		SELECT ci.customer_id, ci.id, COALESCE(NULLIF(ci.channel,''),ci.channel_type,''),
		       COALESCE(NULLIF(ci.channel_id,''),ci.external_id,''),
		       COALESCE(NULLIF(ci.nickname,''),ci.external_name,'')
		FROM customer_identities ci
		JOIN customers c ON c.id=ci.customer_id
		WHERE c.id=ANY($1) AND c.deleted_at IS NULL
		  AND ($2::bigint=0 OR c.tenant_id=$2)
		ORDER BY ci.id DESC`, ids, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[int64]map[string]bool)
	for rows.Next() {
		var customerID int64
		var c Contact
		if err := rows.Scan(&customerID, &c.ID, &c.ChannelType, &c.ExternalID, &c.ExternalName); err != nil {
			return nil, err
		}
		c.ChannelType = NormalizeChannel(c.ChannelType)
		c.ExternalID = strings.TrimSpace(c.ExternalID)
		c.ExternalName = strings.TrimSpace(c.ExternalName)
		if IsInternal(c.ChannelType) || (c.ExternalID == "" && c.ExternalName == "") {
			continue
		}
		key := c.ChannelType + "\x00" + c.ExternalID + "\x00" + c.ExternalName
		if seen[customerID] == nil {
			seen[customerID] = make(map[string]bool)
		}
		if !seen[customerID][key] {
			result[customerID] = append(result[customerID], c)
			seen[customerID][key] = true
		}
	}
	return result, rows.Err()
}
