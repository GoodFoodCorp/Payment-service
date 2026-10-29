package http

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"goodfood/payment-service/internal/application"
)

type contextKey string

const (
	ctxKeyActor     contextKey = "actor"
	ctxKeyRequestID contextKey = "request_id"
	headerRequestID            = "X-Request-ID"
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(headerRequestID, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

func Logger(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			reqID, _ := r.Context().Value(ctxKeyRequestID).(string)
			log.Info().Str("request_id", reqID).Str("method", r.Method).
				Str("path", r.URL.Path).Int("status", rw.status).
				Dur("duration_ms", time.Since(start)).Msg("http_request")
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// JWTConfig is what an access token must match.
type JWTConfig struct {
	Secret   string
	Issuer   string
	Audience string
}

// Auth validates the HS256 access token issued by auth-service (header or
// auth_token cookie) and stores the Actor. Refresh tokens, tokens for another
// audience, unsigned or expired tokens are all refused.
func Auth(cfg JWTConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString := ""
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				tokenString = strings.TrimPrefix(h, "Bearer ")
			} else if c, err := r.Cookie("auth_token"); err == nil {
				tokenString = c.Value
			}
			if tokenString == "" {
				writeError(w, r, http.StatusUnauthorized, "missing authentication token")
				return
			}
			claims := jwt.MapClaims{}
			token, err := jwt.ParseWithClaims(tokenString, claims,
				func(*jwt.Token) (any, error) { return []byte(cfg.Secret), nil },
				jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
				jwt.WithIssuer(cfg.Issuer),
				jwt.WithAudience(cfg.Audience),
				jwt.WithExpirationRequired(),
				jwt.WithLeeway(30*time.Second),
			)
			if err != nil || !token.Valid || claims["typ"] != "access" {
				writeError(w, r, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			actor := application.Actor{}
			if sub, ok := claims["sub"].(string); ok {
				actor.UserID = sub
			}
			if slugs, ok := claims["role_slugs"].([]interface{}); ok {
				for _, s := range slugs {
					if str, ok := s.(string); ok {
						actor.RoleSlugs = append(actor.RoleSlugs, strings.ToLower(str))
					}
				}
			}
			if actor.UserID == "" {
				writeError(w, r, http.StatusUnauthorized, "token has no subject")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyActor, actor)))
		})
	}
}

// ServiceKeyHeader carries the shared secret of service-to-service calls.
const ServiceKeyHeader = "X-Service-Key"

// ServiceKey guards the /internal routes (order-service only).
func ServiceKey(expected string) func(http.Handler) http.Handler {
	expectedHash := sha256.Sum256([]byte(expected))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := r.Header.Get(ServiceKeyHeader)
			providedHash := sha256.Sum256([]byte(provided))
			if expected == "" || provided == "" || subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) != 1 {
				writeError(w, r, http.StatusUnauthorized, "invalid or missing service key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func actorFrom(r *http.Request) application.Actor {
	actor, _ := r.Context().Value(ctxKeyActor).(application.Actor)
	return actor
}
