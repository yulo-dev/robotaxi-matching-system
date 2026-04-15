package location

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/robotaxi/internal/models"
)

func setupTestRedis(t *testing.T) *redis.Client {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 14})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	rdb.FlushDB(ctx)
	t.Cleanup(func() { rdb.FlushDB(ctx); rdb.Close() })
	return rdb
}

func TestUpdateAndFindNearby(t *testing.T) {
	rdb := setupTestRedis(t)
	svc := NewService(rdb)
	ctx := context.Background()

	// Place 3 AVs around SF
	avs := []struct {
		id  string
		loc models.Location
	}{
		{"AV-0001", models.Location{Lat: 37.7749, Lng: -122.4194}},  // SF center
		{"AV-0002", models.Location{Lat: 37.7760, Lng: -122.4180}},  // ~150m away
		{"AV-0003", models.Location{Lat: 37.8000, Lng: -122.4000}},  // ~3km away
	}
	for _, a := range avs {
		if err := svc.UpdateLocation(ctx, a.id, a.loc); err != nil {
			t.Fatalf("update location %s: %v", a.id, err)
		}
	}

	// Search within 1km — should find AV-0001 and AV-0002
	results, err := svc.FindNearbyAVs(ctx, models.Location{Lat: 37.7749, Lng: -122.4194}, 1.0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 AVs within 1km, got %d: %v", len(results), results)
	}

	// Search within 5km — should find all 3
	results2, err := svc.FindNearbyAVs(ctx, models.Location{Lat: 37.7749, Lng: -122.4194}, 5.0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results2) != 3 {
		t.Errorf("expected 3 AVs within 5km, got %d", len(results2))
	}
}

func TestFindNearby_SortedByDistance(t *testing.T) {
	rdb := setupTestRedis(t)
	svc := NewService(rdb)
	ctx := context.Background()

	svc.UpdateLocation(ctx, "AV-FAR", models.Location{Lat: 37.80, Lng: -122.40})
	svc.UpdateLocation(ctx, "AV-NEAR", models.Location{Lat: 37.775, Lng: -122.419})

	results, _ := svc.FindNearbyAVs(ctx, models.Location{Lat: 37.7749, Lng: -122.4194}, 10, 10)
	if len(results) < 2 {
		t.Fatal("expected at least 2 results")
	}
	if results[0] != "AV-NEAR" {
		t.Errorf("closest should be AV-NEAR, got %s", results[0])
	}
}

func TestRemoveLocation(t *testing.T) {
	rdb := setupTestRedis(t)
	svc := NewService(rdb)
	ctx := context.Background()

	svc.UpdateLocation(ctx, "AV-DEL", models.Location{Lat: 37.77, Lng: -122.42})
	svc.RemoveLocation(ctx, "AV-DEL")

	results, _ := svc.FindNearbyAVs(ctx, models.Location{Lat: 37.77, Lng: -122.42}, 1, 10)
	if len(results) != 0 {
		t.Errorf("expected 0 after removal, got %d", len(results))
	}
}

func TestFilterAvailable(t *testing.T) {
	rdb := setupTestRedis(t)
	svc := NewService(rdb)
	ctx := context.Background()

	svc.SetAVStatus(ctx, "AV-A", models.AVStatusAvailable, 90)
	svc.SetAVStatus(ctx, "AV-B", models.AVStatusEnRoute, 80)
	svc.SetAVStatus(ctx, "AV-C", models.AVStatusAvailable, 70)
	svc.SetAVStatus(ctx, "AV-D", models.AVStatusCharging, 20)

	avail, err := svc.FilterAvailable(ctx, []string{"AV-A", "AV-B", "AV-C", "AV-D"})
	if err != nil {
		t.Fatal(err)
	}
	if len(avail) != 2 {
		t.Errorf("expected 2 available, got %d: %v", len(avail), avail)
	}
}

func TestSetAndGetAVStatus(t *testing.T) {
	rdb := setupTestRedis(t)
	svc := NewService(rdb)
	ctx := context.Background()

	svc.SetAVStatus(ctx, "AV-X", models.AVStatusAvailable, 85.5)
	st, bat, err := svc.GetAVStatus(ctx, "AV-X")
	if err != nil {
		t.Fatal(err)
	}
	if st != models.AVStatusAvailable {
		t.Errorf("status = %s, want AVAILABLE", st)
	}
	if bat != 85.5 {
		t.Errorf("battery = %f, want 85.5", bat)
	}
}
