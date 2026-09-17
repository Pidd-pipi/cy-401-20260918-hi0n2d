package dto

// SubmitDeliveryRequest is the payload for party B submitting a delivery.
type SubmitDeliveryRequest struct {
	Description string   `json:"description" validate:"required,min=5"`
	Attachments []string `json:"attachments"`
}

// RejectDeliveryRequest is the payload for party A rejecting a delivery.
// Reason is mandatory: a rejection without explanation is rejected by the
// validator before any state change happens.
type RejectDeliveryRequest struct {
	Reason string `json:"reason" validate:"required,min=2"`
}
