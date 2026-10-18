package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
)

// Admin management errors.
var (
	ErrLastOwner   = errors.New("store: at least one active owner must remain")
	ErrSelf        = errors.New("store: an admin cannot disable or demote themselves")
	ErrEmailExists = errors.New("store: email already has an account")
)

// adminsLock serializes owner changes (last-owner check) across requests.
const adminsLock = "select pg_advisory_xact_lock(7340001)"

func (s *Store) adminTx(ctx context.Context, a Actor, fn func(q *dbq.Queries, tx pgx.Tx) error) error {
	if !a.owner() {
		return ErrForbidden
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, adminsLock); err != nil {
		return err
	}
	if err := fn(s.Q.WithTx(tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AddAdmin creates an account that must change its password at first login (owner only).
func (s *Store) AddAdmin(ctx context.Context, a Actor, p dbq.CreateAdminParams) (dbq.Admin, error) {
	var out dbq.Admin
	p.MustChangePassword = true
	err := s.adminTx(ctx, a, func(q *dbq.Queries, _ pgx.Tx) error {
		if _, err := q.GetAdminByEmail(ctx, p.Email); err == nil {
			return ErrEmailExists
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var err error
		out, err = q.CreateAdmin(ctx, p)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrEmailExists
		}
		if err != nil {
			return err
		}
		return Audit(ctx, q, a, "admin.create", "admin", out.ID.String(), map[string]any{"role": p.Role})
	})
	return out, err
}

// lastActiveOwner reports whether target is the only active owner.
func lastActiveOwner(ctx context.Context, q *dbq.Queries, target dbq.Admin) (bool, error) {
	if target.Role != dbq.AdminRoleOwner || target.DisabledAt != nil {
		return false, nil
	}
	n, err := q.CountActiveOwners(ctx)
	return n <= 1, err
}

// SetAdminRole changes a role, never the actor's own and never the last owner's (owner only).
func (s *Store) SetAdminRole(ctx context.Context, a Actor, id uuid.UUID, role dbq.AdminRole) error {
	return s.adminTx(ctx, a, func(q *dbq.Queries, _ pgx.Tx) error {
		if id == a.AdminID {
			return ErrSelf
		}
		t, err := q.GetAdminByID(ctx, id)
		if err != nil {
			return NotFound(err)
		}
		if t.Role == role {
			return nil
		}
		if last, err := lastActiveOwner(ctx, q, t); err != nil || last {
			if err != nil {
				return err
			}
			return ErrLastOwner
		}
		if err := q.UpdateAdminRole(ctx, dbq.UpdateAdminRoleParams{ID: id, Role: role}); err != nil {
			return err
		}
		return Audit(ctx, q, a, "admin.role", "admin", id.String(), map[string]any{"from": t.Role, "to": role})
	})
}

// SetAdminDisabled disables (sessions ended) or re-enables an account (owner only).
func (s *Store) SetAdminDisabled(ctx context.Context, a Actor, id uuid.UUID, disabled bool) error {
	return s.adminTx(ctx, a, func(q *dbq.Queries, _ pgx.Tx) error {
		if id == a.AdminID {
			return ErrSelf
		}
		t, err := q.GetAdminByID(ctx, id)
		if err != nil {
			return NotFound(err)
		}
		action := "admin.enable"
		p := dbq.SetAdminDisabledParams{ID: id}
		if disabled {
			if last, err := lastActiveOwner(ctx, q, t); err != nil || last {
				if err != nil {
					return err
				}
				return ErrLastOwner
			}
			now := time.Now()
			p.DisabledAt = &now
			action = "admin.disable"
			if err := q.DeleteAdminSessions(ctx, id); err != nil {
				return err
			}
		}
		if err := q.SetAdminDisabled(ctx, p); err != nil {
			return err
		}
		return Audit(ctx, q, a, action, "admin", id.String(), nil)
	})
}

// ResetAdminPassword sets a temporary password (change forced, sessions ended; owner only).
func (s *Store) ResetAdminPassword(ctx context.Context, a Actor, id uuid.UUID, hash string) error {
	return s.adminTx(ctx, a, func(q *dbq.Queries, _ pgx.Tx) error {
		if _, err := q.GetAdminByID(ctx, id); err != nil {
			return NotFound(err)
		}
		if err := q.UpdateAdminPassword(ctx, dbq.UpdateAdminPasswordParams{ID: id, PasswordHash: hash, MustChangePassword: true}); err != nil {
			return err
		}
		if err := q.DeleteAdminSessions(ctx, id); err != nil {
			return err
		}
		return Audit(ctx, q, a, "admin.password_reset", "admin", id.String(), nil)
	})
}
