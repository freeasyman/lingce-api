package badge

import "net/http"

func (h *Handler) registerTicketRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/badge-tickets", authMw(http.HandlerFunc(h.SubmitTicket)))
	mux.Handle("GET /api/v1/badge-tickets/my", authMw(http.HandlerFunc(h.GetMyTickets)))
	mux.Handle("GET /api/v1/badge-tickets", authMw(http.HandlerFunc(h.ListTickets)))
	mux.Handle("GET /api/v1/badge-tickets/{id}", authMw(http.HandlerFunc(h.GetTicket)))
	mux.Handle("POST /api/v1/badge-tickets/{ticket_id}/actions/review", authMw(http.HandlerFunc(h.ReviewTicket)))
	mux.Handle("POST /api/v1/badge-tickets/{ticket_id}/actions/execute", authMw(http.HandlerFunc(h.ExecuteTicket)))
}
