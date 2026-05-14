package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8080, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	logger, closeLogger, err := initializeLogger()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to initialize logger:", err)
		return 1
	}
	// ensure logger resources are cleaned up on exit; print any error to stderr
	defer func() {
		if cerr := closeLogger(); cerr != nil {
			fmt.Fprintln(os.Stderr, "error closing logger:", cerr)
		}
	}()

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error("failed to create store", "error", err)
		return 1
	}
	s := newServer(*st, httpPort, cancel, logger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown server", "error", err)
		return 1
	}
	if serverErr != nil {
		logger.Error("server error", "error", serverErr)
		return 1
	}
	return 0
}
func initializeLogger() (*slog.Logger, closeFunc, error) {
	// initializeLogger returns a logger, a close function to cleanup resources, and an error
	return initializeLoggerWithPath(os.Getenv("LINKO_LOG_FILE"))
}

type closeFunc func() error

func initializeLoggerWithPath(path string) (*slog.Logger, closeFunc, error) {
	if path == "" {
		stderrH := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		return slog.New(stderrH), func() error { return nil }, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	buf := bufio.NewWriterSize(f, 8192)
	fileMw := io.MultiWriter(buf)
	fileH := slog.NewTextHandler(fileMw, &slog.HandlerOptions{Level: slog.LevelInfo})
	stderrH := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	multi := slog.NewMultiHandler(fileH, stderrH)
	l := slog.New(multi)
	closeFn := func() error {
		if err := buf.Flush(); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}
	return l, closeFn, nil
}
