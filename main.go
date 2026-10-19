package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func main() {
	// Support Render, Heroku, Cloud Run default $PORT
	defaultAddr := ":9099"
	if port := os.Getenv("PORT"); port != "" {
		defaultAddr = ":" + port
	}

	var (
		address      = flag.String("address", getEnv("MOCK_MIDTRANS_ADDRESS", defaultAddr), "Address to listen on (e.g. :9099 or :$PORT)")
		webhookURL   = flag.String("webhook-url", getEnv("MOCK_MIDTRANS_WEBHOOK_URL", ""), "Webhook URL of shipment service to send callbacks")
		serverKey    = flag.String("server-key", getEnv("MIDTRANS_DA_SERVER_KEY", ""), "Midtrans Server Key for calculating SHA-512 signatures")
		scenario     = flag.String("scenario", getEnv("MOCK_MIDTRANS_SCENARIO", "normal"), "Scenario: normal | timeout-existing | timeout-not-found")
		delaySeconds = flag.Int("delay-seconds", getEnvInt("MOCK_MIDTRANS_DELAY_SECONDS", 15), "Delay duration in seconds for timeout scenarios")
	)
	flag.Parse()

	cfg := Config{
		Address:    *address,
		WebhookURL: *webhookURL,
		ServerKey:  *serverKey,
		Scenario:   Scenario(*scenario),
		Delay:      time.Duration(*delaySeconds) * time.Second,
	}

	server := NewServer(cfg)

	httpServer := &http.Server{
		Addr:              cfg.Address,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
	}

	fmt.Printf("Midtrans Mock Server running on %s\n", cfg.Address)
	fmt.Printf("Initial Scenario: %s (Timeout delay: %v)\n", cfg.Scenario, cfg.Delay)
	if cfg.WebhookURL != "" {
		fmt.Printf("Webhook Target: %s\n", cfg.WebhookURL)
	}

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start server: %v", err)
	}
}
