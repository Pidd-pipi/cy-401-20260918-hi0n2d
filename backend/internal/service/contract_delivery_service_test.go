package service

import (
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

func countDeliveries(t *testing.T, db *gorm.DB, contractID uint) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.ContractDelivery{}).Where("contract_id = ?", contractID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func firstContractID(t *testing.T, svc *ContractService, userID uint) uint {
	t.Helper()
	list, err := svc.ListByParty(userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("no contract found")
	}
	return list[0].ID
}

// TestConcurrentDeliverySubmitAllowsOnlyOne races two submit requests for the
// same contract: exactly one may succeed, the contract must end up
// pending_review and exactly one delivery row may exist (no orphan).
func TestConcurrentDeliverySubmitAllowsOnlyOne(t *testing.T) {
	db, contractSvc, deliverySvc, _, requester, freelancer := deliveryTestKit(t)
	contractID := firstContractID(t, contractSvc, requester.ID)

	const n = 8
	var wg sync.WaitGroup
	var successMu sync.Mutex
	var success, conflict, other int
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, err := deliverySvc.Submit(contractID, submitReq(), freelancer.ID, freelancer.Name)
			successMu.Lock()
			defer successMu.Unlock()
			switch {
			case err == nil:
				success++
			case isConflictAppError(err):
				conflict++
			default:
				other++
				t.Errorf("unexpected submit error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if success != 1 {
		t.Fatalf("success = %d, want exactly 1 (conflict=%d other=%d)", success, conflict, other)
	}
	if success+conflict != n {
		t.Fatalf("accounted = %d, want %d", success+conflict, n)
	}

	loaded, err := contractSvc.Get(contractID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != constants.ContractPendingReview {
		t.Fatalf("contract status = %s, want pending_review", loaded.Status)
	}
	if rows := countDeliveries(t, db, contractID); rows != 1 {
		t.Fatalf("delivery rows = %d, want exactly 1 (no orphan)", rows)
	}
}

// TestConcurrentReviewAllowsOnlyOne races a reject and an accept against the
// same delivery: exactly one may succeed and the final state must match it.
func TestConcurrentReviewAllowsOnlyOne(t *testing.T) {
	cases := []struct {
		name string
	}{
		{"reject-and-accept"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, contractSvc, deliverySvc, _, requester, freelancer := deliveryTestKit(t)
			contractID := firstContractID(t, contractSvc, requester.ID)
			delivery, err := deliverySvc.Submit(contractID, submitReq(), freelancer.ID, freelancer.Name)
			if err != nil {
				t.Fatalf("submit: %v", err)
			}

			var wg sync.WaitGroup
			var successMu sync.Mutex
			var accepted, rejected, conflict int
			start := make(chan struct{})

			runReview := func(accept bool) {
				defer wg.Done()
				<-start
				var err error
				if accept {
					_, err = deliverySvc.Accept(contractID, delivery.ID, requester.ID, requester.Name)
				} else {
					_, err = deliverySvc.Reject(contractID, delivery.ID, dto.RejectDeliveryRequest{Reason: "并发驳回原因"}, requester.ID, requester.Name)
				}
				successMu.Lock()
				defer successMu.Unlock()
				switch {
				case err == nil && accept:
					accepted++
				case err == nil && !accept:
					rejected++
				case isConflictAppError(err):
					conflict++
				default:
					t.Errorf("unexpected review error: %v", err)
				}
			}
			wg.Add(2)
			go runReview(true)
			go runReview(false)
			close(start)
			wg.Wait()

			if accepted+rejected != 1 {
				t.Fatalf("successful reviews = %d (accepted=%d rejected=%d), want exactly 1 (conflict=%d)", accepted+rejected, accepted, rejected, conflict)
			}
			if conflict != 1 {
				t.Fatalf("conflicts = %d, want 1", conflict)
			}

			loaded, err := contractSvc.Get(contractID)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case accepted == 1 && loaded.Status != constants.ContractCompleted:
				t.Fatalf("accept won but contract status = %s, want completed", loaded.Status)
			case rejected == 1 && loaded.Status != constants.ContractInProgress:
				t.Fatalf("reject won but contract status = %s, want in_progress", loaded.Status)
			}
			if loaded.LatestDelivery == nil {
				t.Fatal("latest delivery missing after review")
			}
			if accepted == 1 && loaded.LatestDelivery.Status != constants.DeliveryAccepted {
				t.Fatalf("delivery status = %s, want accepted", loaded.LatestDelivery.Status)
			}
			if rejected == 1 && loaded.LatestDelivery.Status != constants.DeliveryRejected {
				t.Fatalf("delivery status = %s, want rejected", loaded.LatestDelivery.Status)
			}
			// Exactly one delivery row ever existed: the losing review did not
			// create or duplicate anything.
			if rows := countDeliveries(t, db, contractID); rows != 1 {
				t.Fatalf("delivery rows = %d, want 1", rows)
			}
		})
	}
}

func isConflictAppError(err error) bool {
	var appErr *constants.AppError
	return errors.As(err, &appErr) && appErr.Code == constants.CodeConflict
}

func deliveryTestKit(t *testing.T) (*gorm.DB, *ContractService, *ContractDeliveryService, *RequirementService, *model.User, *model.User) {
	t.Helper()
	db := newFlowTestDB(t)
	logger := discardLogger()

	userRepo := repository.NewUserRepository(db)
	reqRepo := repository.NewRequirementRepository(db)
	bidRepo := repository.NewBidRepository(db)
	contractRepo := repository.NewContractRepository(db)
	deliveryRepo := repository.NewContractDeliveryRepository(db)
	logRepo := repository.NewOperationLogRepository(db)

	logSvc := NewOperationLogService(logRepo, logger)
	contractSvc := NewContractService(contractRepo, deliveryRepo, logSvc, logger)
	deliverySvc := NewContractDeliveryService(contractRepo, deliveryRepo, logSvc, logger)
	reqSvc := NewRequirementService(reqRepo, bidRepo, logSvc, logger)
	bidSvc := NewBidService(bidRepo, reqRepo, logSvc, logger)

	requester := &model.User{Username: "req-delivery", PasswordHash: "x", Name: "需求方", Role: constants.RoleRequester}
	freelancer := &model.User{Username: "free-delivery", PasswordHash: "x", Name: "自由职业者", Role: constants.RoleFreelancer}
	if err := userRepo.Create(requester); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(freelancer); err != nil {
		t.Fatal(err)
	}

	requirement, err := reqSvc.Create(dto.CreateRequirementRequest{
		Title: "交付闭环测试需求", Description: "用于测试合同交付验收闭环的需求描述", MinBudget: 10000, MaxBudget: 50000, Skills: []string{"Go"},
	}, requester.ID, requester.Name, requester.Role)
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}
	bid, err := bidSvc.Create(dto.CreateBidRequest{
		RequirementID: requirement.ID, Amount: 30000, DurationDays: 20, Proposal: "按时交付，提供完整源码与文档。",
	}, freelancer.ID, freelancer.Name, freelancer.Role)
	if err != nil {
		t.Fatalf("create bid: %v", err)
	}
	contract, err := reqSvc.AcceptBid(requirement.ID, bid.ID, requester.ID, requester.Name, "installments", contractSvc)
	if err != nil {
		t.Fatalf("accept bid: %v", err)
	}
	if _, err := contractSvc.Sign(contract.ID, freelancer.ID, freelancer.Name); err != nil {
		t.Fatalf("sign contract: %v", err)
	}
	return db, contractSvc, deliverySvc, reqSvc, requester, freelancer
}

