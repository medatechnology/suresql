package suresql

import (
	"sync"
	"sync/atomic"
	"time"
)

// NodeMetrics tracks runtime metrics for the SureSQL node
type NodeMetrics struct {
	mu sync.RWMutex

	// Connection Pool Metrics
	ConnectionsCreated      uint64    `json:"connections_created"`       // Total connections created
	ConnectionsClosed       uint64    `json:"connections_closed"`        // Total connections closed
	ConnectionsActive       int       `json:"connections_active"`        // Current active connections
	ConnectionPoolSize      int       `json:"connection_pool_size"`      // Max pool size
	ConnectionPoolUsagePct  float64   `json:"connection_pool_usage_pct"` // Usage percentage
	PoolExhaustionCount     uint64    `json:"pool_exhaustion_count"`     // Times pool was full
	LastPoolExhaustion      time.Time `json:"last_pool_exhaustion"`      // Last time pool was full

	// Token Store Metrics
	TokensActive            int       `json:"tokens_active"`             // Active tokens
	TokensCreated           uint64    `json:"tokens_created"`            // Total tokens created
	TokensExpired           uint64    `json:"tokens_expired"`            // Total tokens expired
	RefreshTokensActive     int       `json:"refresh_tokens_active"`     // Active refresh tokens
	RefreshTokensUsed       uint64    `json:"refresh_tokens_used"`       // Total refresh tokens used

	// Request Metrics
	TotalRequests           uint64    `json:"total_requests"`            // Total API requests
	FailedRequests          uint64    `json:"failed_requests"`           // Failed API requests
	AuthenticationAttempts  uint64    `json:"authentication_attempts"`   // Total auth attempts
	AuthenticationFailures  uint64    `json:"authentication_failures"`   // Failed auth attempts

	// Database Metrics
	QueriesExecuted         uint64    `json:"queries_executed"`          // Total queries
	QueriesSuccess          uint64    `json:"queries_success"`           // Successful queries
	QueriesFailed           uint64    `json:"queries_failed"`            // Failed queries
	AverageQueryTime        float64   `json:"average_query_time_ms"`     // Average query time in ms

	// System Metrics
	StartTime               time.Time `json:"start_time"`                // Server start time
	Uptime                  string    `json:"uptime"`                    // Human readable uptime
}

// Global metrics instance
var (
	Metrics     *NodeMetrics
	metricsOnce sync.Once
)

// InitMetrics initializes the global metrics instance
func InitMetrics() {
	metricsOnce.Do(func() {
		Metrics = &NodeMetrics{
			StartTime: time.Now(),
		}
	})
}

// GetMetrics returns a snapshot of current metrics (thread-safe)
func GetMetrics() NodeMetrics {
	if Metrics == nil {
		InitMetrics()
	}

	Metrics.mu.RLock()
	defer Metrics.mu.RUnlock()

	snapshot := *Metrics
	snapshot.Uptime = time.Since(Metrics.StartTime).String()

	// Calculate current values from CurrentNode
	if CurrentNode.DBConnections != nil {
		snapshot.ConnectionsActive = CurrentNode.DBConnections.Len()
		snapshot.ConnectionPoolSize = CurrentNode.MaxPool
		if CurrentNode.MaxPool > 0 {
			snapshot.ConnectionPoolUsagePct = float64(snapshot.ConnectionsActive) / float64(CurrentNode.MaxPool) * 100
		}
	}

	return snapshot
}

// RecordConnectionCreated increments connection creation counter
func (m *NodeMetrics) RecordConnectionCreated() {
	atomic.AddUint64(&m.ConnectionsCreated, 1)
}

// RecordConnectionClosed increments connection closed counter
func (m *NodeMetrics) RecordConnectionClosed() {
	atomic.AddUint64(&m.ConnectionsClosed, 1)
}

// RecordPoolExhaustion records when connection pool is full
func (m *NodeMetrics) RecordPoolExhaustion() {
	atomic.AddUint64(&m.PoolExhaustionCount, 1)
	m.mu.Lock()
	m.LastPoolExhaustion = time.Now()
	m.mu.Unlock()
}

// RecordTokenCreated increments token creation counter
func (m *NodeMetrics) RecordTokenCreated() {
	atomic.AddUint64(&m.TokensCreated, 1)
}

// RecordTokenExpired increments token expiration counter
func (m *NodeMetrics) RecordTokenExpired() {
	atomic.AddUint64(&m.TokensExpired, 1)
}

// RecordRefreshTokenUsed increments refresh token usage counter
func (m *NodeMetrics) RecordRefreshTokenUsed() {
	atomic.AddUint64(&m.RefreshTokensUsed, 1)
}

