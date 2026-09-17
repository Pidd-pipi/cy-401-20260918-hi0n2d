package service

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

// ContractDeliveryService runs the delivery acceptance loop of contracts:
// party B submits deliveries and party A rejects (with a reason) or accepts.
type ContractDeliveryService struct {
	contracts  *repository.ContractRepository
	deliveries *repository.ContractDeliveryRepository
	logs       *OperationLogService
	logger     *slog.Logger
}

// NewContractDeliveryService builds a ContractDeliveryService.
func NewContractDeliveryService(contracts *repository.ContractRepository, deliveries *repository.ContractDeliveryRepository, logs *OperationLogService, logger *slog.Logger) *ContractDeliveryService {
	return &ContractDeliveryService{contracts: contracts, deliveries: deliveries, logs: logs, logger: logger}
}

// GetLatest returns the latest delivery of a contract.
func (s *ContractDeliveryService) GetLatest(contractID uint) (*model.ContractDelivery, error) {
	delivery, err := s.deliveries.FindLatestByContractID(contractID)
	if err != nil {
		return nil, err
	}
	return delivery, nil
}

// Submit lets party B of an in-progress contract submit a delivery. The
// contract status flip (in_progress -> pending_review) and the delivery insert
// share one transaction, so they succeed or fail together.
func (s *ContractDeliveryService) Submit(contractID uint, req dto.SubmitDeliveryRequest, userID uint, userName string) (*model.ContractDelivery, error) {
	c, err := s.loadContractForParty(contractID, userID)
	if err != nil {
		return nil, err
	}
	if c.PartyBID != userID {
		return nil, constants.ErrForbidden
	}
	if c.Status != constants.ContractInProgress {
		if c.Status == constants.ContractPendingReview {
			return nil, constants.NewAppError(constants.CodeConflict, "已有待验收交付，不可重复提交")
		}
		return nil, constants.NewAppError(constants.CodeConflict, "合同当前不可提交交付")
	}

	attachments := req.Attachments
	if attachments == nil {
		attachments = []string{}
	}
	delivery := &model.ContractDelivery{
		ContractID:  c.ID,
		SubmitterID: userID,
		Description: req.Description,
		Attachments: attachments,
		Status:      constants.DeliverySubmitted,
	}
	if err := s.deliveries.Submit(c, constants.ContractInProgress, constants.ContractPendingReview, delivery); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, constants.NewAppError(constants.CodeConflict, "已有待验收交付，不可重复提交")
		}
		return nil, fmt.Errorf("submit delivery: %w", err)
	}
	s.logs.Record(userID, userName, "delivery.submit", "contract_delivery", delivery.ID, fmt.Sprintf("提交合同 %s 的交付", c.ContractNo))
	return delivery, nil
}

// Reject lets party A send a pending delivery back. A reason is required and
// the contract returns to in_progress, all in one transaction.
func (s *ContractDeliveryService) Reject(contractID, deliveryID uint, req dto.RejectDeliveryRequest, userID uint, userName string) (*model.ContractDelivery, error) {
	return s.review(contractID, deliveryID, userID, userName, false, req.Reason)
}

// Accept lets party A approve a pending delivery; the contract becomes
// completed in the same transaction.
func (s *ContractDeliveryService) Accept(contractID, deliveryID uint, userID uint, userName string) (*model.ContractDelivery, error) {
	return s.review(contractID, deliveryID, userID, userName, true, "")
}

func (s *ContractDeliveryService) review(contractID, deliveryID, userID uint, userName string, accept bool, reason string) (*model.ContractDelivery, error) {
	c, err := s.loadContractForParty(contractID, userID)
	if err != nil {
		return nil, err
	}
	if c.PartyAID != userID {
		return nil, constants.ErrForbidden
	}
	delivery, err := s.deliveries.FindByID(deliveryID)
	if err != nil {
		return nil, err
	}
	if delivery.ContractID != contractID {
		return nil, repository.ErrNotFound
	}

	now := time.Now()
	var (
		deliveryStatus string
		contractStatus string
		action, detail string
		fields         map[string]any
	)
	if accept {
		deliveryStatus = constants.DeliveryAccepted
		contractStatus = constants.ContractCompleted
		action = "delivery.accept"
		detail = fmt.Sprintf("接受合同 %s 的交付", c.ContractNo)
		fields = map[string]any{"status": deliveryStatus, "reviewer_id": userID, "reviewed_at": &now}
		for i := range c.Stages {
			c.Stages[i].Status = "done"
		}
	} else {
		deliveryStatus = constants.DeliveryRejected
		contractStatus = constants.ContractInProgress
		action = "delivery.reject"
		detail = fmt.Sprintf("驳回合同 %s 的交付：%s", c.ContractNo, reason)
		fields = map[string]any{"status": deliveryStatus, "reviewer_id": userID, "reviewed_at": &now, "reject_reason": reason}
	}

	if c.Status != constants.ContractPendingReview || delivery.Status != constants.DeliverySubmitted {
		return nil, constants.NewAppError(constants.CodeConflict, "该交付已处理，请勿重复操作")
	}
	c.Status = contractStatus
	if err := s.deliveries.Review(delivery.ID, fields, c, constants.ContractPendingReview, contractStatus); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, constants.NewAppError(constants.CodeConflict, "该交付已处理，请勿重复操作")
		}
		return nil, fmt.Errorf("review delivery: %w", err)
	}
	delivery.Status = deliveryStatus
	delivery.ReviewerID = userID
	delivery.ReviewedAt = &now
	if !accept {
		delivery.RejectReason = reason
	}
	s.logs.Record(userID, userName, action, "contract_delivery", delivery.ID, detail)
	return delivery, nil
}

func (s *ContractDeliveryService) loadContractForParty(contractID, userID uint) (*model.Contract, error) {
	c, err := s.contracts.FindByID(contractID)
	if err != nil {
		return nil, err
	}
	if c.PartyAID != userID && c.PartyBID != userID {
		return nil, constants.ErrForbidden
	}
	return c, nil
}