func submitReq() dto.SubmitDeliveryRequest {
	return dto.SubmitDeliveryRequest{
		Description: "已完成全部功能开发，附件为源码与部署文档。",
		Attachments: []string{"/uploads/delivery-v1.zip"},
	}
}

func TestDeliverySubmitRejectResubmitAcceptLoop(t *testing.T) {
	_, contractSvc, deliverySvc, _, requester, freelancer := deliveryTestKit(t)

	contract, err := contractSvc.ListByParty(requester.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := contract[0]
	if c.Status != constants.ContractInProgress {
		t.Fatalf("status = %s, want in_progress", c.Status)
	}

	// Party A cannot submit.
	if _, err := deliverySvc.Submit(c.ID, submitReq(), requester.ID, requester.Name); err == nil {
		t.Fatal("party A submit error = nil, want forbidden")
	}

	// Party B submits the delivery.
	delivery, err := deliverySvc.Submit(c.ID, submitReq(), freelancer.ID, freelancer.Name)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if delivery.Status != constants.DeliverySubmitted {
		t.Fatalf("delivery status = %s, want submitted", delivery.Status)
	}
	if len(delivery.Attachments) != 1 || delivery.Attachments[0] != "/uploads/delivery-v1.zip" {
		t.Fatalf("attachments = %v", delivery.Attachments)
	}

	loaded, err := contractSvc.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != constants.ContractPendingReview {
		t.Fatalf("contract status = %s, want pending_review", loaded.Status)
	}
	if loaded.LatestDelivery == nil || loaded.LatestDelivery.ID != delivery.ID {
		t.Fatalf("latest delivery = %+v, want id %d", loaded.LatestDelivery, delivery.ID)
	}

	// Duplicate submit while pending review is rejected.
	if _, err := deliverySvc.Submit(c.ID, submitReq(), freelancer.ID, freelancer.Name); err == nil {
		t.Fatal("duplicate submit error = nil, want conflict")
	}
	// Party B cannot review.
	if _, err := deliverySvc.Accept(c.ID, delivery.ID, freelancer.ID, freelancer.Name); err == nil {
		t.Fatal("party B accept error = nil, want forbidden")
	}

	// Party A rejects with a reason; contract goes back to in_progress.
	rejected, err := deliverySvc.Reject(c.ID, delivery.ID, dto.RejectDeliveryRequest{Reason: "缺少单元测试，请补充"}, requester.ID, requester.Name)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != constants.DeliveryRejected || rejected.RejectReason != "缺少单元测试，请补充" {
		t.Fatalf("rejected = %+v", rejected)
	}
	loaded, _ = contractSvc.Get(c.ID)
	if loaded.Status != constants.ContractInProgress {
		t.Fatalf("contract status = %s, want in_progress after reject", loaded.Status)
	}

	// Re-processing the same delivery must fail.
	if _, err := deliverySvc.Accept(c.ID, delivery.ID, requester.ID, requester.Name); err == nil {
		t.Fatal("accept after reject error = nil, want conflict")
	}

	// Party B can submit again; the new delivery becomes the latest one.
	second, err := deliverySvc.Submit(c.ID, dto.SubmitDeliveryRequest{
		Description: "已补充单元测试与覆盖率报告，重新提交交付。", Attachments: []string{},
	}, freelancer.ID, freelancer.Name)
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	loaded, _ = contractSvc.Get(c.ID)
	if loaded.LatestDelivery == nil || loaded.LatestDelivery.ID != second.ID {
		t.Fatalf("latest delivery id = %v, want %d", loaded.LatestDelivery, second.ID)
	}

	// Party A accepts; contract completes and all stages are done.
	accepted, err := deliverySvc.Accept(c.ID, second.ID, requester.ID, requester.Name)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if accepted.Status != constants.DeliveryAccepted {
		t.Fatalf("delivery status = %s, want accepted", accepted.Status)
	}
	loaded, _ = contractSvc.Get(c.ID)
	if loaded.Status != constants.ContractCompleted {
		t.Fatalf("contract status = %s, want completed", loaded.Status)
	}
	for i, stage := range loaded.Stages {
		if stage.Status != "done" {
			t.Fatalf("stage %d status = %s, want done", i, stage.Status)
		}
	}

	// No further submit or review once completed.
	if _, err := deliverySvc.Submit(c.ID, submitReq(), freelancer.ID, freelancer.Name); err == nil {
		t.Fatal("submit after complete error = nil, want conflict")
	}
	if _, err := deliverySvc.Reject(c.ID, second.ID, dto.RejectDeliveryRequest{Reason: "再次驳回"}, requester.ID, requester.Name); err == nil {
		t.Fatal("reject after accept error = nil, want conflict")
	}
}

func TestDeliverySubmitGuardsContractState(t *testing.T) {
	db := newFlowTestDB(t)
	logger := discardLogger()
	userRepo := repository.NewUserRepository(db)
	contractRepo := repository.NewContractRepository(db)
	deliveryRepo := repository.NewContractDeliveryRepository(db)
	logRepo := repository.NewOperationLogRepository(db)
	logSvc := NewOperationLogService(logRepo, logger)
	contractSvc := NewContractService(contractRepo, deliveryRepo, logSvc, logger)
	deliverySvc := NewContractDeliveryService(contractRepo, deliveryRepo, logSvc, logger)

	requester := &model.User{Username: "req-guard", PasswordHash: "x", Name: "需求方", Role: constants.RoleRequester}
	freelancer := &model.User{Username: "free-guard", PasswordHash: "x", Name: "自由职业者", Role: constants.RoleFreelancer}
	if err := userRepo.Create(requester); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(freelancer); err != nil {
		t.Fatal(err)
	}
	contract := &model.Contract{
		ContractNo:    "CY-GUARD-1",
		TotalAmount:   1000,
		PaymentType:   "one_time",
		Stages:        []model.ContractStage{{Name: "验收结项", Amount: 1000, Status: "pending"}},
		Status:        constants.ContractPendingSignature,
		RequirementID: 1,
		PartyAID:      requester.ID,
		PartyBID:      freelancer.ID,
	}
	if err := contractRepo.Create(contract); err != nil {
		t.Fatal(err)
	}

	// Before signature the contract is not in progress: submit must fail and
	// must not leave an orphan delivery behind.
	if _, err := deliverySvc.Submit(contract.ID, submitReq(), freelancer.ID, freelancer.Name); err == nil {
		t.Fatal("submit on pending_signature error = nil, want conflict")
	}
	if count := countDeliveries(t, db, contract.ID); count != 0 {
		t.Fatalf("delivery rows = %d, want 0 (no orphan)", count)
	}

	// Outsider cannot touch the contract.
	outsider := &model.User{Username: "outsider", PasswordHash: "x", Name: "外人", Role: constants.RoleFreelancer}
	if err := userRepo.Create(outsider); err != nil {
		t.Fatal(err)
	}
	if _, err := contractSvc.Sign(contract.ID, requester.ID, requester.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := deliverySvc.Submit(contract.ID, submitReq(), outsider.ID, outsider.Name); !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("outsider submit err = %v, want forbidden", err)
	}
}
