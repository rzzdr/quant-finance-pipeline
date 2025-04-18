package api

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rzzdr/quant-finance-pipeline/pkg/metrics"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// LoggingMiddleware logs request information
func LoggingMiddleware() gin.HandlerFunc {
	log := logger.GetLogger("api.middleware")

	return func(c *gin.Context) {
		// Start timer
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(start)

		// Log request details
		log.Infof("%s %s [%d] %v", method, path, c.Writer.Status(), latency)
	}
}

// MetricsMiddleware captures API metrics
func MetricsMiddleware(recorder *metrics.Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		// Process request
		c.Next()

		// Record metrics
		status := c.Writer.Status()
		latency := time.Since(start)

		recorder.RecordAPIRequest(method, path, status, latency)
	}
}

// CORSMiddleware handles Cross-Origin Resource Sharing
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// AuthMiddleware verifies API authentication
func AuthMiddleware() gin.HandlerFunc {
	log := logger.GetLogger("api.auth")

	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")

		// In a real implementation, this would validate the token
		// For now, just check if it's present
		if token == "" {
			log.Warn("Missing authorization header")
			c.AbortWithStatusJSON(401, gin.H{
				"error": "Unauthorized",
			})
			return
		}

		// For now, just a placeholder for actual token validation
		if len(token) < 10 {
			log.Warn("Invalid token format")
			c.AbortWithStatusJSON(401, gin.H{
				"error": "Invalid token",
			})
			return
		}

		// Add verified user info to context
		c.Set("user_id", "sample_user_id") // This would come from the token

		c.Next()
	}
}

// ErrorMiddleware catches panics and returns an error response
func ErrorMiddleware() gin.HandlerFunc {
	log := logger.GetLogger("api.error")

	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Log the error
				log.Errorf("API panic recovered: %v", err)

				// Return error response
				c.AbortWithStatusJSON(500, gin.H{
					"error": fmt.Sprintf("Internal server error: %v", err),
				})
			}
		}()

		c.Next()
	}
}

// RateLimitMiddleware limits the number of requests per client
func RateLimitMiddleware(rps int, burst int) gin.HandlerFunc {
	log := logger.GetLogger("api.ratelimit")

	// In a real implementation, this would use a proper rate limiter
	// For now, just a placeholder
	return func(c *gin.Context) {
		// Get client IP
		clientIP := c.ClientIP()

		// Simulate rate limiting check
		if clientIP == "192.168.1.1" { // This would be a real check in production
			log.Warnf("Rate limit exceeded for client: %s", clientIP)
			c.AbortWithStatusJSON(429, gin.H{
				"error": "Rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}
