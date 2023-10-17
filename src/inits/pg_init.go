package inits

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	m "last_weekend_services/src/models"
)

func CreatePostgresPool(connString string, context context.Context) (*m.PGPool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(context, cfg)
	if err != nil {
		log.Print(err)
		return nil, err
	}

	return &m.PGPool{Pool: pool}, nil
}
