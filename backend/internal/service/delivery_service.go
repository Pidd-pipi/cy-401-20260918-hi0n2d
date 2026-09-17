package service

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

// DeliveryService implements the contract delivery acceptance loop:
// party B submits a delivery (contract -> pending_review), party A either
// rejects it (delivery rejected, contract -> in_progress, reason required) or
// accepts it (delivery accepted, contract -> completed).
type DeliveryService struct {
	deliveries *repository.ContractDeliveryRepository
	contracts  *repository.ContractRepository
	logs       *repository.OperationLogRepository
	logger     *slog.Logger
}

// NewDeliveryService builds a DeliveryService.
func NewDeliveryService(deliveries *repository.ContractDeliveryRepository, contracts *repository.ContractRepository, logs *repository.OperationLogRepository, logger *slog.Logger) *DeliveryService {
	return &DeliveryService{deliveries: deliveries, contracts: contracts, logs: logs, logger: logger}
}

// Submit lets party B hand in a delivery for an in-progress contract.
// The delivery row and the contract status change commit in one transaction.
func (s *DeliveryService) Submit(contractID uint, req dto.SubmitDeliveryRequest, userID uint, userName string) (*model.ContractDelivery, error) {
	description := strings.TrimSpace(req.Description)
	if description == "" {
		return nil, constants.NewAppError(constants.CodeBadRequest, "交付说明不能为空")
	}
	attachments := req.Attachments
	if attachments == nil {
		attachments = []string{}
	}

	var deliveryID uint
	err := s.deliveries.InTx(func(tx *gorm.DB) error {
		contracts := s.contracts.WithTx(tx)
		deliveries := s.deliveries.WithTx(tx)

		c, err := contracts.FindByID(contractID)
		if err != nil {
			return err
		}
		if c.PartyBID != userID {
			return constants.ErrForbidden
		}
		if c.Status != constants.ContractInProgress {
			return constants.NewAppError(constants.CodeConflict, "仅执行中的合同可以提交交付，待验收期间不可重复提交")
		}

		revision, err := deliveries.LatestRevision(contractID)
		if err != nil {
			return fmt.Errorf("load latest revision: %w", err)
		}

		// Atomic guard: only a contract still in_progress can enter review.
		if err := s.contracts.TransitionStatus(tx, contractID,
			[]string{constants.ContractInProgress}, constants.ContractPendingReview); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return constants.NewAppError(constants.CodeConflict, "提交冲突，交付可能已被提交，请刷新后重试")
			}
			return err
		}

		now := time.Now()
		delivery := &model.ContractDelivery{
			ContractID:    contractID,
			Revision:      revision + 1,
			Description:   description,
			Attachments:   attachments,
			Status:        constants.DeliverySubmitted,
			SubmittedByID: userID,
			SubmittedAt:   now,
		}
		if err := deliveries.Create(delivery); err != nil {
			return fmt.Errorf("create delivery: %w", err)
		}
		deliveryID = delivery.ID

		return s.writeLog(tx, userID, userName, "delivery.submit", contractID,
			fmt.Sprintf("提交交付说明（第 %d 次）", delivery.Revision))
	})
	if err != nil {
		return nil, err
	}

	delivery, err := s.deliveries.FindByID(deliveryID)
	if err != nil {
		return nil, fmt.Errorf("reload delivery: %w", err)
	}
	return delivery, nil
}

