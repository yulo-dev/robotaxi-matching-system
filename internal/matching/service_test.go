package matching

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robotaxi/internal/models"
)

// --- Redis test helper ---

func setupTestRedis(t *testing.T) *redis.Client {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15}) // use DB 15 for tests
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	rdb.FlushDB(ctx)
	t.Cleanup(func() { rdb.FlushDB(ctx); rdb.Close() })
	return rdb
}

func TestMatchingState_CreateAndRead(t *testing.T) {
	rdb := setupTestRedis(t)
	ctx := context.Background()

	state := models.MatchingState{
		RideID:     "ride-001",
		Candidates: []string{"AV-0001", "AV-0002", "AV-0003"},
		Cursor:     0,
		Status:     models.MatchSearching,
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := rdb.Set(ctx, matchPrefix+"ride-001", data, 5*time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	// Read back
	raw, err := rdb.Get(ctx, matchPrefix+"ride-001").Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var got models.MatchingState
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	if got.RideID != "ride-001" {
		t.Errorf("ride_id = %s, want ride-001", got.RideID)
	}
	if len(got.Candidates) != 3 {
		t.Errorf("candidates len = %d, want 3", len(got.Candidates))
	}
	if got.Status != models.MatchSearching {
		t.Errorf("status = %s, want SEARCHING", got.Status)
	}
}

func TestMatchingState_CursorAdvance(t *testing.T) {
	rdb := setupTestRedis(t)
	ctx := context.Background()

	state := models.MatchingState{
		RideID:     "ride-002",
		Candidates: []string{"AV-0001", "AV-0002", "AV-0003"},
		Cursor:     0,
		Status:     models.MatchSearching,
	}

	// Simulate rejection: advance cursor
	state.Cursor = 1
	data, _ := json.Marshal(state)
	rdb.Set(ctx, matchPrefix+"ride-002", data, 5*time.Minute)

	raw, _ := rdb.Get(ctx, matchPrefix+"ride-002").Bytes()
	var got models.MatchingState
	json.Unmarshal(raw, &got)

	if got.Cursor != 1 {
		t.Errorf("cursor = %d, want 1", got.Cursor)
	}
	if got.Candidates[got.Cursor] != "AV-0002" {
		t.Errorf("next candidate = %s, want AV-0002", got.Candidates[got.Cursor])
	}
}

func TestMatchingState_DoneStatus(t *testing.T) {
	rdb := setupTestRedis(t)
	ctx := context.Background()

	state := models.MatchingState{
		RideID:     "ride-003",
		Candidates: []string{"AV-0001", "AV-0002"},
		Cursor:     1,
		Status:     models.MatchDone,
	}
	data, _ := json.Marshal(state)
	rdb.Set(ctx, matchPrefix+"ride-003", data, 5*time.Minute)

	raw, _ := rdb.Get(ctx, matchPrefix+"ride-003").Bytes()
	var got models.MatchingState
	json.Unmarshal(raw, &got)

	if got.Status != models.MatchDone {
		t.Errorf("status = %s, want DONE", got.Status)
	}
}

func TestMatchingState_AllExhausted(t *testing.T) {
	state := models.MatchingState{
		RideID:     "ride-004",
		Candidates: []string{"AV-0001", "AV-0002"},
		Cursor:     2,
		Status:     models.MatchSearching,
	}
	if state.Cursor < len(state.Candidates) {
		t.Error("cursor should be past all candidates")
	}
}

func TestPerRideLock(t *testing.T) {
	rdb := setupTestRedis(t)
	ctx := context.Background()

	lockKey := lockPrefix + "ride-005"

	// First lock should succeed
	ok, err := rdb.SetNX(ctx, lockKey, "worker-1", lockTTL).Result()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("first lock should succeed")
	}

	// Second lock should fail (already held)
	ok2, err := rdb.SetNX(ctx, lockKey, "worker-2", lockTTL).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ok2 {
		t.Error("second lock should fail — per-ride lock violated")
	}

	// Release and retry
	rdb.Del(ctx, lockKey)
	ok3, _ := rdb.SetNX(ctx, lockKey, "worker-2", lockTTL).Result()
	if !ok3 {
		t.Error("lock after release should succeed")
	}
}
