package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rzzdr/quant-finance-pipeline/config"
	"github.com/rzzdr/quant-finance-pipeline/internal/adapters"
	"github.com/rzzdr/quant-finance-pipeline/internal/kafka"
	"github.com/rzzdr/quant-finance-pipeline/internal/market"
	"github.com/rzzdr/quant-finance-pipeline/internal/risk"
	"github.com/rzzdr/quant-finance-pipeline/internal/store"
	"github.com/rzzdr/quant-finance-pipeline/pkg/api"
	"github.com/rzzdr/quant-finance-pipeline/pkg/metrics"
	"github.com/rzzdr/quant-finance-pipeline/pkg/models"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

var (
	configFile = flag.String("config", "config.yaml", "Path to configuration file")
)

func main() {
	// Parse command line flags
	flag.Parse()

	// Initialize logger
	log := logger.GetLogger("api.main")
	log.Info("Starting Quantitative Finance Pipeline API Service")

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

	// Create metrics adapter to satisfy the market.MetricsRecorder interface
	metricsAdapter := adapters.NewMetricsAdapter(recorder)

	// Create Kafka client with nil config (using defaults)
	kafkaClient, err := kafka.NewClient(nil)
	if err != nil {
		log.Fatalf("Failed to create Kafka client: %v", err)
	}

	// Create market processor with the metrics adapter
	marketProcessor := market.NewProcessor(
		kafkaClient,
		market.ProcessorConfig{
			KafkaTopic:   "market-data",
			KafkaGroupID: "api-service",
			OrderBookConfig: market.OrderBookConfig{
				MaxLevels: cfg.OrderBook.PriceLevels,
				PoolSize:  cfg.OrderBook.OrderPoolSize,
			},
		},
		metricsAdapter, // Use our adapter that implements the market.MetricsRecorder interface
	)

	// Create in-memory stores for portfolio and historical data
	portfolioStore := store.NewInMemoryPortfolioStore()
	historicalDataStore := store.NewInMemoryHistoricalDataStore()

	// Add some sample portfolios
	samplePortfolio := &models.Portfolio{
		ID:   "sample-portfolio-1",
		Name: "Sample Diversified Portfolio",
		Positions: []models.Position{
			{
				Symbol:   "AAPL",
				Quantity: 100,
			},
			{
				Symbol:   "MSFT",
				Quantity: 150,
			},
			{
				Symbol:   "SPY",
				Quantity: 200,
			},
		},
	}
	portfolioStore.SavePortfolio(samplePortfolio)

	// Create risk calculator with our stores
	riskCalculator := risk.NewCalculator(
		risk.CalculatorConfig{
			VaRConfidenceLevel: cfg.Risk.VaRConfidenceLevel,
			ESConfidenceLevel:  cfg.Risk.ESConfidenceLevel,
			SimulationRuns:     cfg.Risk.SimulationRuns,
			HistoricalDays:     cfg.Risk.HistoricalDays,
			WorkerCount:        4, // Default worker count
		},
		portfolioStore,
		historicalDataStore,
	)

	// Start market data processor
	if err := marketProcessor.Start(ctx); err != nil {
		log.Fatalf("Failed to start market data processor: %v", err)
	}

	// Create market processor adapter for the API server
	marketProcessorAdapter := adapters.NewMarketProcessorAdapter(marketProcessor)

	apiServer := api.NewServer(
		api.Config{
			Host:         cfg.API.Host,
			Port:         cfg.API.Port,
			ReadTimeout:  cfg.API.ReadTimeout,
			WriteTimeout: cfg.API.WriteTimeout,
		},
		marketProcessorAdapter, // Use our adapter that implements market.Processor interface
		riskCalculator,
		recorder,
	)

	// Start API server
	go func() {
		if err := apiServer.Start(); err != nil {
			log.Errorf("API server error: %v", err)
			cancel() // Cancel context to signal shutdown
		}
	}()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	sig := <-sigChan
	log.Infof("Received signal %v, initiating shutdown", sig)

	// Create a context with timeout for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Stop API server
	if err := apiServer.Stop(shutdownCtx); err != nil {
		log.Errorf("API server shutdown error: %v", err)
	}

	// Stop market data processor
	if err := marketProcessor.Stop(); err != nil {
		log.Errorf("Market data processor shutdown error: %v", err)
	}

	// Close Kafka client
	if err := kafkaClient.Close(); err != nil {
		log.Errorf("Kafka client shutdown error: %v", err)
	}

	log.Info("Shutdown complete")
}
