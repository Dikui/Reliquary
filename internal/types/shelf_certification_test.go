package types

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShelfCertificationStatusUsesBusinessDateAndPrecedence(t *testing.T) {
	day := time.Date(2026, 11, 20, 0, 30, 0, 0, time.Local)
	for _, tc := range []struct {
		name   string
		record ShelfCertification
		want   string
	}{
		{"future actual overrides completed text", ShelfCertification{ObtainedDate: "2026-11-21", Progress: "已完成"}, "date_conflict"},
		{"actual today overrides paused", ShelfCertification{ObtainedDate: "2026-11-20", Progress: "暂停"}, "obtained"},
		{"past actual", ShelfCertification{ObtainedDate: "2026-11-19"}, "obtained"},
		{"obtained text", ShelfCertification{Progress: "已取证"}, "obtained"},
		{"completed text", ShelfCertification{Progress: "已完成", PlannedDate: "2026-11-19"}, "obtained"},
		{"paused overrides overdue plan", ShelfCertification{Progress: "测试暂停中", PlannedDate: "2026-11-19"}, "paused"},
		{"overdue plan overrides not started", ShelfCertification{Progress: "未启动", PlannedDate: "2026-11-19"}, "delayed"},
		{"plan due today", ShelfCertification{PlannedDate: "2026-11-20"}, "in_progress"},
		{"future plan", ShelfCertification{PlannedDate: "2026-11-21"}, "in_progress"},
		{"waiting to start", ShelfCertification{Progress: "待启动", PlannedDate: "2026-11-20"}, "not_started"},
		{"not started", ShelfCertification{Progress: "未启动"}, "not_started"},
		{"progress text", ShelfCertification{Progress: "进行中"}, "in_progress"},
		{"start date", ShelfCertification{StartDate: "2026-11-19"}, "in_progress"},
		{"notes alone", ShelfCertification{Notes: "待补充"}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ShelfCertificationStatus(tc.record, day, day))
		})
	}
}

func TestShelfCertificationStatusUsesLocalTodayAndLocalAsOf(t *testing.T) {
	localToday := time.Date(2026, 11, 20, 0, 30, 0, 0, time.Local)
	_, offset := localToday.Zone()
	requestZone := time.FixedZone("request", offset-2*60*60)
	today := localToday.In(requestZone)
	yesterday := localToday.AddDate(0, 0, -1).In(requestZone)
	future := localToday.AddDate(0, 0, 2).In(requestZone)
	for _, tc := range []struct {
		name   string
		asOf   time.Time
		record ShelfCertification
		want   string
	}{
		{"later actual keeps historical progress", yesterday, ShelfCertification{ObtainedDate: "2026-11-20", Progress: "进行中"}, "in_progress"},
		{"later actual overrides completed text at historical date", yesterday, ShelfCertification{ObtainedDate: "2026-11-20", Progress: "已取证"}, "in_progress"},
		{"later actual allows historical delay", yesterday, ShelfCertification{ObtainedDate: "2026-11-20", Progress: "已完成", PlannedDate: "2026-11-18"}, "delayed"},
		{"actual on the local as-of date", yesterday, ShelfCertification{ObtainedDate: "2026-11-19"}, "obtained"},
		{"actual on local today", today, ShelfCertification{ObtainedDate: "2026-11-20"}, "obtained"},
		{"local plan due today", yesterday, ShelfCertification{PlannedDate: "2026-11-19"}, "in_progress"},
		{"future actual at historical as-of", yesterday, ShelfCertification{ObtainedDate: "2026-11-21"}, "date_conflict"},
		{"future actual at as-of after actual date", future, ShelfCertification{ObtainedDate: "2026-11-21"}, "date_conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ShelfCertificationStatus(tc.record, tc.asOf, today))
		})
	}
}

func TestValidateShelfCertificationLimitsNotesTo20000Characters(t *testing.T) {
	record := ShelfCertification{OwnerID: "model", Standard: "standard", CertificationObject: "sample"}
	record.Notes = strings.Repeat("注", 20000)
	require.NoError(t, ValidateShelfCertification(record))
	record.Notes += "记"
	require.ErrorContains(t, ValidateShelfCertification(record), "certification text is too long")
}
