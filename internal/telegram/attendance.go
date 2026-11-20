package telegram

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/url"
	"time"

	"github.com/megazvezda/laziness/models"
	"github.com/megazvezda/laziness/storage"
)

const (
	attendanceWorkers = 3
	attendanceTimeout = 20 * time.Second
	// spread the requests out a bit so the accounts don't all hit Moodle at once
	attendanceJitter = 3 * time.Second
)

func (b *Bot) markGroupAttendance(ctx context.Context, chatID int64, link string) {
	dbCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	students, err := storage.ListSessions(dbCtx, b.db)
	cancel()
	if err != nil {
		b.logger.ErrorContext(ctx, "Could not list Moodle accounts", "error_kind", errorKind(err))
		b.reply(ctx, chatID, "Could not load Moodle accounts for attendance.")
		return
	}
	if len(students) == 0 {
		b.reply(ctx, chatID, "No Moodle accounts are registered.")
		return
	}

	type attendanceResult struct {
		FullName string
		err      error
	}

	jobs := make(chan models.DBRecord)
	results := make(chan attendanceResult, len(students))

	for range min(attendanceWorkers, len(students)) {
		go func() {
			for student := range jobs {
				if err := waitJitter(ctx, attendanceJitter); err != nil {
					results <- attendanceResult{FullName: student.FullName, err: err}
					continue
				}
				b.logger.Info("Worker received a student", "moodle_user_id", student.MoodleUserID)
				attemptCtx, cancel := context.WithTimeout(ctx, attendanceTimeout)
				err := b.attendance.MarkAttendanceForStudent(attemptCtx, student, link)
				cancel()
				results <- attendanceResult{FullName: student.FullName, err: err}
			}
		}()
	}
	for _, student := range students {
		jobs <- student
	}
	close(jobs)

	for range len(students) {
		result := <-results
		if result.err == nil {
			b.reply(ctx, chatID, "Marked attendance as "+result.FullName)
			continue
		}
		reason := result.err.Error()
		var requestErr *url.Error
		if errors.As(result.err, &requestErr) {
			reason = "could not reach Moodle"
		}
		b.reply(ctx, chatID, "Could not mark attendance for "+result.FullName+": "+reason)
	}
}

func waitJitter(ctx context.Context, max time.Duration) error {
	if max <= 0 {
		return nil
	}
	timer := time.NewTimer(rand.N(max))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