// RecordRequest increments total request counter
func (m *NodeMetrics) RecordRequest(success bool) {
	atomic.AddUint64(&m.TotalRequests, 1)
	if !success {
		atomic.AddUint64(&m.FailedRequests, 1)
	}
}

// RecordAuthentication records authentication attempt
func (m *NodeMetrics) RecordAuthentication(success bool) {
	atomic.AddUint64(&m.AuthenticationAttempts, 1)
	if !success {
		atomic.AddUint64(&m.AuthenticationFailures, 1)
	}
}

// RecordQuery records query execution
func (m *NodeMetrics) RecordQuery(success bool, durationMs float64) {
	atomic.AddUint64(&m.QueriesExecuted, 1)
	if success {
		atomic.AddUint64(&m.QueriesSuccess, 1)
	} else {
		atomic.AddUint64(&m.QueriesFailed, 1)
	}

	// Update average query time (simple moving average)
	m.mu.Lock()
	if m.AverageQueryTime == 0 {
		m.AverageQueryTime = durationMs
	} else {
		// Exponential moving average (alpha = 0.1)
		m.AverageQueryTime = 0.9*m.AverageQueryTime + 0.1*durationMs
	}
	m.mu.Unlock()
}

// GetConnectionPoolStats returns connection pool statistics
func GetConnectionPoolStats() map[string]interface{} {
	if Metrics == nil {
		InitMetrics()
	}

	active := 0
	maxPool := 0
	if CurrentNode.DBConnections != nil {
		active = CurrentNode.DBConnections.Len()
		maxPool = CurrentNode.MaxPool
	}

	usagePct := 0.0
	if maxPool > 0 {
		usagePct = float64(active) / float64(maxPool) * 100
	}

	return map[string]interface{}{
		"active_connections":     active,
		"max_pool_size":          maxPool,
		"usage_percentage":       usagePct,
		"total_created":          atomic.LoadUint64(&Metrics.ConnectionsCreated),
		"total_closed":           atomic.LoadUint64(&Metrics.ConnectionsClosed),
		"pool_exhaustion_count":  atomic.LoadUint64(&Metrics.PoolExhaustionCount),
		"last_exhaustion":        Metrics.LastPoolExhaustion.Format(time.RFC3339),
		"available_slots":        maxPool - active,
	}
}

// GetTokenStats returns token statistics
func GetTokenStats() map[string]interface{} {
	if Metrics == nil {
		InitMetrics()
	}

	return map[string]interface{}{
		"tokens_active":         Metrics.TokensActive,
		"tokens_created":        atomic.LoadUint64(&Metrics.TokensCreated),
		"tokens_expired":        atomic.LoadUint64(&Metrics.TokensExpired),
		"refresh_tokens_active": Metrics.RefreshTokensActive,
		"refresh_tokens_used":   atomic.LoadUint64(&Metrics.RefreshTokensUsed),
	}
}

// IsConnectionPoolNearExhaustion returns true if pool usage is above threshold
func IsConnectionPoolNearExhaustion(thresholdPct float64) bool {
	if CurrentNode.DBConnections == nil || CurrentNode.MaxPool == 0 {
		return false
	}

	active := CurrentNode.DBConnections.Len()
	usagePct := float64(active) / float64(CurrentNode.MaxPool) * 100

	return usagePct >= thresholdPct
}

// GetHealthStatus returns overall health status
func GetHealthStatus() map[string]interface{} {
	metrics := GetMetrics()

	// Determine health status
	status := "healthy"
	issues := []string{}

	// Check connection pool
	if metrics.ConnectionPoolUsagePct >= 90 {
		status = "degraded"
		issues = append(issues, "connection pool near capacity")
	}

	// Check authentication failure rate
	if metrics.AuthenticationAttempts > 0 {
		failureRate := float64(metrics.AuthenticationFailures) / float64(metrics.AuthenticationAttempts) * 100
		if failureRate > 50 {
			status = "degraded"
			issues = append(issues, "high authentication failure rate")
		}
	}

	// Check query failure rate
	if metrics.QueriesExecuted > 0 {
		failureRate := float64(metrics.QueriesFailed) / float64(metrics.QueriesExecuted) * 100
		if failureRate > 10 {
			status = "unhealthy"
			issues = append(issues, "high query failure rate")
		}
	}

	// Check if database is connected
	if !CurrentNode.InternalConnection.IsConnected() {
		status = "unhealthy"
		issues = append(issues, "database not connected")
	}

	return map[string]interface{}{
		"status":     status,
		"issues":     issues,
		"uptime":     time.Since(metrics.StartTime).String(),
		"start_time": metrics.StartTime.Format(time.RFC3339),
	}
}
