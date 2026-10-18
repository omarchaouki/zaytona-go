package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
)

// NewReview is a validated public review.
type NewReview struct {
	Review    domain.ValidReview
	PhoneHash []byte // keyed hash of the phone, nil without one
	PhotoID   *string
	IPHash    []byte
}

// SubmitReview stores a pending review. With a phone, it is linked to the
// latest delivered order of that phone ("verified purchase"); the phone itself
// is never stored.
func (s *Store) SubmitReview(ctx context.Context, n NewReview) (dbq.Review, error) {
	v := n.Review
	var order uuid.NullUUID
	if v.PhoneE164 != "" {
		id, err := s.Q.LatestDeliveredOrderForPhone(ctx, v.PhoneE164)
		switch {
		case err == nil:
			order = uuid.NullUUID{UUID: id, Valid: true}
		case !errors.Is(err, pgx.ErrNoRows):
			return dbq.Review{}, err
		}
	}
	var city *string
	if v.City != "" {
		city = &v.City
	}
	return s.Q.CreateReview(ctx, dbq.CreateReviewParams{
		AuthorName: v.Name, City: city, Rating: int16(v.Rating), Comment: v.Comment, //nolint:gosec // G115: rating is 1..5
		Language: dbq.Locale(v.Language), PhoneHash: n.PhoneHash, OrderID: order, PhotoID: n.PhotoID,
		IpHash: n.IPHash, Status: dbq.ReviewStatusPending,
	})
}

// ModerateReviews publishes or refuses reviews (reason kept internal).
// Returns how many changed; one audit row per review.
func (s *Store) ModerateReviews(ctx context.Context, a Actor, ids []uuid.UUID, approve bool, reason string) (int, error) {
	status, action := dbq.ReviewStatusRejected, "review.reject"
	var r *string
	if approve {
		status, action = dbq.ReviewStatusApproved, "review.approve"
	} else if reason != "" {
		r = &reason
	}
	changed := 0
	err := s.InTx(ctx, func(q *dbq.Queries) error {
		done, err := q.ModerateReviews(ctx, dbq.ModerateReviewsParams{Status: status, Reason: r, AdminID: a.AdminID, Ids: ids})
		if err != nil {
			return err
		}
		for _, id := range done {
			if err := Audit(ctx, q, a, action, "review", id.String(), map[string]any{"reason": reason != "" && !approve}); err != nil {
				return err
			}
		}
		changed = len(done)
		return nil
	})
	return changed, err
}

// SetReviewFeatured pins a review at the top of the list (or unpins it).
func (s *Store) SetReviewFeatured(ctx context.Context, a Actor, id uuid.UUID, featured bool) error {
	return s.InTx(ctx, func(q *dbq.Queries) error {
		if _, err := q.GetReview(ctx, id); err != nil {
			return NotFound(err)
		}
		if err := q.SetReviewFeatured(ctx, dbq.SetReviewFeaturedParams{ID: id, IsFeatured: featured}); err != nil {
			return err
		}
		action := "review.unpin"
		if featured {
			action = "review.pin"
		}
		return Audit(ctx, q, a, action, "review", id.String(), nil)
	})
}

// EditReview fixes the text (the customer's original is kept once) and sets
// the replies ("" removes one).
func (s *Store) EditReview(ctx context.Context, a Actor, id uuid.UUID, comment, replyAr, replyFr string) error {
	return s.InTx(ctx, func(q *dbq.Queries) error {
		cur, err := q.GetReview(ctx, id)
		if err != nil {
			return NotFound(err)
		}
		changed := comment != "" && comment != cur.Comment
		if changed {
			if err := q.EditReviewComment(ctx, dbq.EditReviewCommentParams{ID: id, Comment: comment}); err != nil {
				return err
			}
		}
		opt := func(s string) *string {
			if s == "" {
				return nil
			}
			return &s
		}
		if err := q.SetReviewReplies(ctx, dbq.SetReviewRepliesParams{ID: id, ReplyAr: opt(replyAr), ReplyFr: opt(replyFr)}); err != nil {
			return err
		}
		return Audit(ctx, q, a, "review.edit", "review", id.String(), map[string]any{"comment_changed": changed, "reply_ar": replyAr != "", "reply_fr": replyFr != ""})
	})
}

// DeleteReviews removes reviews for good (owner only) and returns their photo ids.
func (s *Store) DeleteReviews(ctx context.Context, a Actor, ids []uuid.UUID) ([]string, error) {
	if !a.owner() {
		return nil, ErrForbidden
	}
	var photos []string
	err := s.InTx(ctx, func(q *dbq.Queries) error {
		for _, id := range ids {
			photo, err := q.DeleteReview(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if photo != nil {
				photos = append(photos, *photo)
			}
			if err := Audit(ctx, q, a, "review.delete", "review", id.String(), nil); err != nil {
				return err
			}
		}
		return nil
	})
	return photos, err
}
