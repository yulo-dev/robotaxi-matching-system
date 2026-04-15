.PHONY: up down build seed logs test test-unit test-integration

# ─── Lifecycle ───
up:
	docker compose up --build -d
	@echo ""
	@echo "🚕 Robotaxi is running!"
	@echo "   Dashboard:  http://localhost:8080/dashboard/"
	@echo "   API:        http://localhost:8080/api/"
	@echo "   Metrics:    http://localhost:8080/metrics"
	@echo "   WebSocket:  ws://localhost:8080/ws"
	@echo "   Health:     http://localhost:8080/health"
	@echo ""
	@echo "Run 'make seed' to populate demo data."

down:
	docker compose down

build:
	docker compose up --build -d

# ─── Data ───
seed:
	@echo "Waiting 3s for server..."
	@sleep 3
	go run scripts/seed.go

# ─── Tests ───
# Unit tests (no external deps)
test-unit:
	go test ./internal/api/... -v -count=1

# Integration tests (requires Redis on localhost:6379)
test-integration:
	go test ./internal/location/... ./internal/matching/... -v -count=1

# All tests
test: test-unit test-integration

# ─── Dev ───
dev:
	go run cmd/server/main.go

logs:
	docker compose logs -f server

logs-all:
	docker compose logs -f

# ─── Metrics ───
metrics:
	@curl -s http://localhost:8080/metrics | grep ^robotaxi_

# ─── Quick test ride ───
test-ride:
	@echo "Creating fare..."
	@FARE=$$(curl -s -X POST http://localhost:8080/api/fare \
		-H "Content-Type: application/json" \
		-d '{"rider_id":"test-user","pickup_location":{"lat":37.7749,"lng":-122.4194},"destination":{"lat":37.7849,"lng":-122.4094}}'); \
	echo "$$FARE" | python3 -m json.tool; \
	FARE_ID=$$(echo "$$FARE" | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])"); \
	echo ""; echo "Requesting ride with fare $$FARE_ID..."; \
	curl -s -X POST http://localhost:8080/api/rides \
		-H "Content-Type: application/json" \
		-d "{\"fare_id\":\"$$FARE_ID\"}" | python3 -m json.tool
