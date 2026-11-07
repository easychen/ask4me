package main

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *store) countRequestsByKeySince(ctx context.Context, keyHash string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM requests WHERE api_key_hash=? AND created_at>=?`,
		keyHash, since.Unix(),
	).Scan(&n)
	return n, err
}

func (s *store) countPendingByKey(ctx context.Context, keyHash string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM requests WHERE api_key_hash=? AND status IN ('created','delivered')`,
		keyHash,
	).Scan(&n)
	return n, err
}

func (s *store) countRequestsSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM requests WHERE created_at>=?`,
		since.Unix(),
	).Scan(&n)
	return n, err
}

func (s *store) countRequestsByIPSince(ctx context.Context, ip string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM requests WHERE client_ip=? AND created_at>=?`,
		ip, since.Unix(),
	).Scan(&n)
	return n, err
}

func (s *store) countDistinctKeysByIPSince(ctx context.Context, ip string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT api_key_hash) FROM requests WHERE client_ip=? AND created_at>=? AND api_key_hash IS NOT NULL`,
		ip, since.Unix(),
	).Scan(&n)
	return n, err
}

func (s *store) keySeenForIP(ctx context.Context, ip, keyHash string) (bool, error) {
	var x int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM requests WHERE client_ip=? AND api_key_hash=? LIMIT 1`,
		ip, keyHash,
	).Scan(&x)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *store) getRequestStatusOwner(ctx context.Context, reqID string) (string, int64, sql.NullString, error) {
	var status string
	var expiresAt int64
	var owner sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT status, expires_at, api_key_hash FROM requests WHERE request_id=?`,
		reqID,
	).Scan(&status, &expiresAt, &owner)
	return status, expiresAt, owner, err
}
