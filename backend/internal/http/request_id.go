package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stdhttp "net/http"
	"strings"
)

type requestIDContextKey struct{}

const requestIDHeader = "X-Request-ID"

func requestIDMiddleware() middleware {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
			if requestID == "" {
				requestID = newRequestID()
			}

			w.Header().Set(requestIDHeader, requestID)

			ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requestIDFromContext(ctx context.Context) string {
	value, ok := ctx.Value(requestIDContextKey{}).(string)
	if !ok {
		return ""
	}

	return value
}

func newRequestID() string {
	buffer := make([]byte, 16)

	if _, err := rand.Read(buffer); err != nil {
		return "request-id-unavailable"
	}

	return hex.EncodeToString(buffer)
}
