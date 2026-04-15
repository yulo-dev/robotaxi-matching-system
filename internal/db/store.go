package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robotaxi/internal/models"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(connStr string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) RunMigrations(ctx context.Context) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS fares (
		id UUID PRIMARY KEY,
		rider_id TEXT NOT NULL,
		source_lat DOUBLE PRECISION NOT NULL,
		source_lng DOUBLE PRECISION NOT NULL,
		dest_lat DOUBLE PRECISION NOT NULL,
		dest_lng DOUBLE PRECISION NOT NULL,
		price NUMERIC(10,2) NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		expires_at TIMESTAMPTZ NOT NULL
	);

	CREATE TABLE IF NOT EXISTS rides (
		id UUID PRIMARY KEY,
		fare_id UUID NOT NULL REFERENCES fares(id),
		rider_id TEXT NOT NULL,
		source_lat DOUBLE PRECISION NOT NULL,
		source_lng DOUBLE PRECISION NOT NULL,
		dest_lat DOUBLE PRECISION NOT NULL,
		dest_lng DOUBLE PRECISION NOT NULL,
		status TEXT NOT NULL DEFAULT 'REQUESTED',
		av_id TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS avs (
		id TEXT PRIMARY KEY,
		model TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'AVAILABLE',
		battery_pct DOUBLE PRECISION NOT NULL DEFAULT 100,
		lat DOUBLE PRECISION NOT NULL DEFAULT 0,
		lng DOUBLE PRECISION NOT NULL DEFAULT 0,
		last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS uniq_active_ride_per_av
		ON rides(av_id)
		WHERE status IN ('DRIVER_ASSIGNED', 'IN_PROGRESS');
	`
	_, err := s.pool.Exec(ctx, ddl)
	return err
}

// --- Fare ---

func (s *Store) CreateFare(ctx context.Context, f *models.Fare) error {
	f.ID = uuid.New()
	f.CreatedAt = time.Now()
	f.ExpiresAt = f.CreatedAt.Add(10 * time.Minute)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO fares (id, rider_id, source_lat, source_lng, dest_lat, dest_lng, price, created_at, expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		f.ID, f.RiderID, f.Source.Lat, f.Source.Lng, f.Destination.Lat, f.Destination.Lng,
		f.Price, f.CreatedAt, f.ExpiresAt,
	)
	return err
}

func (s *Store) GetFare(ctx context.Context, id uuid.UUID) (*models.Fare, error) {
	f := &models.Fare{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, rider_id, source_lat, source_lng, dest_lat, dest_lng, price, created_at, expires_at
		 FROM fares WHERE id = $1`, id,
	).Scan(&f.ID, &f.RiderID, &f.Source.Lat, &f.Source.Lng, &f.Destination.Lat, &f.Destination.Lng,
		&f.Price, &f.CreatedAt, &f.ExpiresAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return f, err
}

// --- Ride ---

func (s *Store) CreateRide(ctx context.Context, r *models.Ride) error {
	r.ID = uuid.New()
	r.Status = models.RideStatusRequested
	r.CreatedAt = time.Now()
	r.UpdatedAt = r.CreatedAt
	_, err := s.pool.Exec(ctx,
		`INSERT INTO rides (id, fare_id, rider_id, source_lat, source_lng, dest_lat, dest_lng, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		r.ID, r.FareID, r.RiderID, r.Source.Lat, r.Source.Lng, r.Destination.Lat, r.Destination.Lng,
		r.Status, r.CreatedAt, r.UpdatedAt,
	)
	return err
}

func (s *Store) UpdateRideStatus(ctx context.Context, rideID uuid.UUID, status models.RideStatus, avID *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE rides SET status = $2, av_id = $3, updated_at = NOW() WHERE id = $1`,
		rideID, status, avID,
	)
	return err
}

func (s *Store) AssignAVToRide(ctx context.Context, rideID uuid.UUID, avID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE rides SET status = 'DRIVER_ASSIGNED', av_id = $2, updated_at = NOW()
		 WHERE id = $1 AND status IN ('REQUESTED', 'MATCHING')`,
		rideID, avID,
	)
	if err != nil {
		return fmt.Errorf("assign av: %w (possible unique index violation — AV %s already assigned)", avID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("ride %s not in assignable state", rideID)
	}
	return nil
}

func (s *Store) GetRecentRides(ctx context.Context, limit int) ([]models.Ride, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, fare_id, rider_id, source_lat, source_lng, dest_lat, dest_lng, status, av_id, created_at, updated_at
		 FROM rides ORDER BY created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rides []models.Ride
	for rows.Next() {
		var r models.Ride
		err := rows.Scan(&r.ID, &r.FareID, &r.RiderID, &r.Source.Lat, &r.Source.Lng,
			&r.Destination.Lat, &r.Destination.Lng, &r.Status, &r.AVID, &r.CreatedAt, &r.UpdatedAt)
		if err != nil {
			return nil, err
		}
		rides = append(rides, r)
	}
	return rides, nil
}

// --- AV ---

func (s *Store) UpsertAV(ctx context.Context, av *models.AV) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO avs (id, model, status, battery_pct, lat, lng, last_seen)
		 VALUES ($1,$2,$3,$4,$5,$6,NOW())
		 ON CONFLICT (id) DO UPDATE SET status=$3, battery_pct=$4, lat=$5, lng=$6, last_seen=NOW()`,
		av.ID, av.Model, av.Status, av.BatteryPct, av.Location.Lat, av.Location.Lng,
	)
	return err
}

func (s *Store) UpdateAVStatus(ctx context.Context, avID string, status models.AVStatus) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE avs SET status = $2, last_seen = NOW() WHERE id = $1`,
		avID, status,
	)
	return err
}

func (s *Store) GetAllAVs(ctx context.Context) ([]models.AV, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, model, status, battery_pct, lat, lng, last_seen FROM avs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var avs []models.AV
	for rows.Next() {
		var a models.AV
		err := rows.Scan(&a.ID, &a.Model, &a.Status, &a.BatteryPct, &a.Location.Lat, &a.Location.Lng, &a.LastSeen)
		if err != nil {
			return nil, err
		}
		avs = append(avs, a)
	}
	return avs, nil
}

func (s *Store) GetAVCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT status, COUNT(*) FROM avs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int)
	for rows.Next() {
		var st string
		var c int
		if err := rows.Scan(&st, &c); err != nil {
			return nil, err
		}
		m[st] = c
	}
	return m, nil
}

func (s *Store) GetRideCounts(ctx context.Context) (total, active, matched, failed int, err error) {
	err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM rides`).Scan(&total)
	if err != nil {
		return
	}
	err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM rides WHERE status IN ('DRIVER_ASSIGNED','IN_PROGRESS')`).Scan(&active)
	if err != nil {
		return
	}
	err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM rides WHERE status = 'DRIVER_ASSIGNED'`).Scan(&matched)
	if err != nil {
		return
	}
	err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM rides WHERE status = 'CANCELLED'`).Scan(&failed)
	return
}
