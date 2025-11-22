package suresql

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/medatechnology/goutil/simplelog"
)

// AlertLevel represents the severity of an alert
type AlertLevel string

const (
	AlertLevelInfo     AlertLevel = "INFO"
	AlertLevelWarning  AlertLevel = "WARNING"
	AlertLevelCritical AlertLevel = "CRITICAL"
)

// Alert represents a system alert
type Alert struct {
	Level     AlertLevel `json:"level"`
	Title     string     `json:"title"`
	Message   string     `json:"message"`
	Timestamp time.Time  `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// AlertManager manages system alerts and notifications
type AlertManager struct {
	mu                    sync.RWMutex
	alerts                []Alert
	maxAlerts             int
	poolWarningThreshold  float64 // Percentage
	poolCriticalThreshold float64 // Percentage
	checkInterval         time.Duration
	ticker                *time.Ticker
	stopChan              chan struct{}
	wg                    sync.WaitGroup
	running               bool

	// Cooldown to prevent alert spam
	lastPoolWarning   time.Time
	lastPoolCritical  time.Time
	alertCooldown     time.Duration
}

// NewAlertManager creates a new alert manager
func NewAlertManager() *AlertManager {
	return &AlertManager{
		alerts:                make([]Alert, 0),
		maxAlerts:             100, // Keep last 100 alerts
		poolWarningThreshold:  75.0, // Warn at 75% capacity
		poolCriticalThreshold: 90.0, // Critical at 90% capacity
		checkInterval:         30 * time.Second,
		stopChan:              make(chan struct{}),
		alertCooldown:         5 * time.Minute, // Don't repeat same alert within 5 mins
	}
}

// Start starts the alert monitoring routine
func (am *AlertManager) Start(ctx context.Context) {
	am.mu.Lock()
	if am.running {
		am.mu.Unlock()
		return
	}
	am.running = true
	am.mu.Unlock()

	am.ticker = time.NewTicker(am.checkInterval)
	am.wg.Add(1)

	go func() {
		defer am.wg.Done()
		simplelog.LogThis("AlertManager", "Starting alert monitoring")

		for {
			select {
			case <-ctx.Done():
				simplelog.LogThis("AlertManager", "Context cancelled, stopping alert monitoring")
				return
			case <-am.stopChan:
				simplelog.LogThis("AlertManager", "Stop signal received, stopping alert monitoring")
				return
			case <-am.ticker.C:
				am.checkSystemHealth()
			}
		}
	}()
}

// Stop stops the alert monitoring routine
func (am *AlertManager) Stop() {
	am.mu.Lock()
	defer am.mu.Unlock()

	if !am.running {
		return
	}

	simplelog.LogThis("AlertManager", "Stopping alert monitoring")

	if am.ticker != nil {
		am.ticker.Stop()
	}

	close(am.stopChan)
	am.wg.Wait()

	am.running = false
	simplelog.LogThis("AlertManager", "Alert monitoring stopped")
}

// checkSystemHealth checks various system metrics and generates alerts
func (am *AlertManager) checkSystemHealth() {
	// Check connection pool usage
	am.checkConnectionPool()

	// Check authentication failure rate
	am.checkAuthenticationFailures()

	// Check query failure rate
	am.checkQueryFailures()
}

// checkConnectionPool monitors connection pool usage
func (am *AlertManager) checkConnectionPool() {
	if CurrentNode.DBConnections == nil || CurrentNode.MaxPool == 0 {
		return
	}

	active := CurrentNode.DBConnections.Len()
	usagePct := float64(active) / float64(CurrentNode.MaxPool) * 100

	// Critical threshold
	if usagePct >= am.poolCriticalThreshold {
		if time.Since(am.lastPoolCritical) > am.alertCooldown {
			am.CreateAlert(AlertLevelCritical,
				"Connection Pool Critical",
				fmt.Sprintf("Connection pool at %.1f%% capacity (%d/%d). Immediate action required!",
					usagePct, active, CurrentNode.MaxPool),
				map[string]interface{}{
					"active_connections": active,
					"max_pool":          CurrentNode.MaxPool,
					"usage_percentage":  usagePct,
				},
			)
			am.lastPoolCritical = time.Now()
		}
	} else if usagePct >= am.poolWarningThreshold {
		// Warning threshold
		if time.Since(am.lastPoolWarning) > am.alertCooldown {
			am.CreateAlert(AlertLevelWarning,
				"Connection Pool High Usage",
				fmt.Sprintf("Connection pool at %.1f%% capacity (%d/%d). Consider scaling or investigating connection leaks.",
					usagePct, active, CurrentNode.MaxPool),
				map[string]interface{}{
					"active_connections": active,
					"max_pool":          CurrentNode.MaxPool,
					"usage_percentage":  usagePct,
				},
			)
			am.lastPoolWarning = time.Now()
		}
	}

	// Check for pool exhaustion events
	if Metrics != nil {
		exhaustionCount := Metrics.PoolExhaustionCount
		if exhaustionCount > 0 && time.Since(Metrics.LastPoolExhaustion) < 5*time.Minute {
			am.CreateAlert(AlertLevelCritical,
				"Connection Pool Exhaustion",
				fmt.Sprintf("Connection pool has been exhausted %d times recently. Last occurrence: %s",
					exhaustionCount, Metrics.LastPoolExhaustion.Format(time.RFC3339)),
				map[string]interface{}{
					"exhaustion_count": exhaustionCount,
					"last_exhaustion": Metrics.LastPoolExhaustion,
				},
			)
		}
	}
}

// checkAuthenticationFailures monitors authentication failure rate
func (am *AlertManager) checkAuthenticationFailures() {
	if Metrics == nil || Metrics.AuthenticationAttempts < 10 {
		return // Not enough data
	}

	failureRate := float64(Metrics.AuthenticationFailures) / float64(Metrics.AuthenticationAttempts) * 100

	if failureRate > 50 {
		am.CreateAlert(AlertLevelWarning,
			"High Authentication Failure Rate",
			fmt.Sprintf("Authentication failure rate at %.1f%% (%d failures / %d attempts). Possible brute force attack?",
				failureRate, Metrics.AuthenticationFailures, Metrics.AuthenticationAttempts),
			map[string]interface{}{
				"failure_rate":  failureRate,
				"failures":      Metrics.AuthenticationFailures,
				"attempts":      Metrics.AuthenticationAttempts,
			},
		)
	}
}

// checkQueryFailures monitors query failure rate
func (am *AlertManager) checkQueryFailures() {
	if Metrics == nil || Metrics.QueriesExecuted < 10 {
		return // Not enough data
	}

	failureRate := float64(Metrics.QueriesFailed) / float64(Metrics.QueriesExecuted) * 100

	if failureRate > 25 {
		am.CreateAlert(AlertLevelCritical,
			"High Query Failure Rate",
			fmt.Sprintf("Query failure rate at %.1f%% (%d failures / %d queries). Database issues detected!",
				failureRate, Metrics.QueriesFailed, Metrics.QueriesExecuted),
			map[string]interface{}{
				"failure_rate": failureRate,
				"failures":     Metrics.QueriesFailed,
				"total":        Metrics.QueriesExecuted,
			},
		)
	} else if failureRate > 10 {
		am.CreateAlert(AlertLevelWarning,
			"Elevated Query Failure Rate",
			fmt.Sprintf("Query failure rate at %.1f%% (%d failures / %d queries). Investigate database performance.",
				failureRate, Metrics.QueriesFailed, Metrics.QueriesExecuted),
			map[string]interface{}{
				"failure_rate": failureRate,
				"failures":     Metrics.QueriesFailed,
				"total":        Metrics.QueriesExecuted,
			},
		)
	}
}

// CreateAlert creates and logs a new alert
func (am *AlertManager) CreateAlert(level AlertLevel, title, message string, metadata map[string]interface{}) {
	alert := Alert{
		Level:     level,
		Title:     title,
		Message:   message,
		Timestamp: time.Now(),
		Metadata:  metadata,
	}

	// Add to alert history
	am.mu.Lock()
	am.alerts = append(am.alerts, alert)
	// Keep only last maxAlerts
	if len(am.alerts) > am.maxAlerts {
		am.alerts = am.alerts[len(am.alerts)-am.maxAlerts:]
	}
	am.mu.Unlock()

	// Log the alert
	logMessage := fmt.Sprintf("[%s] %s: %s", level, title, message)
	switch level {
	case AlertLevelCritical:
		simplelog.LogErrorStr("ALERT", nil, logMessage)
	case AlertLevelWarning:
		simplelog.LogThis("ALERT", logMessage)
	case AlertLevelInfo:
		simplelog.LogThis("ALERT", logMessage)
	}

	// TODO: Future enhancement - send to external alerting system
	// - Email notifications
	// - Slack/Discord webhooks
	// - PagerDuty integration
	// - Prometheus AlertManager
}

// GetRecentAlerts returns recent alerts
func (am *AlertManager) GetRecentAlerts(limit int) []Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if limit == 0 || limit > len(am.alerts) {
		limit = len(am.alerts)
	}

	// Return most recent alerts
	if len(am.alerts) <= limit {
		result := make([]Alert, len(am.alerts))
		copy(result, am.alerts)
		return result
	}

	result := make([]Alert, limit)
	copy(result, am.alerts[len(am.alerts)-limit:])
	return result
}

// GetAlertsByLevel returns alerts filtered by level
func (am *AlertManager) GetAlertsByLevel(level AlertLevel) []Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	filtered := make([]Alert, 0)
	for _, alert := range am.alerts {
		if alert.Level == level {
			filtered = append(filtered, alert)
		}
	}
	return filtered
}

// ClearAlerts clears all stored alerts
func (am *AlertManager) ClearAlerts() {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.alerts = make([]Alert, 0)
}

// SetThresholds allows customizing alert thresholds
func (am *AlertManager) SetThresholds(warning, critical float64) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.poolWarningThreshold = warning
	am.poolCriticalThreshold = critical
}

// GetAlertStats returns alert statistics
func (am *AlertManager) GetAlertStats() map[string]interface{} {
	am.mu.RLock()
	defer am.mu.RUnlock()

	stats := map[string]interface{}{
		"total_alerts": len(am.alerts),
		"by_level": map[string]int{
			"info":     0,
			"warning":  0,
			"critical": 0,
		},
		"thresholds": map[string]float64{
			"pool_warning":  am.poolWarningThreshold,
			"pool_critical": am.poolCriticalThreshold,
		},
	}

	for _, alert := range am.alerts {
		switch alert.Level {
		case AlertLevelInfo:
			stats["by_level"].(map[string]int)["info"]++
		case AlertLevelWarning:
			stats["by_level"].(map[string]int)["warning"]++
		case AlertLevelCritical:
			stats["by_level"].(map[string]int)["critical"]++
		}
	}

	return stats
}

// Global alert manager instance
var (
	AlertMgr     *AlertManager
	alertMgrOnce sync.Once
)

// InitAlertManager initializes the global alert manager
func InitAlertManager() {
	alertMgrOnce.Do(func() {
		AlertMgr = NewAlertManager()
	})
}

// StartAlerting starts the alert monitoring system
func StartAlerting(ctx context.Context) {
	if AlertMgr == nil {
		InitAlertManager()
	}
	AlertMgr.Start(ctx)
}

// StopAlerting stops the alert monitoring system
func StopAlerting() {
	if AlertMgr != nil {
		AlertMgr.Stop()
	}
}
