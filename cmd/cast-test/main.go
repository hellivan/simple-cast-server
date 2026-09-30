// Command cast-test is a small standalone utility for exercising the
// chromecast package directly against a real device, without needing an
// MQTT broker or the full bridge. Useful for manual testing, e.g.:
//
//	go run ./cmd/cast-test --ip 10.0.4.2 --url https://immich-kiosk.ihell.cloud/
//
// It starts casting the given URL, prints the device status every few
// seconds, and stops casting on Ctrl+C.
package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hellivan/simple-cast-server/internal/chromecast"
)

func main() {
	var (
		ip   string
		port int
		url  string
	)
	flag.StringVar(&ip, "ip", "", "Chromecast/Nest Hub IP address (required).")
	flag.IntVar(&port, "port", 0, "Cast V2 control port (defaults to 8009).")
	flag.StringVar(&url, "url", "", "URL to cast via DashCast (required).")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if ip == "" || url == "" {
		logger.Error("both --ip and --url are required")
		os.Exit(1)
	}

	client := chromecast.NewClient(ip, port, logger)
	defer client.Close()

	logger.Info("starting cast", "ip", ip, "url", url)
	if err := client.Start(url); err != nil {
		logger.Error("failed to start casting", "error", err)
		os.Exit(1)
	}
	logger.Info("cast started")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			logger.Info("stopping cast")
			if err := client.Stop(); err != nil {
				logger.Error("failed to stop casting", "error", err)
				os.Exit(1)
			}
			logger.Info("cast stopped, exiting")
			return
		case <-ticker.C:
			state, err := client.Status()
			if err != nil {
				logger.Error("status check failed", "error", err)
				continue
			}
			logger.Info("status", "casting", state.Casting, "app", state.AppDisplayName)
		}
	}
}
