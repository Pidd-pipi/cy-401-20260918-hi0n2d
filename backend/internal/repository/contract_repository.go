package repository

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/model"
)

// ContractRepository persists contracts.
type ContractRepository struct {
	db *gorm.DB
}

// NewContractRepository builds a ContractRepository.
func NewContractRepository(db *gorm.DB) *ContractRepository {
	return &ContractRepository{db: db}
}

// WithTx returns a repository bound to an existing transaction handle.
func (r *ContractRepository) WithTx(tx *gorm.DB) *ContractRepository {
	return &ContractRepository{db: tx}
}

// FindByIDForUpdate loads a contract inside a transaction. The contract is
// re-read through the transaction handle so concurrent transitions serialize.
func (r *ContractRepository) FindByIDForUpdate(tx *gorm.DB, id uint) (*model.Contract, error) {
	return r.WithTx(tx).FindByID(id)
}

// TransitionStatus atomically changes a contract from one of the expected
// statuses to target. It returns ErrConflict when no row matches, which means
// another request won the race or the state changed in the meantime.
func (r *ContractRepository) TransitionStatus(tx *gorm.DB, id uint, fromStatuses []string, toStatus string) error {
	result := tx.Model(&model.Contract{}).
		Where("id = ? AND status IN ?", id, fromStatuses).
		Update("status", toStatus)
	if result.Error != nil {
		return fmt.Errorf("transition contract status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrConflict
	}
	return nil
}

// MarkStagesDone rewrites only the stages column for a contract. It must run
// inside the review transaction after the contract status CAS has been won, so
// it never overwrites another transaction's status change.
func (r *ContractRepository) MarkStagesDone(tx *gorm.DB, id uint, stages []model.ContractStage) error {
	raw, err := json.Marshal(stages)
	if err != nil {
		return fmt.Errorf("marshal stages: %w", err)
	}
	result := tx.Model(&model.Contract{}).
		Where("id = ?", id).
		Update("stages", string(raw))
	if result.Error != nil {
		return fmt.Errorf("mark stages done: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Create inserts a contract.
func (r *ContractRepository) Create(c *model.Contract) error {
	if err := r.db.Create(c).Error; err != nil {
		return fmt.Errorf("create contract: %w", err)
	}
	return nil
}

// ListByParty returns contracts where the user is either party.
func (r *ContractRepository) ListByParty(userID uint) ([]model.Contract, error) {
	var contracts []model.Contract
	if err := r.db.Where("party_a_id = ? OR party_b_id = ?", userID, userID).
		Preload("PartyA").
		Preload("PartyB").
		Preload("Requirement").
		Order("created_at DESC").
		Find(&contracts).Error; err != nil {
		return nil, fmt.Errorf("list contracts: %w", err)
	}
	return contracts, nil
}

// FindByID loads a contract by primary key.
func (r *ContractRepository) FindByID(id uint) (*model.Contract, error) {
	var c model.Contract
	err := r.db.Preload("PartyA").
		Preload("PartyB").
		Preload("Requirement").
		First(&c, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find contract by id: %w", err)
	}
	return &c, nil
}

// Update persists contract changes.
func (r *ContractRepository) Update(c *model.Contract) error {
	if err := r.db.Save(c).Error; err != nil {
		return fmt.Errorf("update contract: %w", err)
	}
	return nil
}

// CountByParty returns the number of contracts involving a user.
func (r *ContractRepository) CountByParty(userID uint) (int64, error) {
	var count int64
	if err := r.db.Model(&model.Contract{}).
		Where("party_a_id = ? OR party_b_id = ?", userID, userID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count contracts: %w", err)
	}
	return count, nil
}
