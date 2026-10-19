package main

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Scenario string

const (
	ScenarioNormal          Scenario = "normal"
	ScenarioTimeoutExisting Scenario = "timeout-existing"
	ScenarioTimeoutNotFound Scenario = "timeout-not-found"
)

type Config struct {
	Address    string
	WebhookURL string
	ServerKey  string
	Scenario   Scenario
	Delay      time.Duration
}

type transaction struct {
	OrderID           string `json:"order_id"`
	TransactionID     string `json:"transaction_id"`
	GrossAmount       string `json:"gross_amount"`
	TransactionTime   string `json:"transaction_time"`
	ExpiryTime        string `json:"expiry_time"`
	TransactionStatus string `json:"transaction_status"`
}

type Server struct {
	config       Config
	mu           sync.RWMutex
	transactions map[string]transaction
	lastOrderID  string
	sequence     int
	httpClient   *http.Client
}

type chargeRequest struct {
	TransactionDetails struct {
		OrderID     string      `json:"order_id"`
		GrossAmount interface{} `json:"gross_amount"`
	} `json:"transaction_details"`
}

type webhookRequest struct {
	OrderID           string `json:"order_id"`
	TransactionID     string `json:"transaction_id"`
	GrossAmount       string `json:"gross_amount"`
	StatusCode        string `json:"status_code"`
	TransactionStatus string `json:"transaction_status"`
}

func NewServer(config Config) *Server {
	if config.Address == "" {
		config.Address = ":9099"
	}
	if config.Scenario == "" {
		config.Scenario = ScenarioNormal
	}
	if config.Delay <= 0 {
		config.Delay = 15 * time.Second
	}

	return &Server{
		config:       config,
		transactions: make(map[string]transaction),
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/v2/charge", s.charge)
	mux.HandleFunc("/v2/", s.v2)
	mux.HandleFunc("/mock/scenario", s.scenario)
	mux.HandleFunc("/mock/state", s.state)
	mux.HandleFunc("/mock/trigger-webhook", s.triggerWebhook)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "UP",
		"scenario": string(s.config.Scenario),
	})
}

func (s *Server) charge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
		return
	}

	var payload chargeRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if payload.TransactionDetails.OrderID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "transaction_details.order_id is required"})
		return
	}

	s.mu.Lock()
	s.sequence++
	now := time.Now()
	transactionData := transaction{
		OrderID:           payload.TransactionDetails.OrderID,
		TransactionID:     fmt.Sprintf("mock-%d-%s", s.sequence, payload.TransactionDetails.OrderID),
		GrossAmount:       amountString(payload.TransactionDetails.GrossAmount),
		TransactionTime:   now.Format("2006-01-02 15:04:05"),
		ExpiryTime:        now.Add(time.Hour).Format("2006-01-02 15:04:05"),
		TransactionStatus: "pending",
	}
	s.lastOrderID = transactionData.OrderID
	scenario := s.config.Scenario
	if scenario != ScenarioTimeoutNotFound {
		s.transactions[transactionData.OrderID] = transactionData
	}
	delay := s.config.Delay
	s.mu.Unlock()

	if scenario == ScenarioTimeoutExisting || scenario == ScenarioTimeoutNotFound {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status_code":        "201",
		"status_message":     "Success, transaction is created",
		"transaction_id":     transactionData.TransactionID,
		"order_id":           transactionData.OrderID,
		"gross_amount":       transactionData.GrossAmount,
		"currency":           "IDR",
		"payment_type":       "qris",
		"transaction_time":   transactionData.TransactionTime,
		"transaction_status": transactionData.TransactionStatus,
		"expiry_time":        transactionData.ExpiryTime,
		"actions": []map[string]string{{
			"name":   "generate-qr-code",
			"method": "GET",
			"url":    fmt.Sprintf("%s/v2/qris/%s/qr-code", s.baseURL(r), transactionData.TransactionID),
		}},
	})
}

