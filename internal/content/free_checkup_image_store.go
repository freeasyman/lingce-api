package content

import (
	"context"
	"time"
)

// InsertFreeCheckupImage 保存前端生成的长图，返回 id。
func (s *Store) InsertFreeCheckupImage(ctx context.Context, contentType string, data []byte) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO free_checkup_images (content_type, data) VALUES ($1,$2) RETURNING id`, contentType, data).Scan(&id)
	return id, err
}

// GetFreeCheckupImage 读取长图。
func (s *Store) GetFreeCheckupImage(ctx context.Context, id int64) (string, []byte, error) {
	var ct string
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT content_type, data FROM free_checkup_images WHERE id=$1`, id).Scan(&ct, &data)
	return ct, data, err
}

// DeleteOldFreeCheckupImages 清理过期长图（默认保留 7 天）。
func (s *Store) DeleteOldFreeCheckupImages(ctx context.Context, olderThan time.Duration) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM free_checkup_images WHERE created_at < now() - $1::interval`, olderThan.String())
	return err
}
