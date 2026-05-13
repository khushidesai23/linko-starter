package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
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
		logger.Printf("failed to create store: %v", err)
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
		logger.Printf("failed to shutdown server: %v", err)
		return 1
	}
	if serverErr != nil {
		logger.Printf("server error: %v", serverErr)
		return 1
	}
	return 0
}

func initializeLogger() (*log.Logger, closeFunc, error) {
	// initializeLogger returns a logger, a close function to cleanup resources, and an error
	return initializeLoggerWithPath(os.Getenv("LINKO_LOG_FILE"))
}

type closeFunc func() error

func initializeLoggerWithPath(path string) (*log.Logger, closeFunc, error) {
	if path == "" {
		return log.New(os.Stderr, "", log.LstdFlags), func() error { return nil }, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	buf := bufio.NewWriterSize(f, 8192)
	mw := io.MultiWriter(buf, os.Stderr)
	l := log.New(mw, "", log.LstdFlags)
	closeFn := func() error {
		if err := buf.Flush(); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}
	return l, closeFn, nil
}
