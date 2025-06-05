package tables

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var statementCache *cache

type cache struct {
	cache sync.Map
}

func init() {
	statementCache = &cache{}
	go statementCache.waitForClean()
}

func (c *cache) waitForClean() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.close()
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

func (c *cache) cleanup() {
	c.cache.Range(func(k, v interface{}) bool {
		cs := v.(*CachedStmt)
		if time.Since(cs.lastUsed) > 5*time.Minute {
			cs.stmt.Close()
			c.cache.Delete(k)
		}
		return true
	})
}

func (c *cache) close() {
	c.cache.Range(func(k, v interface{}) bool {
		cs := v.(*CachedStmt)
		cs.stmt.Close()
		c.cache.Delete(k)
		return true
	})
}

type CachedStmt struct {
	stmt     *sql.Stmt
	lastUsed time.Time
}

func (c *cache) GetStatement(db string, table string) (*sql.Stmt, bool) {
	if stat, ok := c.cache.Load(fmt.Sprintf("%s_%s", db, table)); ok {
		cs := stat.(*CachedStmt)
		cs.lastUsed = time.Now()
		return cs.stmt, true
	}
	return nil, false
}

func (c *cache) SetStatement(db string, table string, stmt *sql.Stmt) {
	if stmt == nil {
		return
	}
	c.cache.Store(fmt.Sprintf("%s_%s", db, table), &CachedStmt{
		stmt:     stmt,
		lastUsed: time.Now(),
	})
}
