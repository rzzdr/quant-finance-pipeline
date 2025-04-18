package logger

import (
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger represents a logger instance
type Logger struct {
	*zap.SugaredLogger
}

var (
	globalLogger *Logger
	once         sync.Once
)

// Init initializes the global logger instance
func Init(level string, env string) {
	once.Do(func() {
		// Configure logger options
		encoderConfig := zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		}

		// Configure output format based on environment
		var encoder zapcore.Encoder
		if env == "production" {
			encoder = zapcore.NewJSONEncoder(encoderConfig)
		} else {
			encoder = zapcore.NewConsoleEncoder(encoderConfig)
		}

		// Configure log level
		logLevel := zapcore.InfoLevel
		switch level {
		case "debug":
			logLevel = zapcore.DebugLevel
		case "info":
			logLevel = zapcore.InfoLevel
		case "warn":
			logLevel = zapcore.WarnLevel
		case "error":
			logLevel = zapcore.ErrorLevel
		}

		// Create core with stdout output
		core := zapcore.NewCore(
			encoder,
			zapcore.AddSync(os.Stdout),
			zap.NewAtomicLevelAt(logLevel),
		)

		// Create logger with caller information
		logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))

		// Create SugaredLogger for more convenient API
		sugar := logger.Sugar()

		// Set the global logger
		globalLogger = &Logger{sugar}
	})
}

// GetLogger returns a logger instance with the given name
func GetLogger(name string) *Logger {
	// Initialize the logger if it hasn't been done yet
	if globalLogger == nil {
		Init("info", "development")
	}

	// Create a named logger
	return &Logger{
		globalLogger.Named(name),
	}
}

// Debug logs a debug message
func (l *Logger) Debug(args ...interface{}) {
	l.SugaredLogger.Debug(args...)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.SugaredLogger.Debugf(format, args...)
}

// Info logs an informational message
func (l *Logger) Info(args ...interface{}) {
	l.SugaredLogger.Info(args...)
}

// Infof logs a formatted informational message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.SugaredLogger.Infof(format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(args ...interface{}) {
	l.SugaredLogger.Warn(args...)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.SugaredLogger.Warnf(format, args...)
}

// Error logs an error message
func (l *Logger) Error(args ...interface{}) {
	l.SugaredLogger.Error(args...)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.SugaredLogger.Errorf(format, args...)
}

// Fatal logs a fatal message and exits the application
func (l *Logger) Fatal(args ...interface{}) {
	l.SugaredLogger.Fatal(args...)
}

// Fatalf logs a formatted fatal message and exits the application
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.SugaredLogger.Fatalf(format, args...)
}

// With returns a logger with additional structured context
func (l *Logger) With(args ...interface{}) *Logger {
	return &Logger{
		l.SugaredLogger.With(args...),
	}
}

// WithField returns a logger with a single field added to the context
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{
		l.SugaredLogger.With(key, value),
	}
}

// Sync ensures all buffered logs are written
func (l *Logger) Sync() error {
	return l.SugaredLogger.Sync()
}
