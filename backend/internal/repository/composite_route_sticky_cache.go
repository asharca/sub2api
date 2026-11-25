package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const compositeRouteStickyPrefix = "composite_route_sticky:"

type compositeRouteStickyCache struct {
	rdb *redis.Client
}

func NewCompositeRouteStickyCache(rdb *redis.Client) service.CompositeRouteStickyStore {
	return &compositeRouteStickyCache{rdb: rdb}
}

// buildCompositeRouteStickyKey 格式: composite_route_sticky:{groupID}:{routeID}:{sessionHash}
func buildCompositeRouteStickyKey(groupID, routeID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%d:%s", compositeRouteStickyPrefix, groupID, routeID, sessionHash)
}

func (c *compositeRouteStickyCache) GetCompositeRouteSticky(ctx context.Context, groupID, routeID int64, sessionHash string) (string, bool, error) {
	value, err := c.rdb.Get(ctx, buildCompositeRouteStickyKey(groupID, routeID, sessionHash)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

func (c *compositeRouteStickyCache) SetCompositeRouteSticky(ctx context.Context, groupID, routeID int64, sessionHash, target string, ttl time.Duration) error {
	return c.rdb.Set(ctx, buildCompositeRouteStickyKey(groupID, routeID, sessionHash), target, ttl).Err()
}
