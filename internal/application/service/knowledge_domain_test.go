package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func domainTestContext(role types.TenantRole) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, "domain-administrator")
	return context.WithValue(ctx, types.TenantRoleContextKey, role)
}

func requireDomainError(t *testing.T, err error, status int) {
	t.Helper()
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok, "expected application error, got %v", err)
	require.Equal(t, status, appErr.HTTPCode)
}

func TestKnowledgeDomainCRUDAndPermissions(t *testing.T) {
	db := pgtest.Open(t)
	s := NewKnowledgeDomainService(db)
	admin := domainTestContext(types.TenantRoleAdmin)
	domain, err := s.Create(admin, types.KnowledgeDomain{Name: "材料科学", EntityTypes: []string{"材料"}, Fields: []types.DomainField{{Key: "strength", Label: "强度", ValueType: "number"}}})
	require.NoError(t, err)
	require.Equal(t, "domain-administrator", domain.CreatedBy)
	require.False(t, domain.IsBuiltin)
	_, err = s.Create(admin, types.KnowledgeDomain{Name: "材料科学"})
	requireDomainError(t, err, http.StatusConflict)
	for _, role := range []types.TenantRole{types.TenantRoleOwner, types.TenantRoleAdmin, types.TenantRoleContributor, types.TenantRoleViewer} {
		ctx := domainTestContext(role)
		items, err := s.List(ctx)
		require.NoError(t, err)
		require.Len(t, items, 1)
		_, err = s.Get(ctx, domain.ID)
		require.NoError(t, err)
		if role.HasPermission(types.TenantRoleAdmin) {
			continue
		}
		_, err = s.Create(ctx, types.KnowledgeDomain{Name: "Denied"})
		requireDomainError(t, err, http.StatusForbidden)
		_, err = s.Update(ctx, domain.ID, *domain)
		requireDomainError(t, err, http.StatusForbidden)
		err = s.Delete(ctx, domain.ID)
		requireDomainError(t, err, http.StatusForbidden)
		_, err = s.MapField(ctx, domain.ID, types.DomainFieldMapping{OpenField: "压力", FieldKey: "strength"})
		requireDomainError(t, err, http.StatusForbidden)
		_, err = s.AddField(ctx, domain.ID, types.DomainField{Key: "pressure", ValueType: "number"})
		requireDomainError(t, err, http.StatusForbidden)
	}
	domain.Description = "材料属性与研发动态"
	domain.Name = "先进材料"
	domain.IsBuiltin = true
	domain.CreatedBy = "client-supplied-owner"
	updated, err := s.Update(admin, domain.ID, *domain)
	require.NoError(t, err)
	require.Equal(t, "先进材料", updated.Name)
	require.Equal(t, "材料属性与研发动态", updated.Description)
	require.Equal(t, "domain-administrator", updated.CreatedBy)
	require.False(t, updated.IsBuiltin)
	require.NoError(t, s.Delete(admin, domain.ID))
	_, err = s.Get(admin, domain.ID)
	requireDomainError(t, err, http.StatusNotFound)
	_, err = s.Create(admin, types.KnowledgeDomain{Name: "先进材料"})
	require.NoError(t, err, "soft deleted names may be reused")
	_, err = s.List(context.Background())
	requireDomainError(t, err, http.StatusUnauthorized)
}

func TestBuiltinKnowledgeDomainBootstrapPreservesEdits(t *testing.T) {
	db := pgtest.Open(t)
	ctx := domainTestContext(types.TenantRoleOwner)
	require.NoError(t, EnsureSingleTeamSpace(ctx, db))
	for i := 0; i < 3; i++ {
		require.NoError(t, EnsureBuiltinKnowledgeDomains(ctx, db))
	}
	s := NewKnowledgeDomainService(db)
	items, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	domain := items[0]
	require.Equal(t, "AI 行业", domain.Name)
	require.True(t, domain.IsBuiltin)
	require.Len(t, domain.Fields, 4)
	require.Len(t, domain.Relations, 2)
	require.Equal(t, float64(1000000), domain.UnitFamilies[0].Units[2].Factor)
	require.Contains(t, domain.ExtractionHints, "Model versions are distinct objects")
	domain.Name = "AI 研究"
	domain.Fields[0].Aliases = append(domain.Fields[0].Aliases, "最大上下文")
	_, err = s.Update(ctx, domain.ID, domain)
	require.NoError(t, err)
	require.NoError(t, EnsureBuiltinKnowledgeDomains(ctx, db))
	items, err = s.List(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "AI 研究", items[0].Name)
	require.Contains(t, items[0].Fields[0].Aliases, "最大上下文")
	requireDomainError(t, s.Delete(ctx, domain.ID), http.StatusConflict)
}

