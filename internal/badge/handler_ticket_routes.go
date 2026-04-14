package badge

import "net/http"

func (h *Handler) registerTicketRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	// Resource-oriented badge-ticket endpoints
	mux.Handle("POST /api/v1/badge-tickets", authMw(http.HandlerFunc(h.SubmitTicket)))
	mux.Handle("POST /api/v1/badge-tickets/by-device", authMw(http.HandlerFunc(h.SubmitTicketByDevice)))
	mux.Handle("GET /api/v1/badge-tickets/my", authMw(http.HandlerFunc(h.GetMyTickets)))
	mux.Handle("GET /api/v1/badge-tickets", authMw(http.HandlerFunc(h.ListTickets)))
	mux.Handle("POST /api/v1/badge-tickets/{ticket_id}/actions/review", authMw(http.HandlerFunc(h.ReviewTicket)))
	mux.Handle("POST /api/v1/badge-tickets/{ticket_id}/actions/execute", authMw(http.HandlerFunc(h.ExecuteTicket)))
}