func (s *Server) v2(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 3 && parts[0] == "v2" && parts[2] == "status" {
		s.status(w, r, parts[1])
		return
	}
	if len(parts) == 4 && parts[0] == "v2" && parts[1] == "qris" && parts[3] == "qr-code" {
		writeJSON(w, http.StatusOK, map[string]string{"qr_string": "mock-qr-code"})
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "endpoint not found"})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request, orderID string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
		return
	}

	s.mu.RLock()
	transactionData, ok := s.transactions[orderID]
	s.mu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"status_code":    "404",
			"status_message": "Transaction not found",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status_code":        "200",
		"status_message":     "Success",
		"transaction_id":     transactionData.TransactionID,
		"order_id":           transactionData.OrderID,
		"gross_amount":       transactionData.GrossAmount,
		"currency":           "IDR",
		"payment_type":       "qris",
		"transaction_time":   transactionData.TransactionTime,
		"transaction_status": transactionData.TransactionStatus,
		"expiry_time":        transactionData.ExpiryTime,
	})
}

func (s *Server) scenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
		return
	}

	var payload struct {
		Scenario Scenario `json:"scenario"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if !validScenario(payload.Scenario) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported scenario"})
		return
	}

	s.mu.Lock()
	s.config.Scenario = payload.Scenario
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"scenario": string(payload.Scenario)})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
		return
	}

	s.mu.RLock()
	transactions := make([]transaction, 0, len(s.transactions))
	for _, item := range s.transactions {
		transactions = append(transactions, item)
	}
	snapshot := struct {
		Scenario     Scenario      `json:"scenario"`
		LastOrderID  string        `json:"last_order_id"`
		Transactions []transaction `json:"transactions"`
	}{
		Scenario:     s.config.Scenario,
		LastOrderID:  s.lastOrderID,
		Transactions: transactions,
	}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) triggerWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
		return
	}
	if s.config.WebhookURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "webhook URL is not configured"})
		return
	}

	var payload webhookRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
	}
	payload = s.completeWebhookPayload(payload)
	if payload.OrderID == "" || payload.TransactionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "order_id and transaction_id are required"})
		return
	}
	if payload.StatusCode == "" {
		payload.StatusCode = "200"
	}
	if payload.TransactionStatus == "" {
		payload.TransactionStatus = "settlement"
	}
	if payload.GrossAmount == "" {
		payload.GrossAmount = "0"
	}

	body := map[string]string{
		"order_id":           payload.OrderID,
		"transaction_id":     payload.TransactionID,
		"gross_amount":       payload.GrossAmount,
		"status_code":        payload.StatusCode,
		"transaction_status": payload.TransactionStatus,
		"transaction_time":   time.Now().Format("2006-01-02 15:04:05"),
		"payment_type":       "qris",
		"currency":           "IDR",
	}
	body["signature_key"] = signature(body["order_id"], body["status_code"], body["gross_amount"], s.config.ServerKey)
	requestBody, err := json.Marshal(body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.config.WebhookURL, strings.NewReader(string(requestBody)))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.httpClient.Do(request)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"message": err.Error()})
		return
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(response.Body)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"webhook_status": response.StatusCode,
		"webhook_body":   string(responseBody),
	})
}

func (s *Server) completeWebhookPayload(payload webhookRequest) webhookRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if payload.OrderID == "" {
		payload.OrderID = s.lastOrderID
	}
	if transactionData, ok := s.transactions[payload.OrderID]; ok {
		if payload.TransactionID == "" {
			payload.TransactionID = transactionData.TransactionID
		}
		if payload.GrossAmount == "" {
			payload.GrossAmount = transactionData.GrossAmount
		}
	}
	return payload
}

func (s *Server) baseURL(r *http.Request) string {
	return fmt.Sprintf("http://%s", r.Host)
}

func validScenario(scenario Scenario) bool {
	return scenario == ScenarioNormal || scenario == ScenarioTimeoutExisting || scenario == ScenarioTimeoutNotFound
}

func amountString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%.0f", typed)
	case json.Number:
		return typed.String()
	default:
		return "0"
	}
}

func signature(orderID, statusCode, grossAmount, serverKey string) string {
	hash := sha512.Sum512([]byte(orderID + statusCode + grossAmount + serverKey))
	return hex.EncodeToString(hash[:])
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
