package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config for the whole application
type Config struct {
	App       AppConfig
	API       APIConfig
	Kafka     KafkaConfig
	Database  DatabaseConfig
	Risk      RiskConfig
	OrderBook OrderBookConfig
	Metrics   MetricsConfig
	Processor ProcessorConfig
}

// General application configuration
type AppConfig struct {
	Name        string
	Environment string
	LogLevel    string
}

// Configuration for the API server
type APIConfig struct {
	Host            string
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	RateLimit       int
	CORS            CORSConfig
}

// CORS configuration
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
}

// Configuration for Kafka
type KafkaConfig struct {
	Brokers  []string
	Consumer KafkaConsumerConfig
	Producer KafkaProducerConfig
	Topics   KafkaTopicsConfig
}

// Kafka consumer configuration
type KafkaConsumerConfig struct {
	GroupID           string
	AutoOffsetReset   string
	SessionTimeout    time.Duration
	HeartbeatInterval time.Duration
}

// Kafka producer configuration
type KafkaProducerConfig struct {
	Acks            string
	CompressionType string
	BatchSize       int
	LingerMs        int
	RetryBackoff    time.Duration
	MaxRetries      int
}

// Kafka topics configuration
type KafkaTopicsConfig struct {
	MarketDataRaw        string
	MarketDataNormalized string
	OrdersIncoming       string
	OrdersExecuted       string
	RiskMetrics          string
	AnalyticsResults     string
}

// Configuration for the database
type DatabaseConfig struct {
	Host            string
	Port            int
	Username        string
	Password        string
	DBName          string
	SSLMode         string
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime time.Duration
}

// Configuration for risk calculations
type RiskConfig struct {
	VaRConfidenceLevel float64
	ESConfidenceLevel  float64
	SimulationRuns     int
	HistoricalDays     int
}

// Configuration for the order book
type OrderBookConfig struct {
	PriceLevels        int
	OrderPoolSize      int
	SnapshotInterval   time.Duration
	PersistenceEnabled bool
}

// Configuration for metrics
type MetricsConfig struct {
	Prometheus PrometheusConfig
	Interval   time.Duration
}

// Configuration for Prometheus metrics
type PrometheusConfig struct {
	Enabled bool
	Port    int
}

// Configuration for the data processor
type ProcessorConfig struct {
	Workers            int
	BatchSize          int
	ProcessingInterval time.Duration
}

// Loads the configuration from a file and environment variables
func Load() (*Config, error) {
	setDefaults()

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	viper.SetEnvPrefix("QUANT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

func setDefaults() {
	// App defaults
	viper.SetDefault("app.name", "quant-finance-pipeline")
	viper.SetDefault("app.environment", "development")
	viper.SetDefault("app.log_level", "info")

	// API defaults
	viper.SetDefault("api.host", "0.0.0.0")
	viper.SetDefault("api.port", 8080)
	viper.SetDefault("api.read_timeout", "10s")
	viper.SetDefault("api.write_timeout", "10s")
	viper.SetDefault("api.shutdown_timeout", "30s")
	viper.SetDefault("api.rate_limit", 100)
	viper.SetDefault("api.cors.allowed_origins", []string{"*"})
	viper.SetDefault("api.cors.allowed_methods", []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"})
	viper.SetDefault("api.cors.allowed_headers", []string{"Authorization", "Content-Type"})

	// Kafka defaults
	viper.SetDefault("kafka.brokers", []string{"localhost:9092"})
	viper.SetDefault("kafka.consumer.group_id", "quant-finance-group")
	viper.SetDefault("kafka.consumer.auto_offset_reset", "earliest")
	viper.SetDefault("kafka.consumer.session_timeout", "30s")
	viper.SetDefault("kafka.consumer.heartbeat_interval", "3s")
	viper.SetDefault("kafka.producer.acks", "all")
	viper.SetDefault("kafka.producer.compression_type", "snappy")
	viper.SetDefault("kafka.producer.batch_size", 16384)
	viper.SetDefault("kafka.producer.linger_ms", 5)
	viper.SetDefault("kafka.producer.retry_backoff", "100ms")
	viper.SetDefault("kafka.producer.max_retries", 3)
	viper.SetDefault("kafka.topics.market_data_raw", "market.data.raw")
	viper.SetDefault("kafka.topics.market_data_normalized", "market.data.normalized")
	viper.SetDefault("kafka.topics.orders_incoming", "orders.incoming")
	viper.SetDefault("kafka.topics.orders_executed", "orders.executed")
	viper.SetDefault("kafka.topics.risk_metrics", "risk.metrics")
	viper.SetDefault("kafka.topics.analytics_results", "analytics.results")

	// Database defaults
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.username", "postgres")
	viper.SetDefault("database.password", "postgres")
	viper.SetDefault("database.dbname", "quantfinance")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("database.max_idle_conns", 10)
	viper.SetDefault("database.max_open_conns", 50)
	viper.SetDefault("database.conn_max_lifetime", "30m")

	// Risk defaults
	viper.SetDefault("risk.var_confidence_level", 0.99)
	viper.SetDefault("risk.es_confidence_level", 0.975)
	viper.SetDefault("risk.simulation_runs", 10000)
	viper.SetDefault("risk.historical_days", 252)

	// OrderBook defaults
	viper.SetDefault("orderbook.price_levels", 10)
	viper.SetDefault("orderbook.order_pool_size", 100000)
	viper.SetDefault("orderbook.snapshot_interval", "5s")
	viper.SetDefault("orderbook.persistence_enabled", true)

	// Metrics defaults
	viper.SetDefault("metrics.prometheus.enabled", true)
	viper.SetDefault("metrics.prometheus.port", 9090)
	viper.SetDefault("metrics.interval", "15s")

	// Processor defaults
	viper.SetDefault("processor.workers", 8)
	viper.SetDefault("processor.batch_size", 100)
	viper.SetDefault("processor.processing_interval", "50ms")
}

func GetConfigPath() string {
	configPath := os.Getenv("QUANT_CONFIG_PATH")
	if configPath != "" {
		return configPath
	}

	return "./config/config.yaml"
}
