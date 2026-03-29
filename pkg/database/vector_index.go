package database

import (
	"context"
	"fmt"
	"log"
	"math"
)

// EnsureVectorIndexes 检查向量表数据量，自动创建或调整 IVFFlat 索引。
// IVFFlat 的 lists 参数应约等于 sqrt(行数)，最小为 1。
// 此方法在应用启动时调用，是幂等的。
func (db *DB) EnsureVectorIndexes(ctx context.Context) error {
	tables := []struct {
		table     string
		column    string
		indexName string
		dimension int
	}{
		{"knowledge_base", "embedding", "idx_kb_embedding_ivfflat", 1536},
		{"agent_memory", "embedding", "idx_memory_embedding_ivfflat", 1536},
	}

	for _, t := range tables {
		if err := db.ensureIVFFlatIndex(ctx, t.table, t.column, t.indexName, t.dimension); err != nil {
			log.Printf("[DB] Failed to ensure IVFFlat index on %s.%s: %v", t.table, t.column, err)
			// 非致命错误，不中断启动
		}
	}

	return nil
}

func (db *DB) ensureIVFFlatIndex(ctx context.Context, table, column, indexName string, dimension int) error {
	// 1. 检查表是否存在
	var tableExists bool
	err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
		table,
	).Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("check table %s: %w", table, err)
	}
	if !tableExists {
		log.Printf("[DB] Table %s does not exist, skipping index creation", table)
		return nil
	}

	// 2. 统计有向量的行数
	var rowCount int
	err = db.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL`, table, column),
	).Scan(&rowCount)
	if err != nil {
		return fmt.Errorf("count rows in %s: %w", table, err)
	}

	log.Printf("[DB] Table %s has %d rows with embeddings", table, rowCount)

	// 3. IVFFlat 至少需要 1 行数据，lists 不能超过行数
	if rowCount == 0 {
		log.Printf("[DB] No embedding data in %s, skipping IVFFlat index", table)
		return nil
	}

	// 计算最优 lists 值：sqrt(行数)，最小 1，最大 1000
	lists := int(math.Sqrt(float64(rowCount)))
	if lists < 1 {
		lists = 1
	}
	if lists > 1000 {
		lists = 1000
	}

	// 4. 检查索引是否已存在
	var indexExists bool
	err = db.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`,
		indexName,
	).Scan(&indexExists)
	if err != nil {
		return fmt.Errorf("check index %s: %w", indexName, err)
	}

	if indexExists {
		// 检查现有索引的 lists 参数是否需要调整
		// 如果数据量增长超过 4 倍（lists^2 * 4 < rowCount），重建索引
		var currentLists int
		err = db.Pool.QueryRow(ctx,
			`SELECT COALESCE(
				(SELECT (regexp_match(indexdef, 'lists\s*=\s*(\d+)'))[1]::int
				 FROM pg_indexes WHERE indexname = $1),
				0)`,
			indexName,
		).Scan(&currentLists)
		if err != nil || currentLists == 0 {
			log.Printf("[DB] IVFFlat index %s already exists, skipping", indexName)
			return nil
		}

		// 如果当前 lists 与最优值差距不大（2 倍以内），不重建
		if lists <= currentLists*2 && lists >= currentLists/2 {
			log.Printf("[DB] IVFFlat index %s (lists=%d) is adequate for %d rows, skipping rebuild",
				indexName, currentLists, rowCount)
			return nil
		}

		// 需要重建
		log.Printf("[DB] Rebuilding IVFFlat index %s: lists %d -> %d (rows: %d)",
			indexName, currentLists, lists, rowCount)
		_, err = db.Pool.Exec(ctx, fmt.Sprintf(`DROP INDEX IF EXISTS %s`, indexName))
		if err != nil {
			return fmt.Errorf("drop old index %s: %w", indexName, err)
		}
	}

	// 5. 创建 IVFFlat 索引
	// 使用 vector_cosine_ops 匹配业务侧的 <=> 余弦距离查询
	createSQL := fmt.Sprintf(
		`CREATE INDEX %s ON %s USING ivfflat (%s vector_cosine_ops) WITH (lists = %d)`,
		indexName, table, column, lists,
	)
	log.Printf("[DB] Creating IVFFlat index: %s", createSQL)

	_, err = db.Pool.Exec(ctx, createSQL)
	if err != nil {
		return fmt.Errorf("create index %s: %w", indexName, err)
	}

	log.Printf("[DB] IVFFlat index %s created successfully (lists=%d, rows=%d)", indexName, lists, rowCount)
	return nil
}

// SetIVFFlatProbes 设置当前连接的 IVFFlat probes 参数。
// probes 越大搜索越精确但越慢，推荐 sqrt(lists)。
func (db *DB) SetIVFFlatProbes(ctx context.Context, probes int) error {
	if probes < 1 {
		probes = 1
	}
	_, err := db.Pool.Exec(ctx, fmt.Sprintf(`SET ivfflat.probes = %d`, probes))
	return err
}