func TestKnowledgeBaseRequiresActiveDomainAndBlocksDomainDeletion(t *testing.T) {
	db := pgtest.Open(t)
	ctx := domainTestContext(types.TenantRoleOwner)
	domains := NewKnowledgeDomainService(db)
	domain, err := domains.Create(ctx, types.KnowledgeDomain{Name: "物流"})
	require.NoError(t, err)
	s := &knowledgeBaseService{db: db, repo: repository.NewKnowledgeBaseRepository(db)}
	_, err = s.CreateKnowledgeBase(ctx, &types.KnowledgeBase{Name: "缺少领域"})
	requireDomainError(t, err, http.StatusBadRequest)
	_, err = s.CreateKnowledgeBase(ctx, &types.KnowledgeBase{Name: "无效领域", DomainID: uuid.NewString()})
	requireDomainError(t, err, http.StatusBadRequest)
	kb, err := s.CreateKnowledgeBase(ctx, &types.KnowledgeBase{Name: "物流新闻", DomainID: domain.ID})
	require.NoError(t, err)
	require.Equal(t, domain.ID, kb.DomainID)
	requireDomainError(t, domains.Delete(ctx, domain.ID), http.StatusConflict)
	second, err := domains.Create(ctx, types.KnowledgeDomain{Name: "航运"})
	require.NoError(t, err)
	kb, err = s.UpdateKnowledgeBase(ctx, kb.ID, kb.Name, kb.Description, nil, second.ID)
	require.NoError(t, err)
	require.Equal(t, second.ID, kb.DomainID)
	require.NoError(t, domains.Delete(ctx, domain.ID))
	_, err = s.UpdateKnowledgeBase(ctx, kb.ID, kb.Name, kb.Description, nil, "")
	requireDomainError(t, err, http.StatusBadRequest)
	_, err = s.UpdateKnowledgeBase(ctx, kb.ID, kb.Name, kb.Description, nil, domain.ID)
	requireDomainError(t, err, http.StatusBadRequest)
	require.NoError(t, db.Delete(kb).Error)
	require.NoError(t, domains.Delete(ctx, second.ID))
}

func TestDomainOpenFieldConsolidationRetainsSourceFacts(t *testing.T) {
	db := pgtest.Open(t)
	ctx := domainTestContext(types.TenantRoleAdmin)
	s := NewKnowledgeDomainService(db)
	domain, err := s.Create(ctx, types.KnowledgeDomain{Name: "医学", Fields: []types.DomainField{{Key: "dose", Label: "剂量", ValueType: "number"}}})
	require.NoError(t, err)
	other, err := s.Create(ctx, types.KnowledgeDomain{Name: "农业"})
	require.NoError(t, err)
	for i, domainID := range []string{domain.ID, domain.ID, other.ID} {
		kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: types.SingleTenantID, Name: "来源", DomainID: domainID}
		kb.EnsureDefaults()
		require.NoError(t, db.Create(kb).Error)
		evidence := &types.KnowledgeEvidence{ID: uuid.NewString(), TenantID: types.SingleTenantID, KnowledgeBaseID: kb.ID, KnowledgeID: uuid.NewString(), Version: 1, BodyHash: "source", Body: "每次给药量为 2。"}
		require.NoError(t, db.Create(evidence).Error)
		fact := &types.KnowledgeFact{ID: uuid.NewString(), TenantID: types.SingleTenantID, KnowledgeBaseID: kb.ID, SubjectEntityID: "drug", Kind: types.KnowledgeFactAttribute, Predicate: "给药量", OriginalPredicate: "给药量", Value: types.JSON(`2`), ValueType: "number", EvidenceID: evidence.ID, Quote: evidence.Body}
		require.NoError(t, db.Create(fact).Error)
		if i == 0 {
			known := *fact
			known.ID, known.Predicate, known.OriginalPredicate = uuid.NewString(), "dose", "剂量"
			require.NoError(t, db.Create(&known).Error)
		}
	}
	fields, err := s.OpenFields(domainTestContext(types.TenantRoleViewer), domain.ID)
	require.NoError(t, err)
	require.Len(t, fields, 1)
	require.Equal(t, "给药量", fields[0].Name)
	require.Equal(t, int64(2), fields[0].Count)
	require.Len(t, fields[0].ExampleFacts, 2)
	_, err = s.MapField(ctx, domain.ID, types.DomainFieldMapping{OpenField: "给药量", FieldKey: "dose"})
	require.NoError(t, err)
	_, err = s.MapField(ctx, domain.ID, types.DomainFieldMapping{OpenField: "给药量", FieldKey: "dose"})
	require.NoError(t, err, "same mapping is idempotent")
	fields, err = s.OpenFields(ctx, domain.ID)
	require.NoError(t, err)
	require.Empty(t, fields)
	var unchanged int64
	require.NoError(t, db.Model(&types.KnowledgeFact{}).Where("predicate = ? AND original_predicate = ?", "给药量", "给药量").Count(&unchanged).Error)
	require.Equal(t, int64(3), unchanged)
	_, err = s.AddField(ctx, other.ID, types.DomainField{Key: "给药量", Label: "给药量", ValueType: "number"})
	require.NoError(t, err)
	fields, err = s.OpenFields(ctx, other.ID)
	require.NoError(t, err)
	require.Empty(t, fields)
	_, err = s.MapField(ctx, domain.ID, types.DomainFieldMapping{OpenField: "剂量", FieldKey: "missing"})
	requireDomainError(t, err, http.StatusBadRequest)
}

