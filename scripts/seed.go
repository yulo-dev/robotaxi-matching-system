package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

type AV struct {
	ID         string  `json:"id"`
	Model      string  `json:"model"`
	Status     string  `json:"status"`
	BatteryPct float64 `json:"battery_pct"`
	Location   struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"location"`
}

func main() {
	rand.Seed(time.Now().UnixNano())
	baseURL := "http://localhost:8080/api"

	models := []string{"Cybercab", "Model C", "Model X AV", "Model S AV", "Model 3 AV"}
	statuses := []string{"AVAILABLE", "AVAILABLE", "AVAILABLE", "AVAILABLE", "EN_ROUTE", "CHARGING"}

	// SF area
	centerLat, centerLng := 37.7749, -122.4194

	fmt.Println("🌱 Seeding 50 AVs...")
	for i := 1; i <= 50; i++ {
		av := AV{
			ID:         fmt.Sprintf("AV-%04d", i),
			Model:      models[rand.Intn(len(models))],
			Status:     statuses[rand.Intn(len(statuses))],
			BatteryPct: 30 + rand.Float64()*70,
		}
		av.Location.Lat = centerLat + (rand.Float64()-0.5)*0.08
		av.Location.Lng = centerLng + (rand.Float64()-0.5)*0.08

		body, _ := json.Marshal(av)
		resp, err := http.Post(baseURL+"/av/register", "application/json", bytes.NewReader(body))
		if err != nil {
			fmt.Printf("  ✗ AV-%04d: %v\n", i, err)
			continue
		}
		resp.Body.Close()
		fmt.Printf("  ✓ %s (%s) — %.0f%% battery\n", av.ID, av.Status, av.BatteryPct)
	}

	// Create a sample fare + ride
	fmt.Println("\n🚕 Creating sample fare...")
	fareBody := map[string]interface{}{
		"rider_id": "user-001",
		"pickup_location": map[string]float64{"lat": 37.7749, "lng": -122.4194},
		"destination":     map[string]float64{"lat": 37.7849, "lng": -122.4094},
	}
	b, _ := json.Marshal(fareBody)
	resp, err := http.Post(baseURL+"/fare", "application/json", bytes.NewReader(b))
	if err != nil {
		fmt.Printf("  ✗ fare: %v\n", err)
		return
	}
	defer resp.Body.Close()
	var fare map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&fare)
	fareID := fare["id"].(string)
	fmt.Printf("  ✓ Fare %s — $%.2f\n", fareID, fare["price"])

	fmt.Println("\n🚕 Requesting ride...")
	rideBody := map[string]string{"fare_id": fareID}
	b, _ = json.Marshal(rideBody)
	resp2, err := http.Post(baseURL+"/rides", "application/json", bytes.NewReader(b))
	if err != nil {
		fmt.Printf("  ✗ ride: %v\n", err)
		return
	}
	defer resp2.Body.Close()
	var ride map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&ride)
	fmt.Printf("  ✓ Ride %s — status: %s\n", ride["id"], ride["status"])

	fmt.Println("\n✅ Seed complete! Visit http://localhost:8080/dashboard/")
}
