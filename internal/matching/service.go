package matching

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/robotaxi/internal/db"
	"github.com/robotaxi/internal/location"
	"github.com/robotaxi/internal/middleware"
	"github.com/robotaxi/internal/models"
	"github.com/robotaxi/internal/ws"
)

const (
	matchPrefix   = "match:ride:"
	lockPrefix    = "lock:ride:"
	lockTTL       = 30 * time.Second
	searchRadius  = 10.0
	maxCandidates = 10
)

type Service struct {
	rdb        *redis.Client
	locSvc     *location.Service
	store      *db.Store
	hub        *ws.Hub
	DispatchCh chan DispatchEvent
}

type DispatchEvent struct {
	RideID string          `json:"ride_id"`
	AVID   string          `json:"av_id"`
	Pickup models.Location `json:"pickup"`
}

func NewService(rdb *redis.Client, locSvc *location.Service, store *db.Store) *Service {
	return &Service{
		rdb:        rdb,
		locSvc:     locSvc,
		store:      store,
		DispatchCh: make(chan DispatchEvent, 100),
	}
}

func (s *Service) SetHub(h *ws.Hub) { s.hub = h }

func (s *Service) broadcast(evt ws.Event) {
	if s.hub != nil {
		s.hub.Broadcast(evt)
	}
}

// StartMatching begins the matching process for a ride.
func (s *Service) StartMatching(ctx context.Context, ride *models.Ride) error {
	start := time.Now()
	middleware.RidesRequested.Inc()

	nearbyIDs, err := s.locSvc.FindNearbyAVs(ctx, ride.Source, searchRadius, maxCandidates)
	if err != nil {
		return fmt.Errorf("find nearby: %w", err)
	}

	candidates, err := s.locSvc.FilterAvailable(ctx, nearbyIDs)
	if err != nil {
		return fmt.Errorf("filter available: %w", err)
	}

	if len(candidates) == 0 {
		log.Printf("[matching] no candidates for ride %s", ride.ID)
		middleware.RidesFailed.Inc()
		middleware.MatchLatency.Observe(time.Since(start).Seconds())
		s.broadcast(ws.Event{Type: "match_update", Payload: map[string]interface{}{
			"ride_id": ride.ID.String(), "status": "FAILED", "reason": "no candidates",
		}})
		return s.store.UpdateRideStatus(ctx, ride.ID, models.RideStatusCancelled, nil)
	}

	state := models.MatchingState{
		RideID:     ride.ID.String(),
		Candidates: candidates,
		Cursor:     0,
		Status:     models.MatchSearching,
	}
	data, _ := json.Marshal(state)
	s.rdb.Set(ctx, matchPrefix+ride.ID.String(), data, 5*time.Minute)
	s.store.UpdateRideStatus(ctx, ride.ID, models.RideStatusMatching, nil)

	middleware.QueueDepth.Inc()
	s.broadcast(ws.Event{Type: "match_update", Payload: map[string]interface{}{
		"ride_id": ride.ID.String(), "status": "SEARCHING",
		"candidates": candidates, "cursor": 0,
	}})

	go s.dispatchNext(context.Background(), ride.ID.String(), ride.Source)
	return nil
}

func (s *Service) dispatchNext(ctx context.Context, rideID string, pickup models.Location) {
	lockKey := lockPrefix + rideID
	ok, err := s.rdb.SetNX(ctx, lockKey, "1", lockTTL).Result()
	if err != nil || !ok {
		middleware.LockContentions.Inc()
		log.Printf("[matching] lock contention for ride %s", rideID)
		return
	}
	defer s.rdb.Del(ctx, lockKey)

	state, err := s.GetMatchState(ctx, rideID)
	if err != nil || state == nil || state.Status != models.MatchSearching {
		return
	}

	if state.Cursor >= len(state.Candidates) {
		state.Status = models.MatchFailed
		s.saveState(ctx, state)
		uid, _ := uuid.Parse(rideID)
		s.store.UpdateRideStatus(ctx, uid, models.RideStatusCancelled, nil)
		middleware.RidesFailed.Inc()
		middleware.QueueDepth.Dec()
		s.broadcast(ws.Event{Type: "match_update", Payload: map[string]interface{}{
			"ride_id": rideID, "status": "FAILED", "reason": "all candidates exhausted",
		}})
		log.Printf("[matching] all candidates exhausted for ride %s", rideID)
		return
	}

	avID := state.Candidates[state.Cursor]
	middleware.DispatchAttempts.Inc()
	log.Printf("[matching] dispatching ride %s to AV %s (cursor=%d)", rideID, avID, state.Cursor)

	s.broadcast(ws.Event{Type: "match_update", Payload: map[string]interface{}{
		"ride_id": rideID, "status": "DISPATCHED", "av_id": avID,
		"cursor": state.Cursor, "candidates": state.Candidates,
	}})

	s.DispatchCh <- DispatchEvent{RideID: rideID, AVID: avID, Pickup: pickup}
}

