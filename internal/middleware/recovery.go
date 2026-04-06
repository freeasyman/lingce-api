package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Recovery recovers from panics and returns 500 error
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered",
					"error", err,
					"stack", string(debug.Stack()),
				)
				httputil.WriteInternalError(w, "Internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
