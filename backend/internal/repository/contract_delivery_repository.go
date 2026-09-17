package repository

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
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

// DB exposes the underlying handle so the service can open transactions
// spanning the contract and delivery stores.
func (r *ContractDeliveryRepository) DB() *gorm.DB {
	return r.db
}

// InTx runs fn inside a single database transaction. The transaction is
// committed when fn returns nil and rolled back otherwise, so a delivery row
// can never be left without the matching contract status change.
func (r *ContractDeliveryRepository) InTx(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

// WithTx returns a repository bound to an existing transaction handle.
func (r *ContractDeliveryRepository) WithTx(tx *gorm.DB) *ContractDeliveryRepository {
	return &ContractDeliveryRepository{db: tx}
}

// Create inserts a delivery row.
func (r *ContractDeliveryRepository) Create(d *model.ContractDelivery) error {
	if err := r.db.Create(d).Error; err != nil {
		return fmt.Errorf("create delivery: %w", err)
	}
	return nil
}

// FindLatestByContract returns the most recent delivery of a contract.
func (r *ContractDeliveryRepository) FindLatestByContract(contractID uint) (*model.ContractDelivery, error) {
	var d model.ContractDelivery
	err := r.db.Preload("Submitter").
		Preload("Reviewer").
		Where("contract_id = ?", contractID).
		Order("revision DESC").
		First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find latest delivery: %w", err)
	}
	return &d, nil
}

// LatestRevision returns the highest revision number used for a contract,
// or 0 when the contract has no delivery yet.
func (r *ContractDeliveryRepository) LatestRevision(contractID uint) (int, error) {
	var revision int
	err := r.db.Model(&model.ContractDelivery{}).
		Where("contract_id = ?", contractID).
		Select("COALESCE(MAX(revision), 0)").
		Scan(&revision).Error
	if err != nil {
		return 0, fmt.Errorf("latest delivery revision: %w", err)
	}
	return revision, nil
}

// FindByID loads a delivery by primary key.
func (r *ContractDeliveryRepository) FindByID(id uint) (*model.ContractDelivery, error) {
	var d model.ContractDelivery
	err := r.db.Preload("Submitter").
		Preload("Reviewer").
		First(&d, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find delivery by id: %w", err)
	}
	return &d, nil
}

// Save persists delivery changes.
func (r *ContractDeliveryRepository) Save(d *model.ContractDelivery) error {
	if err := r.db.Save(d).Error; err != nil {
		return fmt.Errorf("save delivery: %w", err)
	}
	return nil
}

// ApplyReview conditionally marks a still-pending delivery as reviewed. The
// conditional status guard guarantees that two concurrent reviews of the same
// delivery can never both succeed.
func (r *ContractDeliveryRepository) ApplyReview(tx *gorm.DB, id uint, status, reason string, reviewerID uint, reviewedAt time.Time) error {
	updates := map[string]any{
		"status":         status,
		"reviewed_by_id": reviewerID,
		"reviewed_at":    reviewedAt,
	}
	if status == constants.DeliveryRejected {
		updates["reject_reason"] = reason
	}
	result := tx.Model(&model.ContractDelivery{}).
		Where("id = ? AND status = ?", id, constants.DeliverySubmitted).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("apply delivery review: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrConflict
	}
	return nil
}
