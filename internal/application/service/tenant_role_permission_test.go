package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestKnowledgeDomainAccessRoleMatrix(t *testing.T) {
	cases := []struct {
		role     types.TenantRole
		canWrite bool
	}{
		{types.TenantRoleViewer, false},
		{types.TenantRoleContributor, false},
		{types.TenantRoleAdmin, true},
		{types.TenantRoleOwner, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.role), func(t *testing.T) {
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
			ctx = context.WithValue(ctx, types.TenantRoleContextKey, tc.role)
			tenantID, err := knowledgeDomainAccess(ctx, false)
			require.NoError(t, err)
			require.Equal(t, types.SingleTenantID, tenantID)
			_, err = knowledgeDomainAccess(ctx, true)
			if tc.canWrite {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestKnowledgeWorkflowAuthorizationRoleMatrix(t *testing.T) {
	cases := []struct {
		role                     types.TenantRole
		canWrite, canManageOther bool
	}{
		{types.TenantRoleViewer, false, false},
		{types.TenantRoleContributor, true, false},
		{types.TenantRoleAdmin, true, true},
		{types.TenantRoleOwner, true, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.role), func(t *testing.T) {
			ctx := workflowTestContext("member", types.SingleTenantID)
			svc := &KnowledgeWorkflowService{
				kbs:     &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: types.SingleTenantID}},
				members: workflowTestMembers{member: &types.TenantMember{UserID: "member", TenantID: types.SingleTenantID, Role: tc.role, Status: types.TenantMemberStatusActive}},
			}
			_, _, _, err := svc.authorizeKB(ctx, "kb")
			if tc.canWrite {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrWorkflowForbidden)
			}
			wf := &types.KnowledgeWorkflow{TenantID: types.SingleTenantID, KnowledgeBaseID: "kb", OwnerID: "another-member"}
			err = svc.authorizeWorkflow(ctx, wf)
			if tc.canManageOther {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrWorkflowForbidden)
			}
			wf.OwnerID = "member"
			err = svc.authorizeWorkflow(ctx, wf)
			if tc.canWrite {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrWorkflowForbidden)
			}
		})
	}
}

func TestKnowledgeWorkflowRejectsInvalidCurrentMemberRole(t *testing.T) {
	ctx := workflowTestContext("member", types.SingleTenantID)
	svc := &KnowledgeWorkflowService{members: workflowTestMembers{member: &types.TenantMember{UserID: "member", TenantID: types.SingleTenantID, Role: "invalid-role", Status: types.TenantMemberStatusActive}}}
	_, err := svc.currentRole(ctx, types.SingleTenantID, "member")
	require.ErrorIs(t, err, ErrWorkflowForbidden)
	require.ErrorIs(t, svc.authorizeActor(ctx, types.SingleTenantID, "member"), ErrWorkflowForbidden, "invalid membership must not receive the resource creator exception")
}
