package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rzzdr/quant-finance-pipeline/config"
	"github.com/rzzdr/quant-finance-pipeline/internal/kafka"
	"github.com/rzzdr/quant-finance-pipeline/internal/market"
	"github.com/rzzdr/quant-finance-pipeline/pkg/metrics"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

var (
	configFile = flag.String("config", "config.yaml", "Path to configuration file")
)

func main() {
	// Parse command line flags
	flag.Parse()

	// Initialize logger
	log := logger.GetLogger("processor.main")
	log.Info("Starting Quantitative Finance Pipeline Market Data Processor")

	// Set config path in environment variable
	os.Setenv("QUANT_CONFIG_PATH", *configFile)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create a context that will be canceled on program termination
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize metrics recorder
	recorder := metrics.NewRecorder()

	// Create Kafka client config
	kafkaConfig := &kafka.Config{
		BootstrapServers: strings.Join(cfg.Kafka.Brokers, ","),
		GroupID:          cfg.Kafka.Consumer.GroupID,
		AutoOffsetReset:  cfg.Kafka.Consumer.AutoOffsetReset,
		SessionTimeout:   cfg.Kafka.Consumer.SessionTimeout,
		ProducerAcks:     cfg.Kafka.Producer.Acks,
	}

	// Create Kafka client
	kafkaClient, err := kafka.NewClient(kafkaConfig)
	if err != nil {
		log.Fatalf("Failed to create Kafka client: %v", err)
	}

	// Create market data processor
	processor := market.NewProcessor(
		kafkaClient,
		market.ProcessorConfig{
			KafkaTopic:   "market-data",
			KafkaGroupID: "market-processor",
			OrderBookConfig: market.OrderBookConfig{
				MaxLevels: cfg.OrderBook.PriceLevels,
				PoolSize:  cfg.OrderBook.OrderPoolSize,
			},
		},
		recorder,
	)

	// Start market data processor
	if err := processor.Start(ctx); err != nil {
		log.Fatalf("Failed to start market data processor: %v", err)
	}

	log.Info("Market data processor started")

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	sig := <-sigChan
	log.Infof("Received signal %v, initiating shutdown", sig)

	// Stop processor
	if err := processor.Stop(); err != nil {
		log.Errorf("Market data processor shutdown error: %v", err)
	}

	// Close Kafka client
	if err := kafkaClient.Close(); err != nil {
		log.Errorf("Kafka client shutdown error: %v", err)
	}

	log.Info("Shutdown complete")
}
