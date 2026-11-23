package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestShelfCertificationWriteRejectsObtainedDateAfterLocalToday(t *testing.T) {
	localNow := time.Date(2026, 11, 20, 0, 30, 0, 0, time.Local)
	_, offset := localNow.Zone()
	clockTime := localNow.In(time.FixedZone("clock", offset-3600))
	s := NewEntityService(&gorm.DB{Config: &gorm.Config{NowFunc: func() time.Time { return clockTime }}}, nil)

	for _, date := range []string{"", "2026-11-19", "2026-11-20"} {
		require.NoError(t, s.checkShelfCertificationObtainedDate(date), "obtained_date=%q", date)
	}
	require.ErrorContains(t, s.checkShelfCertificationObtainedDate("2026-11-21"), "实际取证日期不能晚于今天，未取证请填写 planned_date")
}
