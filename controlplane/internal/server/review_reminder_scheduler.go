// Package server provides periodic review reminder scheduling.
package server

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ReviewReminderScheduler sends reminders for upcoming compliance reviews.
type ReviewReminderScheduler struct {
	cron   *cron.Cron
	server *Server
	logger *zap.Logger
}

// NewReviewReminderScheduler creates a scheduler bound to the given server.
func NewReviewReminderScheduler(s *Server) *ReviewReminderScheduler {
	return &ReviewReminderScheduler{
		cron:   cron.New(),
		server: s,
		logger: s.logger.Named("review-reminder-scheduler"),
	}
}

// Start registers the cron schedule and begins ticking. Default runs daily at 9 AM UTC.
func (rrs *ReviewReminderScheduler) Start(schedule string) error {
	if schedule == "" {
		schedule = "0 9 * * *" // Daily at 9 AM UTC
	}
	_, err := rrs.cron.AddFunc(schedule, func() {
		rrs.runOnce()
	})
	if err != nil {
		return fmt.Errorf("register review reminder schedule %q: %w", schedule, err)
	}
	rrs.cron.Start()
	rrs.logger.Info("review reminder scheduler started", zap.String("schedule", schedule))
	// Run once at startup
	go rrs.runOnce()
	return nil
}

// Stop halts the cron scheduler.
func (rrs *ReviewReminderScheduler) Stop() {
	ctx := rrs.cron.Stop()
	<-ctx.Done()
	rrs.logger.Info("review reminder scheduler stopped")
}

// runOnce checks for upcoming reviews and sends reminders.
func (rrs *ReviewReminderScheduler) runOnce() {
	ctx := context.Background()
	rrs.logger.Info("checking for upcoming compliance reviews")

	// Get all tenants
	tenants, _, err := rrs.server.store.ListTenants(ctx, "", 1000, 0)
	if err != nil {
		rrs.logger.Error("list tenants for review reminders", zap.Error(err))
		return
	}

	now := time.Now().UTC()
	reminderWindow := 7 * 24 * time.Hour // Remind 7 days before

	for _, tenant := range tenants {
		reviews, _, err := rrs.server.store.ListComplianceReviews(ctx, tenant.ID, 100, 0)
		if err != nil {
			rrs.logger.Error("list compliance reviews for tenant",
				zap.String("tenant", tenant.Name),
				zap.Error(err))
			continue
		}

		for _, review := range reviews {
			// Skip completed reviews
			if review.Status == "completed" {
				continue
			}

			// Check if review is scheduled within the reminder window
			if review.ScheduledFor != nil {
				timeUntil := review.ScheduledFor.Sub(now)
				if timeUntil > 0 && timeUntil <= reminderWindow {
					rrs.sendReminder(ctx, &review, &tenant)
				}
			}
		}
	}
}

// sendReminder sends a reminder notification for a review.
func (rrs *ReviewReminderScheduler) sendReminder(ctx context.Context, review *storage.ComplianceReview, tenant *storage.Tenant) {
	rrs.logger.Info("sending review reminder",
		zap.String("tenant", tenant.Name),
		zap.String("review_type", review.ReviewType),
		zap.Time("scheduled_for", *review.ScheduledFor),
	)

	// TODO: Integrate with notification system (email, Slack, etc.)
	// For now, we log the reminder and could store it in an audit log
	rrs.server.recordAudit(ctx, rrs.server.systemActor(), tenant.ID, "compliance.review.reminder", "compliance_review", review.ID.String(), map[string]any{
		"review_type":   review.ReviewType,
		"scheduled_for": review.ScheduledFor,
	})
}
