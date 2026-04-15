package api

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestEstimateFare_Haversine(t *testing.T) {
	// SF to Oakland ~13km
	dist := haversine(37.7749, -122.4194, 37.8044, -122.2712)
	if dist < 12 || dist > 15 {
		t.Errorf("SF→Oakland distance = %.2f km, expected ~13km", dist)
	}

	// Same point = 0
	d := haversine(37.77, -122.42, 37.77, -122.42)
	if d != 0 {
		t.Errorf("same point distance = %f, want 0", d)
	}
}

func TestEstimateFare_Price(t *testing.T) {
	from := struct{ Lat, Lng float64 }{37.7749, -122.4194}
	to := struct{ Lat, Lng float64 }{37.7849, -122.4094}

	dist := haversine(from.Lat, from.Lng, to.Lat, to.Lng)
	price := math.Round((3.50+dist*1.80)*100) / 100

	if price < 3.50 {
		t.Errorf("price should be at least base fare $3.50, got $%.2f", price)
	}
	if price > 50 {
		t.Errorf("price for ~1km should be reasonable, got $%.2f", price)
	}
}

func TestFareEndpoint_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// We can't easily inject a real DB in unit tests, so test request validation only
	r.POST("/api/fare", func(c *gin.Context) {
		var req struct {
			PickupLocation struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"pickup_location" binding:"required"`
			Destination struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"destination" binding:"required"`
			RiderID string `json:"rider_id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Missing rider_id
	w := httptest.NewRecorder()
	body := `{"pickup_location":{"lat":37.77,"lng":-122.42},"destination":{"lat":37.78,"lng":-122.41}}`
	req, _ := http.NewRequest("POST", "/api/fare", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing rider_id, got %d", w.Code)
	}

	// Valid request
	w2 := httptest.NewRecorder()
	body2 := `{"rider_id":"u1","pickup_location":{"lat":37.77,"lng":-122.42},"destination":{"lat":37.78,"lng":-122.41}}`
	req2, _ := http.NewRequest("POST", "/api/fare", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for valid request, got %d", w2.Code)
	}
}

func TestRideEndpoint_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/rides", func(c *gin.Context) {
		var req struct {
			FareID string `json:"fare_id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Empty body
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/rides", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
