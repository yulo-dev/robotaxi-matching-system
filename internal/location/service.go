package location

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
	"github.com/robotaxi/internal/models"
)

const geoKey = "av:locations"

type Service struct {
	rdb *redis.Client
}

func NewService(rdb *redis.Client) *Service {
	return &Service{rdb: rdb}
}

// UpdateLocation sets an AV's current position using GEOADD.
func (s *Service) UpdateLocation(ctx context.Context, avID string, loc models.Location) error {
	return s.rdb.GeoAdd(ctx, geoKey, &redis.GeoLocation{
		Name:      avID,
		Longitude: loc.Lng,
		Latitude:  loc.Lat,
	}).Err()
}

// RemoveLocation removes an AV from the geo index.
func (s *Service) RemoveLocation(ctx context.Context, avID string) error {
	return s.rdb.ZRem(ctx, geoKey, avID).Err()
}

// FindNearbyAVs returns AV IDs within radiusKm of the given location.
func (s *Service) FindNearbyAVs(ctx context.Context, loc models.Location, radiusKm float64, limit int) ([]string, error) {
	results, err := s.rdb.GeoSearch(ctx, geoKey, &redis.GeoSearchQuery{
		Longitude:  loc.Lng,
		Latitude:   loc.Lat,
		Radius:     radiusKm,
		RadiusUnit: "km",
		Sort:       "ASC",
		Count:      limit,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("geosearch: %w", err)
	}
	return results, nil
}

// SetAVStatus stores the AV's availability status in a hash.
func (s *Service) SetAVStatus(ctx context.Context, avID string, status models.AVStatus, battery float64) error {
	return s.rdb.HSet(ctx, "av:status:"+avID, map[string]interface{}{
		"status":  string(status),
		"battery": strconv.FormatFloat(battery, 'f', 1, 64),
	}).Err()
}

// GetAVStatus reads status + battery from Redis.
func (s *Service) GetAVStatus(ctx context.Context, avID string) (models.AVStatus, float64, error) {
	vals, err := s.rdb.HGetAll(ctx, "av:status:"+avID).Result()
	if err != nil {
		return "", 0, err
	}
	st := models.AVStatus(vals["status"])
	bat, _ := strconv.ParseFloat(vals["battery"], 64)
	return st, bat, nil
}

// FilterAvailable filters a list of AV IDs, returning only those with AVAILABLE status.
func (s *Service) FilterAvailable(ctx context.Context, avIDs []string) ([]string, error) {
	var available []string
	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(avIDs))
	for i, id := range avIDs {
		cmds[i] = pipe.HGetAll(ctx, "av:status:"+id)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	for i, cmd := range cmds {
		vals, _ := cmd.Result()
		if vals["status"] == string(models.AVStatusAvailable) {
			available = append(available, avIDs[i])
		}
	}
	return available, nil
}
