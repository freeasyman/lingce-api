package his

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// birthDateLayout 是 customers.birth_date 对外的统一格式。
const birthDateLayout = "2006-01-02"

// normalizeBirthDate 解析常见出生日期写法并统一为 YYYY-MM-DD。
// 只接受完整年月日；未来日期与非法日期一律拒绝，避免把估算值写成事实。
func normalizeBirthDate(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	layouts := []string{"2006-01-02", "2006-1-2", "2006/01/02", "2006/1/2", "2006.01.02", "2006.1.2", "20060102"}
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err != nil {
			continue
		}
		if parsed.Year() < 1900 || parsed.After(time.Now()) {
			return "", ErrInvalidRequest
		}
		return parsed.Format(birthDateLayout), nil
	}
	return "", ErrInvalidRequest
}

// birthDateRequiredError 出生日期是建档/首次挂号的必填项。
func birthDateRequiredError() *ValidationError {
	return &ValidationError{
		Message: "请填写出生日期",
		Missing: []MissingField{{Field: "birth_date", Label: "出生日期"}},
	}
}

// invalidBirthDateError 出生日期格式不合法（与缺项一样带字段定位）。
func invalidBirthDateError() *ValidationError {
	return &ValidationError{
		Message: "出生日期格式不正确",
		Missing: []MissingField{{Field: "birth_date", Label: "出生日期"}},
	}
}

// birthDateFromIDCard 从 18 位/15 位居民身份证号解析出生日期。
// 解析失败返回空串（不猜测、不补造），由详情页提示人工补录。
func birthDateFromIDCard(raw string) string {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if len(value) == 15 {
		// 15 位老身份证：YYMMDD + 顺序码；年份按 19xx 处理。
		parsed, err := time.ParseInLocation("060102", value[6:12], time.Local)
		if err != nil {
			return ""
		}
		return parsed.Format(birthDateLayout)
	}
	if len(value) != 18 {
		return ""
	}
	for _, r := range value[:17] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	parsed, err := time.ParseInLocation("20060102", value[6:14], time.Local)
	if err != nil {
		return ""
	}
	if parsed.Year() < 1900 || parsed.After(time.Now()) {
		return ""
	}
	return parsed.Format(birthDateLayout)
}

// birthDateForRequest 优先使用显式出生日期；缺省时用身份证号推算。
func birthDateForRequest(rawBirthDate, idCard string) (*time.Time, error) {
	value, err := normalizeBirthDate(rawBirthDate)
	if err != nil {
		return nil, err
	}
	if value == "" {
		value = birthDateFromIDCard(idCard)
	}
	if value == "" {
		return nil, nil
	}
	parsed, err := time.ParseInLocation(birthDateLayout, value, time.Local)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	return &parsed, nil
}

// ageFromBirthDate 按自然年估算年龄（仅用于展示，不反写出生日期）。
func ageFromBirthDate(birth time.Time, now time.Time) int {
	age := now.Year() - birth.Year()
	if now.Month() < birth.Month() || (now.Month() == birth.Month() && now.Day() < birth.Day()) {
		age--
	}
	if age < 0 {
		return 0
	}
	return age
}

func formatBirthDate(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format(birthDateLayout)
}

// PatientBirthAndIDCard 读取患者出生日期与身份证号（注册响应与病历快照用）。
func (s *Store) PatientBirthAndIDCard(ctx context.Context, tenantID, customerID int64) (string, string, error) {
	var birthDate *time.Time
	var extraRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT birth_date, COALESCE(extra_data, '{}'::jsonb)
		FROM customers
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`,
		tenantID, customerID).Scan(&birthDate, &extraRaw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return formatBirthDate(birthDate), extraString(extraRaw, "id_card"), nil
}

// UpdateCustomer 编辑患者主档（仅更新显式传入的字段），返回更新后的出生日期。
func (s *Store) UpdateCustomer(ctx context.Context, tenantID, customerID int64, req UpdatePatientRequest) (string, error) {
	var (
		name, phone, gender string
		age                 *int
		birthDate           *time.Time
		extraRaw            []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(name, ''), COALESCE(phone, ''), COALESCE(gender, ''),
		       age, birth_date, COALESCE(extra_data, '{}'::jsonb)
		FROM customers
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`,
		tenantID, customerID).Scan(&name, &phone, &gender, &age, &birthDate, &extraRaw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", ErrNotFound
		}
		return "", err
	}

	extra := map[string]string{}
	if len(extraRaw) > 0 {
		_ = json.Unmarshal(extraRaw, &extra)
	}
	if extra == nil {
		extra = map[string]string{}
	}
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if req.Phone != nil {
		phone = strings.TrimSpace(*req.Phone)
	}
	if req.Gender != nil {
		gender = strings.TrimSpace(*req.Gender)
	}
	if req.Age != nil {
		age = req.Age
	}
	if req.Address != nil {
		setOrDelete(extra, "address", *req.Address)
	}
	if req.Allergy != nil {
		setOrDelete(extra, "allergy", *req.Allergy)
	}
	if req.IDCard != nil {
		idCard := strings.TrimSpace(*req.IDCard)
		setOrDelete(extra, "id_card", idCard)
		if req.BirthDate == nil && idCard != "" {
			derived := birthDateFromIDCard(idCard)
			if derived == "" {
				return "", ErrInvalidRequest
			}
			parsed, _ := time.ParseInLocation(birthDateLayout, derived, time.Local)
			birthDate = &parsed
		}
	}
	if req.BirthDate != nil {
		value, err := normalizeBirthDate(*req.BirthDate)
		if err != nil {
			return "", err
		}
		if value == "" {
			birthDate = nil
		} else {
			parsed, err := time.ParseInLocation(birthDateLayout, value, time.Local)
			if err != nil {
				return "", ErrInvalidRequest
			}
			birthDate = &parsed
		}
	}
	if birthDate != nil && req.Age == nil {
		computed := ageFromBirthDate(*birthDate, time.Now())
		if computed > 0 {
			age = &computed
		}
	}
	extraJSON, err := json.Marshal(extra)
	if err != nil {
		return "", err
	}
	if name == "" || phone == "" {
		return "", ErrInvalidRequest
	}
	// 拼音检索字段随姓名同步重算（姓名未变时等价于幂等回填）。
	pinyin, initials := PinyinOf(name)
	tag, err := s.pool.Exec(ctx, `
		UPDATE customers
		SET name = $3, phone = $4, gender = $5, age = $6, birth_date = $7,
		    extra_data = $8::jsonb, pinyin = $9, initials = $10, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`,
		tenantID, customerID, name, phone, nullIfEmpty(gender), age, birthDate, string(extraJSON), pinyin, initials)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return formatBirthDate(birthDate), nil
}

func setOrDelete(extra map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		delete(extra, key)
		return
	}
	extra[key] = value
}
