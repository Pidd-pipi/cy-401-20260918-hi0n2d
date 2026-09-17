package repository

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/model"
)

// ContractDeliveryRepository persists contract deliveries.
type ContractDeliveryRepository struct {
	db *gorm.DB
}

// NewContractDeliveryRepository builds a ContractDeliveryRepository.
func NewContractDeliveryRepository(db *gorm.DB) *ContractDeliveryRepository {
	return &ContractDeliveryRepository{db: db}
}

// Create inserts a delivery row.
func (r *ContractDeliveryRepository) Create(d *model.ContractDelivery) error {
	if err := r.db.Create(d).Error; err != nil {
		return fmt.Errorf("create delivery: %w", err)
	}
	return nil
}

// FindByID loads a delivery by primary key.
func (r *ContractDeliveryRepository) FindByID(id uint) (*model.ContractDelivery, error) {
	var d model.ContractDelivery
	if err := r.db.First(&d, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find delivery by id: %w", err)
	}
	return &d, nil
}

// FindLatestByContractID loads the most recent delivery of a contract, or
// ErrNotFound when the contract has no delivery yet.
func (r *ContractDeliveryRepository) FindLatestByContractID(contractID uint) (*model.ContractDelivery, error) {
	var d model.ContractDelivery
	err := r.db.Where("contract_id = ?", contractID).
		Order("created_at DESC, id DESC").
		First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find latest delivery: %w", err)
	}
	return &d, nil
}

// Submit atomically transitions a contract from wantStatus to nextStatus and
// inserts the delivery row inside one transaction. The conditional contract
// update is the concurrency guard: only one concurrent submit can win, and the
// delivery row is rolled back together with it if the guard fails, so no
// orphan delivery is ever left behind.
func (r *ContractDeliveryRepository) Submit(contract *model.Contract, wantStatus, nextStatus string, delivery *model.ContractDelivery) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Contract{}).
			Where("id = ? AND status = ?", contract.ID, wantStatus).
			Update("status", nextStatus)
		if res.Error != nil {
			return fmt.Errorf("submit delivery: flip contract status: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrConflict
		}
		if err := tx.Create(delivery).Error; err != nil {
			return fmt.Errorf("submit delivery: create delivery: %w", err)
		}
		return nil
	})
}

// Review atomically transitions a pending delivery together with its
// contract. The conditional delivery update (id + submitted status) is the
// concurrency guard: concurrent reject/accept against the same delivery cannot
// both succeed, and the contract status change is rolled back together with
// the delivery if the guard fails. contract must already carry the target
// status (and, on acceptance, the finalized stages); its BeforeSave hook
// serializes the stages.
func (r *ContractDeliveryRepository) Review(deliveryID uint, fields map[string]any, contract *model.Contract, wantContractStatus, nextContractStatus string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.ContractDelivery{}).
			Where("id = ? AND status = ?", deliveryID, "submitted").
			Updates(fields)
		if res.Error != nil {
			return fmt.Errorf("review delivery: update delivery: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrConflict
		}
		res = tx.Model(&model.Contract{}).
			Where("id = ? AND status = ?", contract.ID, wantContractStatus).
			Update("status", nextContractStatus)
		if res.Error != nil {
			return fmt.Errorf("review delivery: flip contract status: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrConflict
		}
		if err := tx.Save(contract).Error; err != nil {
			return fmt.Errorf("review delivery: persist contract: %w", err)
		}
		return nil
	})
}
