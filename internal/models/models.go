package models

import (
	"time"

	"github.com/google/uuid"
)

// --- Enums ---

type RideStatus string

const (
	RideStatusRequested      RideStatus = "REQUESTED"
	RideStatusMatching       RideStatus = "MATCHING"
	RideStatusDriverAssigned RideStatus = "DRIVER_ASSIGNED"
	RideStatusInProgress     RideStatus = "IN_PROGRESS"
	RideStatusCompleted      RideStatus = "COMPLETED"
	RideStatusCancelled      RideStatus = "CANCELLED"
)

type AVStatus string

const (
	AVStatusAvailable  AVStatus = "AVAILABLE"
	AVStatusEnRoute    AVStatus = "EN_ROUTE"
	AVStatusInProgress AVStatus = "IN_PROGRESS"
	AVStatusCharging   AVStatus = "CHARGING"
	AVStatusOffline    AVStatus = "OFFLINE"
)

type MatchStatus string

const (
	MatchSearching MatchStatus = "SEARCHING"
	MatchDone      MatchStatus = "DONE"
	MatchFailed    MatchStatus = "FAILED"
)

type DispatchDecision string

const (
	DecisionAccept DispatchDecision = "ACCEPT"
	DecisionReject DispatchDecision = "REJECT"
)

// --- DB Models ---

type Location struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type Fare struct {
	ID          uuid.UUID `json:"id"`
	RiderID     string    `json:"rider_id"`
	Source      Location  `json:"source"`
	Destination Location  `json:"destination"`
	Price       float64   `json:"price"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Ride struct {
	ID          uuid.UUID  `json:"id"`
	FareID      uuid.UUID  `json:"fare_id"`
	RiderID     string     `json:"rider_id"`
	Source      Location   `json:"source"`
	Destination Location   `json:"destination"`
	Status      RideStatus `json:"status"`
	AVID        *string    `json:"av_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type AV struct {
	ID         string   `json:"id"`
	Model      string   `json:"model"`
	Status     AVStatus `json:"status"`
	BatteryPct float64  `json:"battery_pct"`
	Location   Location `json:"location"`
	LastSeen   time.Time `json:"last_seen"`
}

// --- API Request/Response ---

type FareRequest struct {
	PickupLocation Location `json:"pickup_location" binding:"required"`
	Destination    Location `json:"destination" binding:"required"`
	RiderID        string   `json:"rider_id" binding:"required"`
}

type RideRequest struct {
	FareID string `json:"fare_id" binding:"required"`
}

type DispatchResponse struct {
	RideID   string           `json:"ride_id"`
	AVID     string           `json:"av_id"`
	Decision DispatchDecision `json:"decision"`
	Reason   string           `json:"reason,omitempty"`
}

type AVLocationUpdate struct {
	AVID     string   `json:"av_id" binding:"required"`
	Location Location `json:"location" binding:"required"`
	Status   AVStatus `json:"status"`
	Battery  float64  `json:"battery_pct"`
}

// --- Matching State (stored in Redis) ---

type MatchingState struct {
	RideID     string      `json:"ride_id"`
	Candidates []string    `json:"candidates"`
	Cursor     int         `json:"cursor"`
	Status     MatchStatus `json:"status"`
}

// --- Dashboard Stats ---

type DashboardStats struct {
	TotalAVs       int            `json:"total_avs"`
	AVsByStatus    map[string]int `json:"avs_by_status"`
	ActiveRides    int            `json:"active_rides"`
	TotalFares     int            `json:"total_fares"`
	MatchRate      float64        `json:"match_rate"`
	RecentRides    []Ride         `json:"recent_rides"`
	RecentMatches  []MatchingState `json:"recent_matches"`
}
