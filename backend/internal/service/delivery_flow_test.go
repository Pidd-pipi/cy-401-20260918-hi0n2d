package service

import (
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

func newDeliveryFixture(t *testing.T) (*gormFixture, *model.Contract) {
	t.Helper()
	db := newFlowTestDB(t)
	f := &gormFixture{
		db:           db,
		users:        repository.NewUserRepository(db),
		requirements: repository.NewRequirementRepository(db),
		bids:         repository.NewBidRepository(db),
		contracts:    repository.NewContractRepository(db),
		deliveries:   repository.NewContractDeliveryRepository(db),
		logs:         repository.NewOperationLogRepository(db),
	}

	suffix := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	requester := &model.User{Username: "req-" + suffix, PasswordHash: "x", Name: "需求方", Role: constants.RoleRequester}
	freelancer := &model.User{Username: "free-" + suffix, PasswordHash: "x", Name: "自由职业者", Role: constants.RoleFreelancer}
	other := &model.User{Username: "other-" + suffix, PasswordHash: "x", Name: "路人", Role: constants.RoleFreelancer}
	mustCreate(t, f.users, requester)
	mustCreate(t, f.users, freelancer)
	mustCreate(t, f.users, other)
	f.requester = requester
	f.freelancer = freelancer
	f.other = other

	logSvc := NewOperationLogService(f.logs, discardLogger())
	f.reqSvc = NewRequirementService(f.requirements, f.bids, logSvc, discardLogger())
	f.bidSvc = NewBidService(f.bids, f.requirements, logSvc, discardLogger())
	f.contractSvc = NewContractService(f.contracts, logSvc, discardLogger())
	f.deliverySvc = NewDeliveryService(f.deliveries, f.contracts, f.logs, discardLogger())

	requirement, err := f.reqSvc.Create(dto.CreateRequirementRequest{
		Title: "交付闭环测试需求", Description: "用于验证合同交付验收闭环的需求", MinBudget: 1000, MaxBudget: 9000, Skills: []string{"Go"},
	}, requester.ID, requester.Name, requester.Role)
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}
	bid, err := f.bidSvc.Create(dto.CreateBidRequest{
		RequirementID: requirement.ID, Amount: 5000, DurationDays: 15, Proposal: "我可以高质量完成这个交付任务。",
	}, freelancer.ID, freelancer.Name, freelancer.Role)
	if err != nil {
		t.Fatalf("create bid: %v", err)
	}
	contract, err := f.reqSvc.AcceptBid(requirement.ID, bid.ID, requester.ID, requester.Name, "installments", f.contractSvc)
	if err != nil {
		t.Fatalf("accept bid: %v", err)
	}
	if _, err := f.contractSvc.Sign(contract.ID, freelancer.ID, freelancer.Name); err != nil {
		t.Fatalf("sign contract: %v", err)
	}
	contract, err = f.contracts.FindByID(contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Status != constants.ContractInProgress {
		t.Fatalf("contract status = %s, want in_progress", contract.Status)
	}
	return f, contract
}

type gormFixture struct {
	db                           *gorm.DB
	users                        *repository.UserRepository
	requirements                 *repository.RequirementRepository
	bids                         *repository.BidRepository
	contracts                    *repository.ContractRepository
	deliveries                   *repository.ContractDeliveryRepository
	logs                         *repository.OperationLogRepository
	reqSvc                       *RequirementService
	bidSvc                       *BidService
	contractSvc                  *ContractService
	deliverySvc                  *DeliveryService
	requester, freelancer, other *model.User
}

func mustCreate(t *testing.T, repo interface {
	Create(*model.User) error
}, u *model.User) {
	t.Helper()
	if err := repo.Create(u); err != nil {
		t.Fatalf("create user: %v", err)
	}
}

func TestDeliverySubmitRejectResubmitAcceptLoop(t *testing.T) {
	f, contract := newDeliveryFixture(t)

	submitReq := dto.SubmitDeliveryRequest{
		Description: "功能已全部开发完成，附带测试报告与部署说明。",
		Attachments: []string{"/uploads/delivery-v1.zip", "/uploads/report.pdf"},
	}

	t.Run("party A cannot submit delivery", func(t *testing.T) {
		if _, err := f.deliverySvc.Submit(contract.ID, submitReq, f.requester.ID, f.requester.Name); err == nil {
			t.Fatal("party A submit succeeded, want forbidden")
		}
	})

	t.Run("outsider cannot read latest delivery", func(t *testing.T) {
		if _, err := f.deliverySvc.Latest(contract.ID, f.other.ID); err == nil {
			t.Fatal("outsider read latest succeeded, want forbidden")
		}
	})

	// First submission: contract -> pending_review.
	delivery, err := f.deliverySvc.Submit(contract.ID, submitReq, f.freelancer.ID, f.freelancer.Name)
	if err != nil {
		t.Fatalf("submit delivery: %v", err)
	}
	if delivery.Status != constants.DeliverySubmitted || delivery.Revision != 1 {
		t.Fatalf("delivery = status %s revision %d, want submitted/1", delivery.Status, delivery.Revision)
	}
	if len(delivery.Attachments) != 2 {
		t.Fatalf("attachments = %v, want 2", delivery.Attachments)
	}
	reloaded, err := f.contracts.FindByID(contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != constants.ContractPendingReview {
		t.Fatalf("contract status = %s, want pending_review", reloaded.Status)
	}

	// Persistence: the latest delivery can be read back.
	latest, err := f.deliverySvc.Latest(contract.ID, f.requester.ID)
	if err != nil {
		t.Fatalf("latest delivery: %v", err)
	}
	if latest == nil || latest.ID != delivery.ID {
		t.Fatalf("latest = %+v, want delivery %d", latest, delivery.ID)
	}

	t.Run("cannot resubmit while pending review", func(t *testing.T) {
		if _, err := f.deliverySvc.Submit(contract.ID, submitReq, f.freelancer.ID, f.freelancer.Name); err == nil {
			t.Fatal("duplicate submit succeeded, want conflict")
		}
	})

	t.Run("party B cannot review", func(t *testing.T) {
		_, err := f.deliverySvc.Review(contract.ID, dto.ReviewDeliveryRequest{Action: constants.DeliveryActionAccept}, f.freelancer.ID, f.freelancer.Name)
		if err == nil {
			t.Fatal("party B review succeeded, want forbidden")
		}
	})

	t.Run("reject requires reason", func(t *testing.T) {
		_, err := f.deliverySvc.Review(contract.ID, dto.ReviewDeliveryRequest{Action: constants.DeliveryActionReject}, f.requester.ID, f.requester.Name)
		if err == nil {
			t.Fatal("reject without reason succeeded, want bad request")
		}
		reloaded, _ := f.contracts.FindByID(contract.ID)
		if reloaded.Status != constants.ContractPendingReview {
			t.Fatalf("contract status = %s, still want pending_review after invalid reject", reloaded.Status)
		}
	})

	// Reject: contract -> in_progress, delivery marked rejected with reason.
	rejected, err := f.deliverySvc.Review(contract.ID, dto.ReviewDeliveryRequest{
		Action: constants.DeliveryActionReject, RejectReason: "缺少验收测试用例，请补充",
	}, f.requester.ID, f.requester.Name)
	if err != nil {
		t.Fatalf("reject delivery: %v", err)
	}
	if rejected.Status != constants.DeliveryRejected || rejected.RejectReason == "" || rejected.ReviewedByID != f.requester.ID {
		t.Fatalf("rejected delivery = %+v", rejected)
	}
	reloaded, _ = f.contracts.FindByID(contract.ID)
	if reloaded.Status != constants.ContractInProgress {
		t.Fatalf("contract status = %s, want back to in_progress", reloaded.Status)
	}

	t.Run("cannot accept a contract back in progress", func(t *testing.T) {
		_, err := f.deliverySvc.Review(contract.ID, dto.ReviewDeliveryRequest{Action: constants.DeliveryActionAccept}, f.requester.ID, f.requester.Name)
		if err == nil {
			t.Fatal("accept without pending delivery succeeded, want conflict")
		}
	})

	// Resubmit after rejection: revision 2.
	second, err := f.deliverySvc.Submit(contract.ID, dto.SubmitDeliveryRequest{
		Description: "已补充完整的验收测试用例和回归报告，请再次验收。",
		Attachments: []string{},
	}, f.freelancer.ID, f.freelancer.Name)
	if err != nil {
		t.Fatalf("resubmit delivery: %v", err)
	}
	if second.Revision != 2 || second.Status != constants.DeliverySubmitted {
		t.Fatalf("second delivery = revision %d status %s, want 2/submitted", second.Revision, second.Status)
	}
	latest, _ = f.deliverySvc.Latest(contract.ID, f.freelancer.ID)
	if latest == nil || latest.Revision != 2 {
		t.Fatalf("latest revision = %+v, want 2", latest)
	}

	// Accept: contract -> completed, delivery accepted, stages all done.
	accepted, err := f.deliverySvc.Review(contract.ID, dto.ReviewDeliveryRequest{Action: constants.DeliveryActionAccept}, f.requester.ID, f.requester.Name)
	if err != nil {
		t.Fatalf("accept delivery: %v", err)
	}
	if accepted.Status != constants.DeliveryAccepted || accepted.ReviewedAt == nil {
		t.Fatalf("accepted delivery = %+v", accepted)
	}
	reloaded, _ = f.contracts.FindByID(contract.ID)
	if reloaded.Status != constants.ContractCompleted {
		t.Fatalf("contract status = %s, want completed", reloaded.Status)
	}
	for i, stage := range reloaded.Stages {
		if stage.Status != "done" {
			t.Fatalf("stage %d status = %s, want done", i, stage.Status)
		}
	}

	t.Run("cannot process again after completion", func(t *testing.T) {
		if _, err := f.deliverySvc.Review(contract.ID, dto.ReviewDeliveryRequest{Action: constants.DeliveryActionAccept}, f.requester.ID, f.requester.Name); err == nil {
			t.Fatal("second accept succeeded, want conflict")
		}
		if _, err := f.deliverySvc.Submit(contract.ID, submitReq, f.freelancer.ID, f.freelancer.Name); err == nil {
			t.Fatal("submit on completed contract succeeded, want conflict")
		}
	})

	// History preserved: two deliveries exist, latest is the accepted one.
	var count int64
	if err := f.db.Model(&model.ContractDelivery{}).Where("contract_id = ?", contract.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("delivery count = %d, want 2 (reject history preserved)", count)
	}
}

func TestDeliveryConcurrentSubmitSucceedsOnce(t *testing.T) {
	f, contract := newDeliveryFixture(t)
	// A single pooled connection serializes transactions; the CAS guard must
	// still ensure exactly one submission wins.
	sqlDB, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)

	const n = 16
	var wg sync.WaitGroup
	var ok, fail int64
	var mu sync.Mutex
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, err := f.deliverySvc.Submit(contract.ID, dto.SubmitDeliveryRequest{
				Description: "并发提交的交付说明，内容足够长以通过校验。",
			}, f.freelancer.ID, f.freelancer.Name)
			mu.Lock()
			if err == nil {
				ok++
			} else {
				fail++
			}
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if ok != 1 || fail != n-1 {
		t.Fatalf("submit results = %d ok, %d fail, want exactly 1 ok", ok, fail)
	}
	reloaded, err := f.contracts.FindByID(contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != constants.ContractPendingReview {
		t.Fatalf("contract status = %s, want pending_review", reloaded.Status)
	}
	var count int64
	if err := f.db.Model(&model.ContractDelivery{}).Where("contract_id = ?", contract.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("delivery rows = %d, want 1 (no orphan deliveries)", count)
	}
}

func TestDeliveryConcurrentRejectSucceedsOnce(t *testing.T) {
	f, contract := newDeliveryFixture(t)
	if _, err := f.deliverySvc.Submit(contract.ID, dto.SubmitDeliveryRequest{
		Description: "并发驳回测试用的交付说明。",
	}, f.freelancer.ID, f.freelancer.Name); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)

	const n = 16
	var wg sync.WaitGroup
	var ok int64
	var mu sync.Mutex
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, err := f.deliverySvc.Review(contract.ID, dto.ReviewDeliveryRequest{
				Action: constants.DeliveryActionReject, RejectReason: "并发驳回原因",
			}, f.requester.ID, f.requester.Name)
			mu.Lock()
			if err == nil {
				ok++
			}
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if ok != 1 {
		t.Fatalf("reject results = %d ok, want exactly 1", ok)
	}
	reloaded, _ := f.contracts.FindByID(contract.ID)
	if reloaded.Status != constants.ContractInProgress {
		t.Fatalf("contract status = %s, want in_progress", reloaded.Status)
	}
	latest, err := f.deliverySvc.Latest(contract.ID, f.requester.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != constants.DeliveryRejected || latest.RejectReason != "并发驳回原因" {
		t.Fatalf("latest delivery = %+v, want rejected with reason", latest)
	}
}

func TestDeliveryConcurrentAcceptSucceedsOnce(t *testing.T) {
	f, contract := newDeliveryFixture(t)
	if _, err := f.deliverySvc.Submit(contract.ID, dto.SubmitDeliveryRequest{
		Description: "并发接受测试用的交付说明。",
	}, f.freelancer.ID, f.freelancer.Name); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)

	const n = 16
	var wg sync.WaitGroup
	var ok int64
	var mu sync.Mutex
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, err := f.deliverySvc.Review(contract.ID, dto.ReviewDeliveryRequest{
				Action: constants.DeliveryActionAccept,
			}, f.requester.ID, f.requester.Name)
			mu.Lock()
			if err == nil {
				ok++
			}
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if ok != 1 {
		t.Fatalf("accept results = %d ok, want exactly 1", ok)
	}
	reloaded, _ := f.contracts.FindByID(contract.ID)
	if reloaded.Status != constants.ContractCompleted {
		t.Fatalf("contract status = %s, want completed", reloaded.Status)
	}
	var accepted int64
	if err := f.db.Model(&model.ContractDelivery{}).
		Where("contract_id = ? AND status = ?", contract.ID, constants.DeliveryAccepted).
		Count(&accepted).Error; err != nil {
		t.Fatal(err)
	}
	if accepted != 1 {
		t.Fatalf("accepted deliveries = %d, want exactly 1", accepted)
	}
}

func TestDeliveryConcurrentMixedReviewSucceedsOnce(t *testing.T) {
	f, contract := newDeliveryFixture(t)
	if _, err := f.deliverySvc.Submit(contract.ID, dto.SubmitDeliveryRequest{
		Description: "驳回与接受混合并发测试用的交付说明。",
	}, f.freelancer.ID, f.freelancer.Name); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)

	const n = 32
	var wg sync.WaitGroup
	var ok, rejects, accepts int64
	var mu sync.Mutex
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		action := constants.DeliveryActionReject
		if i%2 == 0 {
			action = constants.DeliveryActionAccept
		}
		go func(action string) {
			defer wg.Done()
			<-start
			_, err := f.deliverySvc.Review(contract.ID, dto.ReviewDeliveryRequest{
				Action: action, RejectReason: "混合并发驳回原因",
			}, f.requester.ID, f.requester.Name)
			mu.Lock()
			if err == nil {
				ok++
				if action == constants.DeliveryActionReject {
					rejects++
				} else {
					accepts++
				}
			}
			mu.Unlock()
		}(action)
	}
	close(start)
	wg.Wait()

	if ok != 1 || rejects+accepts != 1 {
		t.Fatalf("mixed review = %d ok (reject %d, accept %d), want exactly 1 winner", ok, rejects, accepts)
	}

	reloaded, _ := f.contracts.FindByID(contract.ID)
	latest, err := f.deliverySvc.Latest(contract.ID, f.requester.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rejects == 1 {
		if reloaded.Status != constants.ContractInProgress {
			t.Fatalf("contract status = %s, want in_progress when reject won", reloaded.Status)
		}
		if latest.Status != constants.DeliveryRejected {
			t.Fatalf("delivery status = %s, want rejected", latest.Status)
		}
	} else {
		if reloaded.Status != constants.ContractCompleted {
			t.Fatalf("contract status = %s, want completed when accept won", reloaded.Status)
		}
		if latest.Status != constants.DeliveryAccepted {
			t.Fatalf("delivery status = %s, want accepted", latest.Status)
		}
	}
}
