package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ContractDelivery is one delivery submitted by party B for a contract.
// The contract status and the delivery row are written together in a single
// transaction, so a delivery can never exist without its matching status flow.
type ContractDelivery struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	ContractID    uint       `gorm:"index;not null" json:"contractId"`
	SubmitterID   uint       `gorm:"index;not null" json:"submitterId"`
	Description   string     `gorm:"type:text;not null" json:"description"`
	AttachmentsJS string     `gorm:"column:attachments;type:text" json:"-"`
	Status        string     `gorm:"size:24;not null;default:submitted" json:"status"`
	RejectReason  string     `gorm:"type:text" json:"rejectReason"`
	ReviewerID    uint       `gorm:"index" json:"reviewerId"`
	ReviewedAt    *time.Time `json:"reviewedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"-"`

	// Computed fields.
	Attachments []string `gorm:"-" json:"attachments"`
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
