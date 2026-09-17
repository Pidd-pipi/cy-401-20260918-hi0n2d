package constants

// Contract delivery (acceptance) statuses.
const (
	DeliverySubmitted = "submitted" // 待验收
	DeliveryRejected  = "rejected"  // 甲方驳回，合同回到执行中
	DeliveryAccepted  = "accepted"  // 甲方接受，合同完成
)

// DeliveryActions supported by the acceptance loop.
const (
	DeliveryActionReject = "reject"
	DeliveryActionAccept = "accept"
)

// ValidDeliveryAction reports whether an acceptance action is valid.
func ValidDeliveryAction(action string) bool {
	switch action {
	case DeliveryActionReject, DeliveryActionAccept:
		return true
	}
	return false
}
