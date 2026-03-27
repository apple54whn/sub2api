package admin

import (
	"errors"
	"io"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type OpenAIRegisterHandler struct {
	openaiRegisterService *service.OpenAIRegisterService
}

func NewOpenAIRegisterHandler(openaiRegisterService *service.OpenAIRegisterService) *OpenAIRegisterHandler {
	return &OpenAIRegisterHandler{openaiRegisterService: openaiRegisterService}
}

// GetSettings returns DB-backed OpenAI register settings.
// GET /api/v1/admin/openai-register/settings
func (h *OpenAIRegisterHandler) GetSettings(c *gin.Context) {
	settings, err := h.openaiRegisterService.GetSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// UpdateSettings updates DB-backed OpenAI register settings.
// PUT /api/v1/admin/openai-register/settings
func (h *OpenAIRegisterHandler) UpdateSettings(c *gin.Context) {
	var req service.OpenAIRegisterSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	settings, err := h.openaiRegisterService.UpdateSettings(c.Request.Context(), &req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// GetRuntime returns in-memory runtime status for the current process.
// GET /api/v1/admin/openai-register/runtime
func (h *OpenAIRegisterHandler) GetRuntime(c *gin.Context) {
	response.Success(c, h.openaiRegisterService.GetRuntime())
}

// RunCheck manually triggers account status inspection.
// POST /api/v1/admin/openai-register/checks/run
func (h *OpenAIRegisterHandler) RunCheck(c *gin.Context) {
	var req service.OpenAIRegisterRunCheckInput
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.openaiRegisterService.TriggerCheck(c.Request.Context(), &req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
