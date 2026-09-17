package handler

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/middleware"
	"github.com/gigmatch/gigmatch/internal/service"
	"github.com/gigmatch/gigmatch/internal/util"
)

// DeliveryHandler exposes the contract delivery acceptance endpoints.
type DeliveryHandler struct {
	svc    *service.DeliveryService
	logger *slog.Logger
}

// NewDeliveryHandler builds a DeliveryHandler.
func NewDeliveryHandler(svc *service.DeliveryService, logger *slog.Logger) *DeliveryHandler {
	return &DeliveryHandler{svc: svc, logger: logger}
}

// Submit handles POST /contracts/:id/deliveries.
func (h *DeliveryHandler) Submit(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req dto.SubmitDeliveryRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	delivery, err := h.svc.Submit(id, req, u.ID, u.Name)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, delivery)
}

// Latest handles GET /contracts/:id/deliveries/latest.
func (h *DeliveryHandler) Latest(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	delivery, err := h.svc.Latest(id, u.ID)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, delivery)
}

// Review handles POST /contracts/:id/deliveries/review.
func (h *DeliveryHandler) Review(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req dto.ReviewDeliveryRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	delivery, err := h.svc.Review(id, req, u.ID, u.Name)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, delivery)
}
