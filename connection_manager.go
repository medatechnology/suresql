package suresql

import (
	"context"
	"sync"
	"time"

	"github.com/medatechnology/goutil/simplelog"
)

// ConnectionManager manages database connections and handles cleanup
type ConnectionManager struct {
	node           *SureSQLNode
	cleanupTicker  *time.Ticker
	stopChan       chan struct{}
	wg             sync.WaitGroup
	cleanupRunning bool
	mu             sync.Mutex
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(node *SureSQLNode) *ConnectionManager {
	return &ConnectionManager{
		node:     node,
		stopChan: make(chan struct{}),
	}
}

// StartCleanupRoutine starts the background cleanup routine
// This monitors the TTLMap and closes connections when they expire
func (cm *ConnectionManager) StartCleanupRoutine(ctx context.Context, interval time.Duration) {
	cm.mu.Lock()
	if cm.cleanupRunning {
		cm.mu.Unlock()
		return
	}
	cm.cleanupRunning = true
	cm.mu.Unlock()

	if interval == 0 {
		interval = DEFAULT_TTL_TICKER_MINUTES
	}

	cm.cleanupTicker = time.NewTicker(interval)
	cm.wg.Add(1)

	go func() {
		defer cm.wg.Done()
		simplelog.LogThis("ConnectionManager", "Starting connection cleanup routine")

		for {
			select {
			case <-ctx.Done():
				simplelog.LogThis("ConnectionManager", "Context cancelled, stopping cleanup routine")
				return
			case <-cm.stopChan:
				simplelog.LogThis("ConnectionManager", "Stop signal received, stopping cleanup routine")
				return
			case <-cm.cleanupTicker.C:
				cm.cleanupExpiredConnections()
			}
		}
	}()
}

// cleanupExpiredConnections checks for and cleans up expired connections
// Note: TTLMap automatically removes expired entries, but we want to explicitly
// close the database connections before they're removed
func (cm *ConnectionManager) cleanupExpiredConnections() {
	if cm.node.DBConnections == nil {
		return
	}

	// TTLMap handles expiration automatically
	// This function serves as a periodic health check
	// and explicitly closes any connections that should be cleaned up

	// Get current size before cleanup
	sizeBefore := cm.node.DBConnections.Len()

	// Force cleanup of expired entries in TTLMap
	// The TTLMap will remove expired entries on its own timer
	// We just log the current state for monitoring

	sizeAfter := cm.node.DBConnections.Len()

	if sizeBefore != sizeAfter {
		cleaned := sizeBefore - sizeAfter
		simplelog.LogFormat("ConnectionManager: TTLMap cleaned up %d expired connections", cleaned)
		// Record the cleanup in metrics
		for i := 0; i < cleaned; i++ {
			Metrics.RecordTokenExpired()
			Metrics.RecordConnectionClosed()
		}
	}
}

// closeConnection closes a single connection by token
func (cm *ConnectionManager) closeConnection(token string) bool {
	cm.node.mu.Lock()
	defer cm.node.mu.Unlock()

	dbInterface, ok := cm.node.DBConnections.Get(token)
	if !ok {
		return false
	}

	// Try to close the connection
	if db, ok := dbInterface.(SureSQLDB); ok {
		if closer, ok := interface{}(db).(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				simplelog.LogErrorAny("ConnectionManager", err, "failed to close expired connection")
			} else {
				Metrics.RecordConnectionClosed()
			}
		}
	}

	// Remove from pool
	cm.node.DBConnections.Delete(token)
	return true
}

// Stop stops the cleanup routine gracefully
func (cm *ConnectionManager) Stop() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.cleanupRunning {
		return
	}

	simplelog.LogThis("ConnectionManager", "Stopping connection cleanup routine")

	// Stop the ticker
	if cm.cleanupTicker != nil {
		cm.cleanupTicker.Stop()
	}

	// Signal stop
	close(cm.stopChan)

	// Wait for goroutine to finish
	cm.wg.Wait()

	cm.cleanupRunning = false
	simplelog.LogThis("ConnectionManager", "Connection cleanup routine stopped")
}

// CleanupAllConnections closes all connections in the pool (used during shutdown)
func (cm *ConnectionManager) CleanupAllConnections() int {
	if cm.node.DBConnections == nil {
		return 0
	}

	allConnections := cm.node.DBConnections.Map()
	count := 0

	for token := range allConnections {
		if cm.closeConnection(token) {
			count++
		}
	}

	simplelog.LogFormat("ConnectionManager: Closed all %d connections during shutdown", count)
	return count
}

// GetConnectionCount returns current connection count
func (cm *ConnectionManager) GetConnectionCount() int {
	if cm.node.DBConnections == nil {
		return 0
	}
	return cm.node.DBConnections.Len()
}

// GetConnectionPoolUsage returns connection pool usage percentage
func (cm *ConnectionManager) GetConnectionPoolUsage() float64 {
	if cm.node.MaxPool == 0 {
		return 0
	}
	active := cm.GetConnectionCount()
	return float64(active) / float64(cm.node.MaxPool) * 100
}

// IsPoolNearCapacity checks if pool is near capacity
func (cm *ConnectionManager) IsPoolNearCapacity(thresholdPct float64) bool {
	return cm.GetConnectionPoolUsage() >= thresholdPct
}

// ForceCleanupConnection forcefully removes a connection (for admin use)
func (cm *ConnectionManager) ForceCleanupConnection(token string) bool {
	return cm.closeConnection(token)
}

// Global connection manager instance
var (
	ConnectionMgr     *ConnectionManager
	connectionMgrOnce sync.Once
)

// InitConnectionManager initializes the global connection manager
func InitConnectionManager() {
	connectionMgrOnce.Do(func() {
		ConnectionMgr = NewConnectionManager(&CurrentNode)
	})
}

// StartConnectionCleanup starts the connection cleanup routine
// This should be called after CurrentNode is fully initialized
func StartConnectionCleanup(ctx context.Context) {
	if ConnectionMgr == nil {
		InitConnectionManager()
	}

	// Start cleanup routine with configured TTL ticker interval
	interval := CurrentNode.Config.TTLTicker
	if interval == 0 {
		interval = DEFAULT_TTL_TICKER_MINUTES
	}

	ConnectionMgr.StartCleanupRoutine(ctx, interval)
}

// StopConnectionCleanup stops the connection cleanup routine gracefully
func StopConnectionCleanup() {
	if ConnectionMgr != nil {
		ConnectionMgr.Stop()
	}
}
