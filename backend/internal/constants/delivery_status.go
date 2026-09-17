package constants

// Contract delivery statuses.
const (
	DeliverySubmitted = "submitted" // 乙方已提交，等待甲方验收
	DeliveryRejected  = "rejected"  // 甲方驳回，合同回到执行中
	DeliveryAccepted  = "accepted"  // 甲方接受，合同完成
)

// ValidDeliveryStatus reports whether a delivery status is valid.
func ValidDeliveryStatus(s string) bool {
	switch s {
	case DeliverySubmitted, DeliveryRejected, DeliveryAccepted:
		return true
	}
	return false
}
