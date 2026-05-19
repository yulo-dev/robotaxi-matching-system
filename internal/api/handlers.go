package api 

import (
	"math"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/robotaxi/internal/db"
	"github.com/robotaxi/internal/location"
	"github.com/robotaxi/internal/matching"
	"github.com/robotaxi/internal/models"
)

type Handler struct {
	store    *db.Store
	locSvc   *location.Service
	matchSvc *matching.Service
}

func NewHandler(store *db.Store, locSvc *location.Service, matchSvc *matching.Service) *Handler {
	return &Handler{store: store, locSvc: locSvc, matchSvc: matchSvc}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api")
	{
		api.POST("/fare", h.CreateFare)
		api.POST("/rides", h.CreateRide)
		api.POST("/dispatch/decision", h.HandleDispatchDecision)
		api.POST("/av/location", h.UpdateAVLocation)
		api.POST("/av/register", h.RegisterAV)
		api.GET("/rides", h.ListRides)
		api.GET("/avs", h.ListAVs)
		api.GET("/dashboard/stats", h.DashboardStats)
		api.GET("/matching/states", h.ListMatchStates)
	}
}

// POST /api/fare — get fare estimate
func (h *Handler) CreateFare(c *gin.Context) {
	var req models.FareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	price := estimateFare(req.PickupLocation, req.Destination)
	fare := &models.Fare{
		RiderID:     req.RiderID,
		Source:      req.PickupLocation,
		Destination: req.Destination,
		Price:       price,
	}
	if err := h.store.CreateFare(c.Request.Context(), fare); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, fare)
}

// POST /api/rides — request a ride
func (h *Handler) CreateRide(c *gin.Context) {
	var req models.RideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fareID, err := uuid.Parse(req.FareID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid fare_id"})
		return
	}

	fare, err := h.store.GetFare(c.Request.Context(), fareID)
	if err != nil || fare == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "fare not found"})
		return
	}

	ride := &models.Ride{
		FareID:      fareID,
		RiderID:     fare.RiderID,
		Source:      fare.Source,
		Destination: fare.Destination,
	}
	if err := h.store.CreateRide(c.Request.Context(), ride); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Trigger matching asynchronously
	go h.matchSvc.StartMatching(c.Request.Context(), ride)

	c.JSON(http.StatusCreated, ride)
}

// POST /api/dispatch/decision — AV accepts or rejects
func (h *Handler) HandleDispatchDecision(c *gin.Context) {
	var resp models.DispatchResponse
	if err := c.ShouldBindJSON(&resp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.matchSvc.HandleDispatchDecision(c.Request.Context(), resp); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// POST /api/av/location — AV sends location update
func (h *Handler) UpdateAVLocation(c *gin.Context) {
	var req models.AVLocationUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.locSvc.UpdateLocation(c.Request.Context(), req.AVID, req.Location); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.Status != "" {
		h.locSvc.SetAVStatus(c.Request.Context(), req.AVID, req.Status, req.Battery)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// POST /api/av/register — register a new AV
func (h *Handler) RegisterAV(c *gin.Context) {
	var av models.AV
	if err := c.ShouldBindJSON(&av); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.UpsertAV(c.Request.Context(), &av); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Also add to location cache
	h.locSvc.UpdateLocation(c.Request.Context(), av.ID, av.Location)
	h.locSvc.SetAVStatus(c.Request.Context(), av.ID, av.Status, av.BatteryPct)
	c.JSON(http.StatusCreated, av)
}

// GET /api/rides
func (h *Handler) ListRides(c *gin.Context) {
	rides, err := h.store.GetRecentRides(c.Request.Context(), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rides)
}

// GET /api/avs
func (h *Handler) ListAVs(c *gin.Context) {
	avs, err := h.store.GetAllAVs(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, avs)
}

// GET /api/dashboard/stats
func (h *Handler) DashboardStats(c *gin.Context) {
	ctx := c.Request.Context()
	avCounts, _ := h.store.GetAVCounts(ctx)
	total, active, matched, failed, _ := h.store.GetRideCounts(ctx)
	rides, _ := h.store.GetRecentRides(ctx, 20)
	matches, _ := h.matchSvc.GetAllMatchStates(ctx)

	totalAVs := 0
	for _, v := range avCounts {
		totalAVs += v
	}

	matchRate := 0.0
	if total > 0 {
		matchRate = float64(matched) / float64(total) * 100
	}

	c.JSON(http.StatusOK, models.DashboardStats{
		TotalAVs:      totalAVs,
		AVsByStatus:   avCounts,
		ActiveRides:   active,
		TotalFares:    total,
		MatchRate:     matchRate,
		RecentRides:   rides,
		RecentMatches: matches,
	})
	_ = failed
}

// GET /api/matching/states
func (h *Handler) ListMatchStates(c *gin.Context) {
	states, err := h.matchSvc.GetAllMatchStates(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, states)
}

// --- Helpers ---

func estimateFare(from, to models.Location) float64 {
	dist := haversine(from.Lat, from.Lng, to.Lat, to.Lng)
	base := 3.50
	perKm := 1.80
	return math.Round((base+dist*perKm)*100) / 100
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
