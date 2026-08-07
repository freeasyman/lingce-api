package content

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Publish Task Handlers

// GetPublishDashboard handles getting publish dashboard
func (h *Handler) GetPublishDashboard(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var tenantID *int64
	tenantIDParam := r.URL.Query().Get("tenant_id")
	if tenantIDParam != "" || claims.TenantID != nil {
		parsed, err := tenancy.RequireTenantID(claims, tenantIDParam)
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		tenantID = &parsed
	}

	resp, err := h.service.GetPublishDashboard(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// ListPublishTasks handles listing publish tasks
func (h *Handler) ListPublishTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req PublishTaskListRequest
	tenantIDParam := r.URL.Query().Get("tenant_id")
	if tenantIDParam != "" || claims.TenantID != nil {
		parsed, err := tenancy.RequireTenantID(claims, tenantIDParam)
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		req.TenantID = &parsed
	}

	if contentIDStr := r.URL.Query().Get("content_id"); contentIDStr != "" {
		if parsed, err := strconv.ParseInt(contentIDStr, 10, 64); err == nil {
			req.ContentID = &parsed
		}
	}
	if platform := r.URL.Query().Get("platform"); platform != "" {
		req.Platform = &platform
	}
	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}
	if createdByStr := r.URL.Query().Get("created_by"); createdByStr != "" {
		if parsed, err := strconv.ParseInt(createdByStr, 10, 64); err == nil {
			req.CreatedBy = &parsed
		}
	}
	if startDate := r.URL.Query().Get("start_date"); startDate != "" {
		req.StartDate = &startDate
	}
	if endDate := r.URL.Query().Get("end_date"); endDate != "" {
		req.EndDate = &endDate
	}
	req.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	req.PageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))

	tasks, total, err := h.service.ListPublishTasks(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, tasks, int64(total), req.Page, req.PageSize)
}

// GetPublishTask handles getting publish task details
func (h *Handler) GetPublishTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	taskID, err := strconv.ParseInt(r.PathValue("task_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}

	task, err := h.service.GetPublishTaskByID(r.Context(), taskID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, task.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	httputil.WriteSuccess(w, task)
}

// CreatePublishTask handles creating a publish task
func (h *Handler) CreatePublishTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreatePublishTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID, err := tenancy.RequireTenantID(claims, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	task, err := h.service.CreatePublishTask(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, task)
}

// BatchCreatePublishTasks handles batch creating publish tasks
func (h *Handler) BatchCreatePublishTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req BatchCreatePublishTasksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID, err := tenancy.RequireTenantID(claims, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	tasks, err := h.service.BatchCreatePublishTasks(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"created": len(tasks),
		"tasks":   tasks,
		"message": "Publish tasks created successfully",
	})
}

// UpdatePublishTaskStatus handles updating publish task status
func (h *Handler) UpdatePublishTaskStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	taskID, err := strconv.ParseInt(r.PathValue("task_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}

	var req UpdateTaskStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	existing, err := h.service.GetPublishTaskByID(r.Context(), taskID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, existing.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	task, err := h.service.UpdatePublishTaskStatus(r.Context(), taskID, req.Status)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, task)
}

// CancelPublishTask handles canceling a publish task
func (h *Handler) CancelPublishTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	taskID, err := strconv.ParseInt(r.PathValue("task_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}

	existing, err := h.service.GetPublishTaskByID(r.Context(), taskID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, existing.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	task, err := h.service.UpdatePublishTaskStatus(r.Context(), taskID, "cancelled")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, task)
}

// RetryPublishTask handles retrying a failed publish task
func (h *Handler) RetryPublishTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	taskID, err := strconv.ParseInt(r.PathValue("task_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}

	existing, err := h.service.GetPublishTaskByID(r.Context(), taskID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, existing.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	task, err := h.service.UpdatePublishTaskStatus(r.Context(), taskID, "pending")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, task)
}

// DeletePublishTask handles deleting a publish task
func (h *Handler) DeletePublishTask(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can delete publish tasks
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	taskID, err := strconv.ParseInt(r.PathValue("task_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}

	if err := h.service.DeletePublishTask(r.Context(), taskID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Task deleted successfully"})
}
