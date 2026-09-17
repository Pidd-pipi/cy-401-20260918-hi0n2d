package handler

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/middleware"
	"github.com/gigmatch/gigmatch/internal/service"
	"github.com/gigmatch/gigmatch/internal/util"
)

// ContractHandler exposes contract endpoints.
type ContractHandler struct {
	svc        *service.ContractService
	deliveries *service.ContractDeliveryService
	logger     *slog.Logger
}

// NewContractHandler builds a ContractHandler.
func NewContractHandler(svc *service.ContractService, deliveries *service.ContractDeliveryService, logger *slog.Logger) *ContractHandler {
	return &ContractHandler{svc: svc, deliveries: deliveries, logger: logger}
}

// List handles GET /contracts.
func (h *ContractHandler) List(c *gin.Context) {
	u := middleware.GetCurrentUser(c)
	contracts, err := h.svc.ListByParty(u.ID)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, contracts)
}

// Get handles GET /contracts/:id.
func (h *ContractHandler) Get(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	contract, err := h.svc.Get(id)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, contract)
}

// Sign handles POST /contracts/:id/sign.
func (h *ContractHandler) Sign(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	contract, err := h.svc.Sign(id, u.ID, u.Name)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, contract)
}

// Complete handles POST /contracts/:id/complete.
func (h *ContractHandler) Complete(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	contract, err := h.svc.Complete(id, u.ID, u.Name)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, contract)
}

// SubmitDelivery handles POST /contracts/:id/deliveries (party B submits).
func (h *ContractHandler) SubmitDelivery(c *gin.Context) {
	contractID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req dto.SubmitDeliveryRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	delivery, err := h.deliveries.Submit(contractID, req, u.ID, u.Name)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, delivery)
}

// RejectDelivery handles POST /contracts/:id/deliveries/:deliveryId/reject.
func (h *ContractHandler) RejectDelivery(c *gin.Context) {
	contractID, deliveryID, ok := parseDeliveryParams(c)
	if !ok {
		return
	}
	var req dto.RejectDeliveryRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	delivery, err := h.deliveries.Reject(contractID, deliveryID, req, u.ID, u.Name)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, delivery)
}

// AcceptDelivery handles POST /contracts/:id/deliveries/:deliveryId/accept.
func (h *ContractHandler) AcceptDelivery(c *gin.Context) {
	contractID, deliveryID, ok := parseDeliveryParams(c)
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	delivery, err := h.deliveries.Accept(contractID, deliveryID, u.ID, u.Name)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, delivery)
}

func parseDeliveryParams(c *gin.Context) (uint, uint, bool) {
	contractID, ok := parseUintParam(c, "id")
	if !ok {
		return 0, 0, false
	}
	deliveryID, ok := parseUintParam(c, "deliveryId")
	if !ok {
		return 0, 0, false
	}
	return contractID, deliveryID, true
}
