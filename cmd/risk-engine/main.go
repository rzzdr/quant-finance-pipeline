package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/config"
	"github.com/rzzdr/quant-finance-pipeline/internal/kafka"
	"github.com/rzzdr/quant-finance-pipeline/internal/market"
	"github.com/rzzdr/quant-finance-pipeline/internal/risk"
	"github.com/rzzdr/quant-finance-pipeline/internal/store"
	"github.com/rzzdr/quant-finance-pipeline/pkg/metrics"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

const (
	riskCalculationInterval = 5 * time.Minute
)

func main() {
	// Parse command line flags
	flag.Parse()

	// Initialize logger
	log := logger.GetLogger("risk-engine.main")
	log.Info("Starting Quantitative Finance Pipeline Risk Engine")

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

	// Create Kafka client
	kafkaConfig := &kafka.Config{
		BootstrapServers: strings.Join(cfg.Kafka.Brokers, ","),
		GroupID:          cfg.Kafka.Consumer.GroupID,
		AutoOffsetReset:  cfg.Kafka.Consumer.AutoOffsetReset,
		SessionTimeout:   cfg.Kafka.Consumer.SessionTimeout,
		ProducerAcks:     cfg.Kafka.Producer.Acks,
	}
	kafkaClient, err := kafka.NewClient(kafkaConfig)
	if err != nil {
		log.Fatalf("Failed to create Kafka client: %v", err)
	}

	// Create portfolio store
	portfolioStore := store.NewInMemoryPortfolioStore()

	// Create historical data store
	historicalDataStore := store.NewInMemoryHistoricalDataStore()

	// Create market data processor to receive market data
	marketProcessor := market.NewProcessor(
		kafkaClient,
		market.ProcessorConfig{
			KafkaTopic:   cfg.Kafka.Topics.MarketDataNormalized,
			KafkaGroupID: "risk-engine",
			OrderBookConfig: market.OrderBookConfig{
				MaxLevels: cfg.OrderBook.PriceLevels,
				PoolSize:  cfg.OrderBook.OrderPoolSize,
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
			SimulationRuns:     cfg.Risk.SimulationRuns,
			WorkerCount:        4, // Default worker count
		},
		portfolioStore,
		historicalDataStore,
	)

	// Create a consumer for portfolio updates
	portfolioConsumer, err := kafkaClient.NewConsumer(
		[]string{cfg.Kafka.Topics.OrdersExecuted},
		&kafka.ConsumerConfig{
			GroupID:         "risk-engine",
			AutoOffsetReset: "latest",
		},
	)
	if err != nil {
		log.Fatalf("Failed to create portfolio consumer: %v", err)
	}

	// Create a producer for risk results
	riskProducer, err := kafkaClient.NewProducer(cfg.Kafka.Topics.RiskMetrics)
	if err != nil {
		log.Fatalf("Failed to create risk producer: %v", err)
	}

	// Start market data processor
	if err := marketProcessor.Start(ctx); err != nil {
		log.Fatalf("Failed to start market data processor: %v", err)
	}

	// Start consuming portfolio updates
	go func() {
		log.Info("Starting portfolio updates consumer")

		for {
			select {
			case <-ctx.Done():
				return
			default:
				message, err := portfolioConsumer.ConsumeMessage(ctx, 100*time.Millisecond)
				if err != nil {
					log.Errorf("Error consuming portfolio message: %v", err)
					continue
				}

				if message == nil {
					continue
				}

				// Deserialize the portfolio update
				var portfolio models.Portfolio
				if err := json.Unmarshal(message.Value, &portfolio); err != nil {
					log.Errorf("Failed to unmarshal portfolio update: %v", err)
					continue
				}

				// Save portfolio to store
				err = portfolioStore.SavePortfolio(&portfolio)
				if err != nil {
					log.Errorf("Failed to save portfolio %s: %v", portfolio.ID, err)
					continue
				}

				// Calculate risk metrics
				riskMetrics, err := riskCalculator.CalculateRiskMetrics(ctx, portfolio.ID)
				if err != nil {
					log.Errorf("Failed to calculate risk metrics for portfolio %s: %v", portfolio.ID, err)
					continue
				}

				// Serialize risk metrics
				riskMetricsJson, err := json.Marshal(riskMetrics)
				if err != nil {
					log.Errorf("Failed to marshal risk metrics: %v", err)
					continue
				}

				// Produce risk metrics
				err = riskProducer.ProduceMessage(ctx, []byte(portfolio.ID), riskMetricsJson, nil)
				if err != nil {
					log.Errorf("Failed to produce risk metrics: %v", err)
					continue
				}

				log.Infof("Calculated risk metrics for portfolio %s", portfolio.ID)
			}
		}
	}()

	// Calculate risk metrics periodically for all portfolios
	go func() {
		ticker := time.NewTicker(riskCalculationInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Info("Running periodic risk calculation for all portfolios")

				// Get all portfolios
				portfolios, err := portfolioStore.GetAllPortfolios()
				if err != nil {
					log.Errorf("Failed to get all portfolios: %v", err)
					continue
				}

				// Calculate risk metrics for each portfolio
				for _, portfolio := range portfolios {
					riskMetrics, err := riskCalculator.CalculateRiskMetrics(ctx, portfolio.ID)
					if err != nil {
						log.Errorf("Failed to calculate risk metrics for portfolio %s: %v", portfolio.ID, err)
						continue
					}

					// Serialize risk metrics
					riskMetricsJson, err := json.Marshal(riskMetrics)
					if err != nil {
						log.Errorf("Failed to marshal risk metrics: %v", err)
						continue
					}

					// Produce risk metrics
					err = riskProducer.ProduceMessage(ctx, []byte(portfolio.ID), riskMetricsJson, nil)
					if err != nil {
						log.Errorf("Failed to produce risk metrics: %v", err)
						continue
					}

					log.Infof("Calculated risk metrics for portfolio %s", portfolio.ID)
				}
			}
		}
	}()

	log.Info("Risk engine started")

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	sig := <-sigChan
	log.Infof("Received signal %v, initiating shutdown", sig)

	// Stop consumer
	if err := portfolioConsumer.Close(); err != nil {
		log.Errorf("Portfolio consumer shutdown error: %v", err)
	}

	// Stop market data processor
	if err := marketProcessor.Stop(); err != nil {
		log.Errorf("Market data processor shutdown error: %v", err)
	}

	// Close producer
	riskProducer.Close()
	log.Info("Risk producer closed")

	// Close Kafka client
	if err := kafkaClient.Close(); err != nil {
		log.Errorf("Kafka client shutdown error: %v", err)
	}

	log.Info("Shutdown complete")
}
