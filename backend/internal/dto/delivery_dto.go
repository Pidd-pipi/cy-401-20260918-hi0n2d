package dto

// SubmitDeliveryRequest is the payload for party B submitting a delivery.
type SubmitDeliveryRequest struct {
	Description string   `json:"description" validate:"required,min=5,max=2000"`
	Attachments []string `json:"attachments" validate:"dive,max=500"`
}

// ReviewDeliveryRequest is the payload for party A reviewing a delivery.
// RejectReason is required when the action is "reject".
type ReviewDeliveryRequest struct {
	Action       string `json:"action" validate:"required,oneof=reject accept"`
	RejectReason string `json:"rejectReason" validate:"max=500"`
}
