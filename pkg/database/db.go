package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds database connection parameters.
type Config struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
	MaxConns int32  `yaml:"max_conns"`
	MinConns int32  `yaml:"min_conns"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "",
		DBName:   "aicorp",
		SSLMode:  "disable",
		MaxConns: 20,
		MinConns: 2,
	}
}

// DB wraps a pgxpool connection pool and provides domain-specific repositories.
type DB struct {
	Pool    *pgxpool.Pool
	Agents  *AgentRepo
	Tasks   *TaskRepo
	KB      *KnowledgeBaseRepo
	Metrics *MetricsRepo
	Models  *ModelRegistryRepo
	Audit   *AuditRepo
}

// New creates a new DB instance with connection pool and all repositories.
func New(ctx context.Context, cfg Config) (*DB, error) {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName, cfg.SSLMode,
	)

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	db := &DB{
		Pool:    pool,
		Agents:  &AgentRepo{pool: pool},
		Tasks:   &TaskRepo{pool: pool},
		KB:      &KnowledgeBaseRepo{pool: pool},
		Metrics: &MetricsRepo{pool: pool},
		Models:  &ModelRegistryRepo{pool: pool},
		Audit:   &AuditRepo{pool: pool},
	}

	return db, nil
}

// Close closes the connection pool.
func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

// HealthCheck verifies the database is reachable and returns basic stats.
func (db *DB) HealthCheck(ctx context.Context) (map[string]interface{}, error) {
	start := time.Now()
	if err := db.Pool.Ping(ctx); err != nil {
		return nil, err
	}
	latency := time.Since(start)

	stat := db.Pool.Stat()
	return map[string]interface{}{
		"status":           "healthy",
		"latency_ms":       latency.Milliseconds(),
		"total_conns":      stat.TotalConns(),
		"idle_conns":       stat.IdleConns(),
		"acquired_conns":   stat.AcquiredConns(),
		"max_conns":        stat.MaxConns(),
	}, nil
}
