package inits

import (
	"context"
	"github.com/jackc/pgx/v5"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	m "last_weekend_services/src/models"
)

func BeforeConnect(ctx context.Context, cfg *pgx.ConnConfig) error {
	log.Print("Before Connect")

	return nil
}

func AfterConnect(ctx context.Context, cfg *pgx.Conn) error {
	log.Print("After Connect")

	return nil
}

func BeforeAcquire(context.Context, *pgx.Conn) bool {
	return false
}

func AfterRelease(*pgx.Conn) bool {
	return false
}

func CreatePostgresPool(connString string, context context.Context) (*m.PGPool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	cfg.MaxConns = 8
	cfg.MaxConnIdleTime = 5 * time.Second
	//cfg.BeforeConnect = BeforeConnect
	//cfg.AfterConnect = AfterConnect
	//cfg.BeforeAcquire = BeforeAcquire
	//cfg.AfterRelease = AfterRelease

	pool, err := pgxpool.NewWithConfig(context, cfg)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	return &m.PGPool{Pool: pool}, nil
}
