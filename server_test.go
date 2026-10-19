package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalScenario(t *testing.T) {
	server := NewServer(Config{
		Scenario: ScenarioNormal,
		Delay:    50 * time.Millisecond,
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Charge request
	reqBody := `{"transaction_details":{"order_id":"STR123456","gross_amount":50000}}`
	resp, err := http.Post(ts.URL+"/v2/charge", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatalf("charge request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var chargeResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&chargeResp); err != nil {
		t.Fatalf("decode charge resp failed: %v", err)
	}
	if chargeResp["order_id"] != "STR123456" {
		t.Fatalf("expected order_id STR123456, got %v", chargeResp["order_id"])
	}

	// Status check request
	respStatus, err := http.Get(ts.URL + "/v2/STR123456/status")
	if err != nil {
		t.Fatalf("status request failed: %v", err)
	}
	defer respStatus.Body.Close()

	if respStatus.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for status, got %d", respStatus.StatusCode)
	}

	var statusResp map[string]string
	if err := json.NewDecoder(respStatus.Body).Decode(&statusResp); err != nil {
		t.Fatalf("decode status resp failed: %v", err)
	}
	if statusResp["transaction_status"] != "pending" {
		t.Fatalf("expected status pending, got %v", statusResp["transaction_status"])
	}
}

func TestTimeoutExistingScenario(t *testing.T) {
	server := NewServer(Config{
		Scenario: ScenarioTimeoutExisting,
		Delay:    100 * time.Millisecond,
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Short timeout client to simulate timeout
	client := &http.Client{Timeout: 30 * time.Millisecond}
	reqBody := `{"transaction_details":{"order_id":"STR999999","gross_amount":100000}}`
	_, err := client.Post(ts.URL+"/v2/charge", "application/json", bytes.NewBufferString(reqBody))
	if err == nil {
		t.Fatalf("expected charge to timeout, got nil error")
	}

	// In timeout-existing, the transaction should still be stored in mock state!
	respStatus, err := http.Get(ts.URL + "/v2/STR999999/status")
	if err != nil {
		t.Fatalf("status check failed: %v", err)
	}
	defer respStatus.Body.Close()

	if respStatus.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on status lookup for timeout-existing, got %d", respStatus.StatusCode)
	}
}

func TestTimeoutNotFoundScenario(t *testing.T) {
	server := NewServer(Config{
		Scenario: ScenarioTimeoutNotFound,
		Delay:    100 * time.Millisecond,
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Short timeout client to simulate timeout
	client := &http.Client{Timeout: 30 * time.Millisecond}
	reqBody := `{"transaction_details":{"order_id":"STR-NOT-FOUND","gross_amount":25000}}`
	_, err := client.Post(ts.URL+"/v2/charge", "application/json", bytes.NewBufferString(reqBody))
	if err == nil {
		t.Fatalf("expected charge to timeout, got nil error")
	}

	// In timeout-not-found, transaction should return 404!
	respStatus, err := http.Get(ts.URL + "/v2/STR-NOT-FOUND/status")
	if err != nil {
		t.Fatalf("status check failed: %v", err)
	}
	defer respStatus.Body.Close()

	if respStatus.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for timeout-not-found, got %d", respStatus.StatusCode)
	}
}

func TestSwitchScenarioAndTriggerWebhook(t *testing.T) {
	var receivedWebhook map[string]interface{}
	webhookReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedWebhook)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer webhookReceiver.Close()

	server := NewServer(Config{
		Scenario:   ScenarioNormal,
		WebhookURL: webhookReceiver.URL,
		ServerKey:  "dummy-key",
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Switch scenario
	switchBody := `{"scenario":"timeout-existing"}`
	respSwitch, err := http.Post(ts.URL+"/mock/scenario", "application/json", bytes.NewBufferString(switchBody))
	if err != nil || respSwitch.StatusCode != http.StatusOK {
		t.Fatalf("failed to switch scenario: %v, status: %d", err, respSwitch.StatusCode)
	}
	respSwitch.Body.Close()

	// Trigger webhook
	triggerBody := `{"order_id":"STR-HOOK-TEST","transaction_id":"trx-123","gross_amount":"50000","transaction_status":"settlement"}`
	respHook, err := http.Post(ts.URL+"/mock/trigger-webhook", "application/json", bytes.NewBufferString(triggerBody))
	if err != nil || respHook.StatusCode != http.StatusOK {
		t.Fatalf("failed to trigger webhook: %v, status: %d", err, respHook.StatusCode)
	}
	respHook.Body.Close()

	if receivedWebhook["order_id"] != "STR-HOOK-TEST" {
		t.Fatalf("expected received webhook order_id STR-HOOK-TEST, got %v", receivedWebhook["order_id"])
	}
	if receivedWebhook["signature_key"] == "" {
		t.Fatalf("expected non-empty signature_key")
	}
}
