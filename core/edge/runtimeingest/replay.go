package runtimeingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	ReplayWindowKeyPrefix      = "edge:rt:nonce:"
	ReplayWindowTTL            = time.Hour
	MaxReplayWindowCardinality = int64(10000)
)

var ErrReplayWindowFull = errors.New("runtime replay window cap exhausted")

type ReplayWindow struct {
	client  redis.Cmdable
	ttl     time.Duration
	maxCard int64
}

func NewReplayWindow(client redis.Cmdable, ttl time.Duration, maxCard int64) *ReplayWindow {
	if ttl <= 0 {
		ttl = ReplayWindowTTL
	}
	if maxCard <= 0 {
		maxCard = MaxReplayWindowCardinality
	}
	return &ReplayWindow{client: client, ttl: ttl, maxCard: maxCard}
}

func (r *ReplayWindow) Reserve(ctx context.Context, tenantID, collectorID, nonce string) (bool, error) {
	key, value, err := r.keyAndValue(tenantID, collectorID, nonce)
	if err != nil {
		return false, err
	}
	count, err := r.client.SCard(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count >= r.maxCard {
		seen, err := r.client.SIsMember(ctx, key, value).Result()
		if err != nil {
			return false, err
		}
		if seen {
			return false, nil
		}
		return false, ErrReplayWindowFull
	}
	added, err := r.client.SAdd(ctx, key, value).Result()
	if err != nil {
		return false, err
	}
	if added == 0 {
		return false, nil
	}
	if err := r.client.Expire(ctx, key, r.ttl).Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *ReplayWindow) Release(ctx context.Context, tenantID, collectorID, nonce string) error {
	key, value, err := r.keyAndValue(tenantID, collectorID, nonce)
	if err != nil {
		return err
	}
	return r.client.SRem(ctx, key, value).Err()
}

func (r *ReplayWindow) keyAndValue(tenantID, collectorID, nonce string) (string, string, error) {
	if r == nil || r.client == nil {
		return "", "", errors.New("runtime replay window redis client unavailable")
	}
	tenantID = strings.TrimSpace(tenantID)
	collectorID = strings.TrimSpace(collectorID)
	nonce = strings.TrimSpace(nonce)
	if tenantID == "" || collectorID == "" || nonce == "" {
		return "", "", fmt.Errorf("runtime replay window requires tenant_id, collector_id, and nonce")
	}
	return ReplayWindowKeyPrefix + tenantID + ":" + collectorID, replayNonceDigest(nonce), nil
}

func replayNonceDigest(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return hex.EncodeToString(sum[:])
}