// Review lets party A reject or accept the latest pending delivery. The review
// result, the contract status transition and the audit log share one
// transaction, so they all succeed or all fail together.
func (s *DeliveryService) Review(contractID uint, req dto.ReviewDeliveryRequest, userID uint, userName string) (*model.ContractDelivery, error) {
	if !constants.ValidDeliveryAction(req.Action) {
		return nil, constants.NewAppError(constants.CodeBadRequest, "无效的验收操作")
	}
	reason := strings.TrimSpace(req.RejectReason)
	if req.Action == constants.DeliveryActionReject && reason == "" {
		return nil, constants.NewAppError(constants.CodeBadRequest, "驳回时必须填写驳回原因")
	}

	var deliveryID uint
	err := s.deliveries.InTx(func(tx *gorm.DB) error {
		contracts := s.contracts.WithTx(tx)
		deliveries := s.deliveries.WithTx(tx)

		c, err := contracts.FindByID(contractID)
		if err != nil {
			return err
		}
		if c.PartyAID != userID {
			return constants.ErrForbidden
		}
		if c.Status != constants.ContractPendingReview {
			return constants.NewAppError(constants.CodeConflict, "当前没有待验收的交付，不可重复处理")
		}

		delivery, err := deliveries.FindLatestByContract(contractID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return constants.NewAppError(constants.CodeConflict, "当前没有待验收的交付，不可重复处理")
			}
			return err
		}
		if delivery.Status != constants.DeliverySubmitted {
			return constants.NewAppError(constants.CodeConflict, "该交付已处理，不可重复操作")
		}
		deliveryID = delivery.ID

		// The contract status CAS is the serialization point: it takes the
		// contract row lock first, so concurrent reject/accept requests are
		// forced into a strict order and only one can win.
		var targetStatus, logAction, logDetail string
		switch req.Action {
		case constants.DeliveryActionAccept:
			targetStatus = constants.ContractCompleted
			logAction = "delivery.accept"
			logDetail = fmt.Sprintf("验收通过第 %d 次交付", delivery.Revision)
		case constants.DeliveryActionReject:
			targetStatus = constants.ContractInProgress
			logAction = "delivery.reject"
			logDetail = fmt.Sprintf("驳回第 %d 次交付：%s", delivery.Revision, reason)
		default:
			return constants.NewAppError(constants.CodeBadRequest, "无效的验收操作")
		}
		if err := s.contracts.TransitionStatus(tx, contractID,
			[]string{constants.ContractPendingReview}, targetStatus); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return constants.NewAppError(constants.CodeConflict, "处理冲突，交付状态可能已变更，请刷新后重试")
			}
			return err
		}

		now := time.Now()
		if req.Action == constants.DeliveryActionAccept {
			for i := range c.Stages {
				c.Stages[i].Status = "done"
			}
			if err := s.contracts.MarkStagesDone(tx, contractID, c.Stages); err != nil {
				return fmt.Errorf("mark stages done: %w", err)
			}
		}

		// Conditional write: a delivery that is no longer "submitted" cannot be
		// reviewed a second time, even if two transactions passed earlier checks.
		if err := deliveries.ApplyReview(tx, delivery.ID,
			reviewStatus(req.Action), reason, userID, now); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return constants.NewAppError(constants.CodeConflict, "该交付已处理，不可重复操作")
			}
			return fmt.Errorf("apply review: %w", err)
		}
		return s.writeLog(tx, userID, userName, logAction, contractID, logDetail)
	})
	if err != nil {
		return nil, err
	}

	delivery, err := s.deliveries.FindByID(deliveryID)
	if err != nil {
		return nil, fmt.Errorf("reload delivery: %w", err)
	}
	return delivery, nil
}

func reviewStatus(action string) string {
	if action == constants.DeliveryActionReject {
		return constants.DeliveryRejected
	}
	return constants.DeliveryAccepted
}

// Latest returns the newest delivery of a contract. It returns (nil, nil) when
// the contract has no delivery yet. Only the two parties may read it.
func (s *DeliveryService) Latest(contractID, userID uint) (*model.ContractDelivery, error) {
	c, err := s.contracts.FindByID(contractID)
	if err != nil {
		return nil, err
	}
	if c.PartyAID != userID && c.PartyBID != userID {
		return nil, constants.ErrForbidden
	}
	delivery, err := s.deliveries.FindLatestByContract(contractID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("find latest delivery: %w", err)
	}
	return delivery, nil
}

func (s *DeliveryService) writeLog(tx *gorm.DB, userID uint, userName, action string, contractID uint, detail string) error {
	entry := &model.OperationLog{
		UserID:   userID,
		UserName: userName,
		Action:   action,
		Entity:   "contract",
		EntityID: contractID,
		Detail:   detail,
	}
	if err := s.logs.WithTx(tx).Create(entry); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}
