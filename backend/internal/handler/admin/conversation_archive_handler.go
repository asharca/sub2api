package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type createConversationArchiveRequest struct {
	DeleteAfterBackup bool `json:"delete_after_backup"`
}

func (h *ConversationLogHandler) Export(c *gin.Context) {
	filters, ok := parseConversationLogFilters(c)
	if !ok {
		return
	}
	if filters.StartTime != nil && filters.EndTime != nil && !filters.StartTime.Before(*filters.EndTime) {
		response.BadRequest(c, "start_date must not be after end_date")
		return
	}
	var req struct{}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	export, err := h.conversationLogService.ExportConversationLogs(c.Request.Context(), filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = export.Close() }()
	c.DataFromReader(http.StatusOK, export.SizeBytes, "application/gzip", export.File, map[string]string{
		"Content-Disposition": `attachment; filename="` + export.FileName + `"`,
		"Cache-Control":       "no-store",
	})
}

func (h *ConversationLogHandler) CreateArchive(c *gin.Context) {
	filters, ok := parseConversationLogFilters(c)
	if !ok {
		return
	}
	if filters.StartTime != nil && filters.EndTime != nil && !filters.StartTime.Before(*filters.EndTime) {
		response.BadRequest(c, "start_date must not be after end_date")
		return
	}
	var req createConversationArchiveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	record, err := h.backupService.StartConversationArchive(c.Request.Context(), h.conversationLogService, filters, req.DeleteAfterBackup)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, record)
}

func (h *ConversationLogHandler) RetryArchive(c *gin.Context) {
	var req struct{}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	record, err := h.backupService.RetryConversationArchive(c.Request.Context(), h.conversationLogService, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, record)
}

func (h *ConversationLogHandler) DeleteArchive(c *gin.Context) {
	if err := h.backupService.DeleteConversationArchive(c.Request.Context(), c.Param("id")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, nil)
}

func (h *ConversationLogHandler) ListArchives(c *gin.Context) {
	records, err := h.backupService.ListConversationArchives(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": records})
}

func (h *ConversationLogHandler) GetArchive(c *gin.Context) {
	record, err := h.backupService.GetConversationArchive(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *ConversationLogHandler) GetArchiveDownloadURL(c *gin.Context) {
	url, err := h.backupService.GetConversationArchiveDownloadURL(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"url": url})
}
