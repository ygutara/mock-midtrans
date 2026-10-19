# Midtrans Mock Server (TEDB-2467)

Standalone mock server untuk Midtrans DA (Delivery App) API yang digunakan untuk menguji skenario timeout charge, status check recovery, dan webhook race condition.

Server ini dibuat tanpa dependency external (hanya menggunakan Go Standard Library).

---

## Fitur Utama

- **`POST /v2/charge`**: Menerbitkan transaksi pembayaran / QRIS dengan dukungan delay timeout.
- **`GET /v2/{order_id}/status`**: Mengambil status transaksi untuk verifikasi fallback status check.
- **`GET /v2/qris/{transaction_id}/qr-code`**: Mengembalikan string QR mock.
- **`POST /mock/scenario`**: Mengubah skenario simulasi secara dinamis saat server berjalan.
- **`GET /mock/state`**: Melihat daftar transaksi dan state aktif saat ini.
- **`POST /mock/trigger-webhook`**: Mengirim webhook callback ke backend shipment dengan validasi signature SHA-512 otomatis.
- **`GET /health`**: Healthcheck endpoint.

---

## Skenario yang Didukung

| Skenario | Deskripsi |
| :--- | :--- |
| **`normal`** | Charge langsung mengembalikan response 201 (sukses). |
| **`timeout-existing`** | Charge menyimpan transaksi di state mock, lalu menahan response sampai client timeout. Pemanggilan `/v2/{order_id}/status` mengembalikan transaksi (sukses recovery). |
| **`timeout-not-found`** | Charge menahan response tanpa menyimpan transaksi. Pemanggilan `/v2/{order_id}/status` mengembalikan HTTP 404 Not Found (trigger rollback di backend). |

---

## Cara Menjalankan

### 1. Menggunakan `go run`
```bash
go run . \
  -address :9099 \
  -webhook-url http://localhost:8080/v1/midtrans/da/notification \
  -server-key "your-midtrans-server-key" \
  -scenario normal \
  -delay-seconds 15
```

### 2. Menggunakan Environment Variables
```bash
export MOCK_MIDTRANS_ADDRESS=":9099"
export MOCK_MIDTRANS_WEBHOOK_URL="http://localhost:8080/v1/midtrans/da/notification"
export MIDTRANS_DA_SERVER_KEY="your-midtrans-server-key"
export MOCK_MIDTRANS_SCENARIO="timeout-existing"
export MOCK_MIDTRANS_DELAY_SECONDS=15

go run .
```

---

## Integrasi dengan Lionparcel Shipment Service

Pada file konfigurasi shipment (`app.toml`):

```toml
[midtrans_da]
base_url = "http://localhost:9099"
```

*(Jika `base_url` dikosongkan, shipment akan kembali menggunakan Sandbox/Production Midtrans).*

---

## Mengubah Skenario saat Berjalan

Ganti skenario ke `timeout-existing`:
```bash
curl -X POST http://localhost:9099/mock/scenario \
  -H 'Content-Type: application/json' \
  -d '{"scenario":"timeout-existing"}'
```

Ganti skenario ke `timeout-not-found`:
```bash
curl -X POST http://localhost:9099/mock/scenario \
  -H 'Content-Type: application/json' \
  -d '{"scenario":"timeout-not-found"}'
```

Kembali ke `normal`:
```bash
curl -X POST http://localhost:9099/mock/scenario \
  -H 'Content-Type: application/json' \
  -d '{"scenario":"normal"}'
```

---

## Trigger Webhook Manual

Mock server otomatis menghitung `signature_key` sesuai spesifikasi Midtrans (`SHA512(order_id + status_code + gross_amount + server_key)`).

Kirim notifikasi settlement (lunas):
```bash
curl -X POST http://localhost:9099/mock/trigger-webhook \
  -H 'Content-Type: application/json' \
  -d '{
    "transaction_status":"settlement",
    "status_code":"200"
  }'
```

Uji missing transaction (untuk menguji race condition `sql: no rows`):
```bash
curl -X POST http://localhost:9099/mock/trigger-webhook \
  -H 'Content-Type: application/json' \
  -d '{
    "order_id":"STR-UNKNOWN",
    "transaction_id":"mock-unknown",
    "gross_amount":"10000",
    "transaction_status":"settlement",
    "status_code":"200"
  }'
```

---

## Menjalankan Unit Test

```bash
go test -v ./...
```
