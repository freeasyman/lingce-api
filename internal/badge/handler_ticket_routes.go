package badge

import "net/http"

func (h *Handler) registerTicketRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	// Resource-oriented badge-ticket endpoints
	mux.Handle("POST /api/v1/badge-tickets", authMw(http.HandlerFunc(h.SubmitTicket)))
	mux.Handle("POST /api/v1/badge-tickets/by-device", authMw(http.HandlerFunc(h.SubmitTicketByDevice)))
	mux.Handle("GET /api/v1/badge-tickets/my", authMw(http.HandlerFunc(h.GetMyTickets)))
	mux.Handle("GET /api/v1/badge-tickets", authMw(http.HandlerFunc(h.ListTickets)))
	mux.Handle("GET /api/v1/badge-tickets/{id}", authMw(http.HandlerFunc(h.GetTicket)))
	mux.Handle("POST /api/v1/badge-tickets/{ticket_id}/actions/review", authMw(http.HandlerFunc(h.ReviewTicket)))
	mux.Handle("POST /api/v1/badge-tickets/{ticket_id}/actions/execute", authMw(http.HandlerFunc(h.ExecuteTicket)))

	// Legacy proxy endpoints (Phase 2 compatibility)
	h.registerLegacyTicketProxyRoutes(mux, authMw)
}

func (h *Handler) registerLegacyTicketProxyRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/badge-control/tickets/submit", withDeprecation(authMw(http.HandlerFunc(h.SubmitTicket))))
	mux.Handle("POST /api/v1/badge-control/tickets/submit-by-device", withDeprecation(authMw(http.HandlerFunc(h.SubmitTicketByDevice))))
	mux.Handle("GET /api/v1/badge-control/tickets/my", withDeprecation(authMw(http.HandlerFunc(h.GetMyTickets))))
	mux.Handle("GET /api/v1/badge-control/tickets", withDeprecation(authMw(http.HandlerFunc(h.ListTickets))))
	mux.Handle("GET /api/v1/badge-control/tickets/{id}", withDeprecation(authMw(http.HandlerFunc(h.GetTicket))))
	mux.Handle("POST /api/v1/badge-control/tickets/{ticket_id}/review", withDeprecation(authMw(http.HandlerFunc(h.ReviewTicket))))
	mux.Handle("POST /api/v1/badge-control/tickets/{ticket_id}/execute", withDeprecation(authMw(http.HandlerFunc(h.ExecuteTicket))))
}

func withDeprecation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Sunset", "Tue, 30 Jun 2026 23:59:59 GMT")
		w.Header().Set("Link", `</api/v1/badge-tickets>; rel="successor-version"`)
		next.ServeHTTP(w, r)
	})
}
