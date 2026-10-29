package stripe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"goodfood/payment-service/internal/domain"
)

// webhookTolerance bounds the age of a signed event, against replays.
const webhookTolerance = 5 * time.Minute

// ErrInvalidSignature is returned for any webhook that must be rejected.
var ErrInvalidSignature = errors.New("invalid webhook signature")

// WebhookVerifier checks the Stripe-Signature header (HMAC-SHA256 of
// "timestamp.payload" with the endpoint secret) and decodes the event.
type WebhookVerifier struct {
	secret string
	now    func() time.Time
}

func NewWebhookVerifier(secret string) *WebhookVerifier {
	return &WebhookVerifier{secret: secret, now: time.Now}
}

type webhookEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object struct {
			ID            string `json:"id"`
			Object        string `json:"object"`
			PaymentIntent string `json:"payment_intent"`
		} `json:"object"`
	} `json:"data"`
}

// Verify authenticates the payload and maps it to a GatewayEvent. The intent
// id comes from the object itself for payment_intent.* events and from the
// charge's payment_intent field for charge.* events.
func (v *WebhookVerifier) Verify(payload []byte, signatureHeader string) (*domain.GatewayEvent, error) {
	if v.secret == "" {
		return nil, ErrInvalidSignature
	}
	timestamp, signatures := parseSignatureHeader(signatureHeader)
	if timestamp == 0 || len(signatures) == 0 {
		return nil, ErrInvalidSignature
	}
	age := v.now().Sub(time.Unix(timestamp, 0))
	if age > webhookTolerance || age < -webhookTolerance {
		return nil, ErrInvalidSignature
	}
	expected := sign(v.secret, timestamp, payload)
	valid := false
	for _, s := range signatures {
		if hmac.Equal([]byte(s), []byte(expected)) {
			valid = true
		}
	}
	if !valid {
		return nil, ErrInvalidSignature
	}

	var ev webhookEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil, err
	}
	intentID := ev.Data.Object.ID
	if ev.Data.Object.Object != "payment_intent" {
		intentID = ev.Data.Object.PaymentIntent
	}
	return &domain.GatewayEvent{ID: ev.ID, Type: ev.Type, IntentID: intentID}, nil
}

func parseSignatureHeader(header string) (int64, []string) {
	var timestamp int64
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			timestamp, _ = strconv.ParseInt(value, 10, 64)
		case "v1":
			signatures = append(signatures, value)
		}
	}
	return timestamp, signatures
}

func sign(secret string, timestamp int64, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