// HandleDispatchDecision processes an AV's accept/reject response.
func (s *Service) HandleDispatchDecision(ctx context.Context, resp models.DispatchResponse) error {
	lockKey := lockPrefix + resp.RideID
	ok, err := s.rdb.SetNX(ctx, lockKey, "1", lockTTL).Result()
	if err != nil || !ok {
		middleware.LockContentions.Inc()
		return fmt.Errorf("lock contention for ride %s", resp.RideID)
	}
	defer s.rdb.Del(ctx, lockKey)

	state, err := s.GetMatchState(ctx, resp.RideID)
	if err != nil || state == nil {
		return fmt.Errorf("no match state for ride %s", resp.RideID)
	}
	if state.Status != models.MatchSearching {
		return fmt.Errorf("ride %s already %s", resp.RideID, state.Status)
	}

	if resp.Decision == models.DecisionAccept {
		state.Status = models.MatchDone
		state.Cursor++
		s.saveState(ctx, state)

		rideUUID, _ := uuid.Parse(resp.RideID)
		if err := s.store.AssignAVToRide(ctx, rideUUID, resp.AVID); err != nil {
			middleware.UniqueIndexViolations.Inc()
			log.Printf("[matching] unique index violation: %v, re-matching", err)
			state.Status = models.MatchSearching
			s.saveState(ctx, state)
			go s.dispatchNext(context.Background(), resp.RideID, models.Location{})
			return err
		}

		s.locSvc.SetAVStatus(ctx, resp.AVID, models.AVStatusEnRoute, 0)
		s.store.UpdateAVStatus(ctx, resp.AVID, models.AVStatusEnRoute)
		middleware.RidesMatched.Inc()
		middleware.QueueDepth.Dec()

		s.broadcast(ws.Event{Type: "ride_update", Payload: map[string]string{
			"ride_id": resp.RideID, "status": "DRIVER_ASSIGNED", "av_id": resp.AVID,
		}})
		s.broadcast(ws.Event{Type: "match_update", Payload: map[string]interface{}{
			"ride_id": resp.RideID, "status": "DONE", "av_id": resp.AVID,
			"cursor": state.Cursor, "candidates": state.Candidates,
		}})

		log.Printf("[matching] ride %s matched with AV %s ✓", resp.RideID, resp.AVID)
		return nil
	}

	// Rejected
	middleware.DispatchRejections.Inc()
	log.Printf("[matching] AV %s rejected ride %s: %s", resp.AVID, resp.RideID, resp.Reason)
	state.Cursor++
	s.saveState(ctx, state)

	s.broadcast(ws.Event{Type: "match_update", Payload: map[string]interface{}{
		"ride_id": resp.RideID, "status": "SEARCHING", "rejected_av": resp.AVID,
		"cursor": state.Cursor, "candidates": state.Candidates,
	}})

	go func() {
		time.Sleep(100 * time.Millisecond)
		s.dispatchNext(context.Background(), resp.RideID, models.Location{})
	}()
	return nil
}

func (s *Service) GetMatchState(ctx context.Context, rideID string) (*models.MatchingState, error) {
	data, err := s.rdb.Get(ctx, matchPrefix+rideID).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state models.MatchingState
	json.Unmarshal(data, &state)
	return &state, nil
}

func (s *Service) GetAllMatchStates(ctx context.Context) ([]models.MatchingState, error) {
	keys, err := s.rdb.Keys(ctx, matchPrefix+"*").Result()
	if err != nil {
		return nil, err
	}
	var states []models.MatchingState
	for _, k := range keys {
		data, err := s.rdb.Get(ctx, k).Bytes()
		if err != nil {
			continue
		}
		var st models.MatchingState
		json.Unmarshal(data, &st)
		states = append(states, st)
	}
	return states, nil
}

func (s *Service) saveState(ctx context.Context, state *models.MatchingState) {
	data, _ := json.Marshal(state)
	s.rdb.Set(ctx, matchPrefix+state.RideID, data, 5*time.Minute)
}
