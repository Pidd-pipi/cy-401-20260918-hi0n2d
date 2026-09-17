package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ContractDelivery is one delivery submitted by party B for a contract.
// A contract may have many deliveries over time; the latest one drives the
// acceptance loop together with the contract status.
type ContractDelivery struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	ContractID    uint       `gorm:"uniqueIndex:uniq_contract_revision;not null" json:"contractId"`
	Revision      int        `gorm:"uniqueIndex:uniq_contract_revision;not null" json:"revision"`
	Description   string     `gorm:"type:text;not null" json:"description"`
	AttachmentsJS string     `gorm:"column:attachments;type:text" json:"-"`
	Status        string     `gorm:"size:24;not null;default:submitted" json:"status"`
	RejectReason  string     `gorm:"type:text" json:"rejectReason"`
	SubmittedByID uint       `gorm:"index;not null" json:"submittedById"`
	ReviewedByID  uint       `gorm:"index" json:"reviewedById"`
	SubmittedAt   time.Time  `json:"submittedAt"`
	ReviewedAt    *time.Time `json:"reviewedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"-"`

	// Computed fields.
	Attachments []string  `gorm:"-" json:"attachments"`
	Contract    *Contract `gorm:"foreignKey:ContractID" json:"contract,omitempty"`
	Submitter   *User     `gorm:"foreignKey:SubmittedByID" json:"submitter,omitempty"`
	Reviewer    *User     `gorm:"foreignKey:ReviewedByID" json:"reviewer,omitempty"`
}

// BeforeSave serializes attachments.
func (d *ContractDelivery) BeforeSave(_ *gorm.DB) error {
	if d.Attachments != nil {
		raw, err := json.Marshal(d.Attachments)
		if err != nil {
			return err
		}
		d.AttachmentsJS = string(raw)
	}
	return nil
}

// AfterFind restores attachments.
func (d *ContractDelivery) AfterFind(_ *gorm.DB) error {
	d.Attachments = []string{}
	if d.AttachmentsJS != "" {
		_ = json.Unmarshal([]byte(d.AttachmentsJS), &d.Attachments)
	}
	return nil
}
