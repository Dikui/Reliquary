package service

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestKnowledgeDomainDeletionReportsOnlyActiveTeamReferences(t *testing.T) {
	db := pgtest.Open(t)
	ctx := domainTestContext(types.TenantRoleAdmin)
	svc := NewKnowledgeDomainService(db)
	domain, err := svc.Create(ctx, types.KnowledgeDomain{Name: "Referenced domain", Description: "Preserved on conflict"})
	require.NoError(t, err)
	otherDomain, err := svc.Create(ctx, types.KnowledgeDomain{Name: "Other domain"})
	require.NoError(t, err)
	createKB := func(name, domainID string, tenantID uint64) *types.KnowledgeBase {
		t.Helper()
		kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenantID, DomainID: domainID, Name: name}
		kb.EnsureDefaults()
		require.NoError(t, db.Create(kb).Error)
		return kb
	}
	first := createKB("First reference", domain.ID, types.SingleTenantID)
	second := createKB("Second reference", domain.ID, types.SingleTenantID)
	deleted := createKB("Deleted reference", domain.ID, types.SingleTenantID)
	require.NoError(t, db.Delete(deleted).Error)
	createKB("Other domain", otherDomain.ID, types.SingleTenantID)
	createKB("Historical foreign team", domain.ID, 42)

	for _, expectedCount := range []int64{2, 1} {
		err := svc.Delete(ctx, domain.ID)
		appErr, ok := apperrors.IsAppError(err)
		require.True(t, ok, "expected structured conflict, got %v", err)
		require.Equal(t, http.StatusConflict, appErr.HTTPCode)
		require.Equal(t, apperrors.ErrKnowledgeDomainReferenced, appErr.Code)
		details, ok := appErr.Details.(types.KnowledgeDomainReferenceDetails)
		require.True(t, ok, "expected domain reference details, got %T", appErr.Details)
		require.Equal(t, expectedCount, details.KnowledgeBaseCount)
		preserved, err := svc.Get(ctx, domain.ID)
		require.NoError(t, err)
		require.False(t, preserved.DeletedAt.Valid)
		require.Equal(t, "Preserved on conflict", preserved.Description)
		if expectedCount == 2 {
			require.NoError(t, db.Delete(first).Error)
		}
	}
	require.NoError(t, db.Delete(second).Error)
	require.NoError(t, svc.Delete(ctx, domain.ID), "deleted, other-domain and foreign-team references cannot block deletion")
	_, err = svc.Get(ctx, domain.ID)
	requireDomainError(t, err, http.StatusNotFound)
}
