package server

import (
	"net/http"
	"strconv"

	"github.com/medatechnology/suresql"
	"github.com/medatechnology/simplehttp"
)

// RegisterMonitoringRoutes registers monitoring and metrics endpoints
func RegisterMonitoringRoutes(server simplehttp.Server) {
	// Public health check endpoints (no auth required)
	server.GET("/health", HandleHealth)
	server.GET("/ready", HandleReadiness)

	// Protected monitoring endpoints (basic auth required)
	monitoring := server.Group("/monitoring")
	monitoring.Use(simplehttp.MiddlewareBasicAuth(
		suresql.CurrentNode.InternalConfig.Username,
		suresql.CurrentNode.InternalConfig.Password,
	))
	{
		monitoring.GET("/metrics", HandleMetrics)
		monitoring.GET("/metrics/pool", HandlePoolMetrics)
		monitoring.GET("/metrics/tokens", HandleTokenMetrics)
		monitoring.GET("/alerts", HandleAlerts)
		monitoring.GET("/alerts/stats", HandleAlertStats)
		monitoring.DELETE("/alerts", HandleClearAlerts)
		monitoring.GET("/health/detailed", HandleDetailedHealth)
	}
}

// HandleHealth returns basic health status (liveness probe)
func HandleHealth(ctx simplehttp.Context) error {
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"version": suresql.APP_VERSION,
		"service": suresql.APP_NAME,
	})
}

// HandleReadiness returns readiness status (readiness probe)
func HandleReadiness(ctx simplehttp.Context) error {
	// Check if database is connected
	if !suresql.CurrentNode.InternalConnection.IsConnected() {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]interface{}{
			"status": "not ready",
			"reason": "database connection failed",
		})
	}

	// Check connection pool health
	if suresql.ConnectionMgr != nil {
		usage := suresql.ConnectionMgr.GetConnectionPoolUsage()
		if usage >= 95.0 {
			return ctx.JSON(http.StatusServiceUnavailable, map[string]interface{}{
				"status": "not ready",
				"reason": "connection pool near exhaustion",
				"usage":  usage,
			})
		}
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status":  "ready",
		"version": suresql.APP_VERSION,
	})
}

// HandleMetrics returns comprehensive metrics
func HandleMetrics(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "/monitoring/metrics", "metrics")

	metrics := suresql.GetMetrics()

	return state.SetSuccess("Metrics retrieved successfully", metrics).
		LogAndResponse("metrics retrieved", nil, false)
}

// HandlePoolMetrics returns connection pool specific metrics
func HandlePoolMetrics(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "/monitoring/metrics/pool", "pool_metrics")

	poolStats := suresql.GetConnectionPoolStats()

	return state.SetSuccess("Pool metrics retrieved successfully", poolStats).
		LogAndResponse("pool metrics retrieved", nil, false)
}

// HandleTokenMetrics returns token specific metrics
func HandleTokenMetrics(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "/monitoring/metrics/tokens", "token_metrics")

	tokenStats := suresql.GetTokenStats()

	return state.SetSuccess("Token metrics retrieved successfully", tokenStats).
		LogAndResponse("token metrics retrieved", nil, false)
}

// HandleAlerts returns recent alerts
func HandleAlerts(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "/monitoring/alerts", "alerts")

	// Get limit from query parameter (default 20)
	limit := 20
	if limitStr := ctx.GetQueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Get level filter from query parameter (optional)
	levelFilter := ctx.GetQueryParam("level")

	var alerts []suresql.Alert
	if levelFilter != "" {
		level := suresql.AlertLevel(levelFilter)
		alerts = suresql.AlertMgr.GetAlertsByLevel(level)
	} else {
		alerts = suresql.AlertMgr.GetRecentAlerts(limit)
	}

	response := map[string]interface{}{
		"alerts": alerts,
		"count":  len(alerts),
	}

	return state.SetSuccess("Alerts retrieved successfully", response).
		LogAndResponse("alerts retrieved", nil, false)
}

// HandleAlertStats returns alert statistics
func HandleAlertStats(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "/monitoring/alerts/stats", "alert_stats")

	stats := suresql.AlertMgr.GetAlertStats()

	return state.SetSuccess("Alert stats retrieved successfully", stats).
		LogAndResponse("alert stats retrieved", nil, false)
}

// HandleClearAlerts clears all alerts
func HandleClearAlerts(ctx simplehttp.Context) error {
	state := NewHandlerState(ctx, suresql.CurrentNode.InternalConfig.Username, "/monitoring/alerts", "clear_alerts")

	suresql.AlertMgr.ClearAlerts()

	return state.SetSuccess("Alerts cleared successfully", nil).
		LogAndResponse("alerts cleared", nil, true)
}

// HandleDetailedHealth returns detailed health status
func HandleDetailedHealth(ctx simplehttp.Context) error {
	health := suresql.GetHealthStatus()

	// Add additional details
	health["pool_stats"] = suresql.GetConnectionPoolStats()
	health["token_stats"] = suresql.GetTokenStats()
	health["recent_alerts"] = suresql.AlertMgr.GetRecentAlerts(5)

	status := http.StatusOK
	if health["status"] == "unhealthy" {
		status = http.StatusServiceUnavailable
	} else if health["status"] == "degraded" {
		status = http.StatusOK // Still return 200 for degraded
	}

	return ctx.JSON(status, suresql.StandardResponse{
		Status:  status,
		Message: "Health status retrieved",
		Data:    health,
	})
}
