package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/types"
)

var ErrTeamInvitationRequired = errors.New("registration is invite-only after the first user")
var ErrInvalidTeamInvitation = errors.New("invitation is invalid or expired")

func (r *userRepository) HasTeamUsers(ctx context.Context) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Unscoped().Model(&types.User{}).Count(&count).Error
	return count > 0, err
}

// RegisterTeamUser commits the account, role and invitation use together.
// The transaction lock also serializes first-user registration across processes.
func (r *userRepository) RegisterTeamUser(ctx context.Context, user *types.User, invitationToken string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651001)).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Unscoped().Model(&types.User{}).Count(&count).Error; err != nil {
			return err
		}
		now := time.Now()
		role := types.TenantRoleOwner
		var invitation *types.TenantInvitation
		if count > 0 {
			if invitationToken == "" {
				return ErrTeamInvitationRequired
			}
			invitation = &types.TenantInvitation{}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("token = ? AND tenant_id = ? AND status = ? AND invitee_user_id = ''",
					invitationToken, types.SingleTenantID, types.TenantInvitationStatusPending).
				First(invitation).Error; err != nil {
				return ErrInvalidTeamInvitation
			}
			if invitation.IsExpired(now) || !invitation.Role.IsValid() || invitation.Role == types.TenantRoleOwner {
				return ErrInvalidTeamInvitation
			}
			role = invitation.Role
		}
		user.TenantID = types.SingleTenantID
		user.IsSystemAdmin = count == 0
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		member := &types.TenantMember{
			UserID: user.ID, TenantID: types.SingleTenantID, Role: role,
			Status: types.TenantMemberStatusActive, JoinedAt: now,
		}
		if invitation != nil {
			member.InvitedBy = invitation.InvitedBy
		}
		if err := tx.Create(member).Error; err != nil {
			return err
		}
		if invitation != nil {
			return tx.Model(invitation).Updates(map[string]any{
				"accepted_count": gorm.Expr("accepted_count + 1"), "updated_at": now,
			}).Error
		}
		return nil
	})
}
