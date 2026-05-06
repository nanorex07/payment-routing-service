package logging

import (
	"context"
	"log/slog"
	"os"
)

type SlogLogger struct {
	logger *slog.Logger
}

func NewSlogLogger() *SlogLogger {
	return &SlogLogger{logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))}
}

func (l *SlogLogger) Info(ctx context.Context, message string, attrs ...any) {
	l.logger.InfoContext(ctx, message, attrs...)
}

func (l *SlogLogger) Error(ctx context.Context, message string, attrs ...any) {
	l.logger.ErrorContext(ctx, message, attrs...)
}