func TestKnowledgeBaseDomainUpdateCannotRestoreDeletedBase(t *testing.T) {
	db := pgtest.Open(t)
	ctx := domainTestContext(types.TenantRoleAdmin)
	domains := NewKnowledgeDomainService(db)
	first, err := domains.Create(ctx, types.KnowledgeDomain{Name: "新闻"})
	require.NoError(t, err)
	second, err := domains.Create(ctx, types.KnowledgeDomain{Name: "研发"})
	require.NoError(t, err)
	s := &knowledgeBaseService{db: db, repo: repository.NewKnowledgeBaseRepository(db)}
	kb, err := s.CreateKnowledgeBase(ctx, &types.KnowledgeBase{Name: "团队动态", DomainID: first.ID})
	require.NoError(t, err)
	stale := *kb
	require.NoError(t, db.Delete(kb).Error)
	stale.DomainID, stale.Name = second.ID, "迟到的更新"
	requireDomainError(t, s.persistKnowledgeBase(ctx, &stale, false), http.StatusNotFound)
	var stored types.KnowledgeBase
	require.NoError(t, db.Unscoped().Where("id = ?", kb.ID).First(&stored).Error)
	require.True(t, stored.DeletedAt.Valid)
	require.Equal(t, first.ID, stored.DomainID)
	require.Equal(t, "团队动态", stored.Name)
}

func TestKnowledgeDomainTemplateValidation(t *testing.T) {
	valid := BuiltinAIKnowledgeDomain()
	require.NoError(t, validateKnowledgeDomain(valid))
	for _, edit := range []func(*types.KnowledgeDomain){
		func(d *types.KnowledgeDomain) { d.Fields[0].ValueType = "json" },
		func(d *types.KnowledgeDomain) { d.Fields[0].UnitFamily = "unknown" },
		func(d *types.KnowledgeDomain) { d.Fields[1].Aliases = append(d.Fields[1].Aliases, "context length") },
		func(d *types.KnowledgeDomain) { d.Fields[0].EntityTypes = []string{"未知类型"} },
		func(d *types.KnowledgeDomain) { d.UnitFamilies[0].Units[0].Factor = 0 },
		func(d *types.KnowledgeDomain) { d.UnitFamilies[0].Units[0].Factor = 2 },
		func(d *types.KnowledgeDomain) {
			d.UnitFamilies[0].Units[1].Aliases = append(d.UnitFamilies[0].Units[1].Aliases, "token")
		},
	} {
		domain := BuiltinAIKnowledgeDomain()
		edit(domain)
		requireDomainError(t, validateKnowledgeDomain(domain), http.StatusBadRequest)
	}
}
