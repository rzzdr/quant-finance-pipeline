package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/config"
	"github.com/rzzdr/quant-finance-pipeline/internal/kafka"
	"github.com/rzzdr/quant-finance-pipeline/internal/market"
	"github.com/rzzdr/quant-finance-pipeline/internal/risk"
	"github.com/rzzdr/quant-finance-pipeline/pkg/metrics"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

const (
	riskCalculationInterval = 5 * time.Minute
)

var (
	configFile = flag.String("config", "config.yaml", "Path to configuration file")
)

func main() {
	// Parse command line flags
	flag.Parse()

	// Initialize logger
	log := logger.GetLogger("risk-engine.main")
	log.Info("Starting Quantitative Finance Pipeline Risk Engine")

	// Load configuration
	cfg, err := config.Load(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create a context that will be canceled on program termination
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize metrics recorder
	recorder := metrics.NewRecorder()

	// Create Kafka client
	kafkaClient, err := kafka.NewClient(cfg.Kafka)
	if err != nil {
		log.Fatalf("Failed to create Kafka client: %v", err)
	}

	// Create market data processor to receive market data
	marketProcessor := market.NewProcessor(
		kafkaClient,
		market.ProcessorConfig{
			KafkaTopic:   "market-data",
			KafkaGroupID: "risk-engine",
			OrderBookConfig: market.OrderBookConfig{
				MaxLevels: cfg.OrderBook.MaxLevels,
				PoolSize:  cfg.OrderBook.PoolSize,
			},
		},
		recorder,
	)

	// Create risk calculator
	riskCalculator := risk.NewCalculator(
		risk.CalculatorConfig{
			HistoricalDays:     cfg.Risk.HistoricalDays,
			VaRConfidenceLevel: cfg.Risk.VaRConfidenceLevel,
			ESConfidenceLevel:  cfg.Risk.ESConfidenceLevel,
		},
	)

	// Create a consumer for portfolio updates
	consumer:= kafka.NewConsumer(
		kafkaClient,
		kafka.ConsumerConfig{
			GroupID:         "risk-engine",
			AutoOffsetReset: "latest",
		},
	)

	// Create a producer for risk results
	producer := kafka.NewProducer(
		kafkaClient,
		kafka.ProducerConfig{
			BatchSize:    100,
			BatchTimeout: 100 * time.Millisecond,
		},
	)
	if err != nil {
		log.Fatalf("Failed to create producer: %v", err)
	}

	// Start consuming portfolio updates
	err = consumer.Subscribe("portfolio-updates", func(ctx context.Context, msg kafka.Message) error {
		// Parse portfolio update from message
		var portfolio models.Portfolio
		if err := portfolio.UnmarshalJSON(msg.Value); err != nil {
			log.Errorf("Error parsing portfolio update: %v", err)
			return nil // Don't retry, just log the error
		}

		// Update portfolio in risk calculator
		riskCalculator.UpdatePortfolio(&portfolio)

		// Calculate risk for this portfolio
		result, err := riskCalculator.CalculateRisk(portfolio.ID)
		if err != nil {
			log.Errorf("Error calculating risk for portfolio %s: %v", portfolio.ID, err)
			return nil
		}

		// Log the risk calculation
		log.Infof("Risk calculation for portfolio %s: VaR=%.2f, ES=%.2f",
			portfolio.ID, result.VaR, result.ES)
		// Publish risk result to Kafka
		resultData, err := json.Marshal(result)
		if err != nil {
			log.Errorf("Error marshaling risk result: %v", err)
			return nil
		}
		}

		resultMsg := kafka.Message{
			Topic: "risk-results",
			Key:   []byte(portfolio.ID),
			Value: resultData,
		}
		err = producer.Publish(ctx, resultMsg)
		if err != nil {
			log.Errorf("Error publishing risk result: %v", err)
			return nil
		}

		return nil
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to portfolio updates: %v", err)
	}

	// Start consuming market data for risk calculator
	err = consumer.Subscribe("market-data", func(ctx context.Context, msg kafka.Message) error {
		// Parse market data from message
		var marketData models.MarketData
		if err := marketData.UnmarshalJSON(msg.Value); err != nil {
			log.Errorf("Error parsing market data: %v", err)
			return nil // Don't retry, just log the error
		}

		// Update market data in risk calculator
		riskCalculator.UpdateMarketData(&marketData)

		return nil
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to market data: %v", err)
	}

	// Start periodic risk calculation for all portfolios
	go func() {
		ticker := time.NewTicker(riskCalculationInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Info("Running periodic risk calculation for all portfolios")
				riskCalculator.CalculateAllRisk()
			}
		}
	}()

	// Start the consumer
	if err := consumer.Start(); err != nil {
		log.Fatalf("Failed to start consumer: %v", err)
	}

	log.Info("Risk engine started")

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	sig := <-sigChan
	log.Infof("Received signal %v, initiating shutdown", sig)

	// Stop consumer
	if err := consumer.Stop(); err != nil {
		log.Errorf("Consumer shutdown error: %v", err)
	}

	// Stop market data processor
	if err := marketProcessor.Stop(); err != nil {
		log.Errorf("Market data processor shutdown error: %v", err)
	}

	// Close producer
	if err := producer.Close(); err != nil {
		log.Errorf("Producer shutdown error: %v", err)
	}

	// Close Kafka client
	if err := kafkaClient.Close(); err != nil {
		log.Errorf("Kafka client shutdown error: %v", err)
	}

	log.Info("Shutdown complete")
}
