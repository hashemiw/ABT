package database

import (
	"abt/backend/internal/config"
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, c config.Config) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(c.DatabaseURL)
	// Driver errors may contain credentials; never include them in operational logs.
	if err != nil {
		return nil, fmt.Errorf("invalid database connection configuration")
	}
	pc.MaxConns = c.DBMaxConns
	pc.ConnConfig.ConnectTimeout = c.DBTimeout
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create database pool failed")
	}
	pingCtx, cancel := context.WithTimeout(ctx, c.DBTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database startup readiness failed")
	}
	return pool, nil
}
