package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/finverse/payment-service/internal/domain"
)

// MarkProcessing moves an accepted async-rail payment INITIATED -> PROCESSING.
func (r *PaymentRepository) MarkProcessing(ctx context.Context, paymentID string, settleAt time.Time) error {
	res := r.db.WithContext(ctx).Model(&paymentModel{}).
		Where("id = ? AND status = ?", paymentID, string(domain.StatusInitiated)).
		Updates(map[string]any{"status": string(domain.StatusProcessing), "settle_at": settleAt})
	if res.Error != nil {
		return fmt.Errorf("marking payment processing: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("payment %s is not INITIATED, cannot move to PROCESSING", paymentID)
	}
	paymentsTotal.WithLabelValues("PROCESSING").Inc()
	return nil
}

// ClaimDueForSettlement leases due PROCESSING payments. FOR UPDATE SKIP LOCKED
// inside the UPDATE's subselect means two replicas polling at once get
// disjoint sets; the lease (claimed_until) keeps a payment away from other
// pollers while this one works on it, and lets it be retried if this one dies.
func (r *PaymentRepository) ClaimDueForSettlement(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*domain.Payment, error) {
	var ms []paymentModel
	err := r.db.WithContext(ctx).Raw(`
		UPDATE payments
		   SET claimed_until = ?, settle_attempts = settle_attempts + 1
		 WHERE id IN (
		       SELECT id FROM payments
		        WHERE status = 'PROCESSING' AND settle_at <= ?
		          AND (claimed_until IS NULL OR claimed_until <= ?)
		        ORDER BY settle_at
		          FOR UPDATE SKIP LOCKED
		        LIMIT ?)
		RETURNING *`, now.Add(lease), now, now, limit).Scan(&ms).Error
	if err != nil {
		return nil, fmt.Errorf("claiming due payments: %w", err)
	}
	out := make([]*domain.Payment, 0, len(ms))
	for i := range ms {
		out = append(out, fromPaymentModel(&ms[i]))
	}
	return out, nil
}

func (r *PaymentRepository) ReleaseForRetry(ctx context.Context, paymentID string, retryAt time.Time) error {
	return r.db.WithContext(ctx).Model(&paymentModel{}).Where("id = ?", paymentID).
		Update("claimed_until", retryAt).Error
}
