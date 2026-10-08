package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}

func TestLoadTimezone_EmptyMeansDefault(t *testing.T) {
	loc, err := LoadTimezone("")
	require.NoError(t, err)
	require.Equal(t, DefaultTimezone, loc.String())
}

func TestLoadTimezone_RejectsUnknownName(t *testing.T) {
	_, err := LoadTimezone("Mars/Olympus")
	require.Error(t, err)
}

func TestOrgLocation_FallsBackInsteadOfFailing(t *testing.T) {
	require.Equal(t, "Asia/Bangkok", OrgLocation("Asia/Bangkok").String())
	require.Equal(t, DefaultTimezone, OrgLocation("Mars/Olympus").String())
	require.Equal(t, DefaultTimezone, OrgLocation("").String())
}

// The whole point of #9: Myanmar is UTC+6:30, so 2026-10-08 00:00 local is
// 2026-10-07 17:30 UTC - not the UTC midnight the old code used.
func TestStartOfDay_IsLocalMidnightNotUTCMidnight(t *testing.T) {
	yangon := mustLoc(t, "Asia/Yangon")
	date := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC) // how a parsed "2026-10-08" arrives

	start := StartOfDay(date, yangon)

	require.Equal(t, time.Date(2026, 10, 7, 17, 30, 0, 0, time.UTC), start.UTC())
	require.Equal(t, time.Date(2026, 10, 9, 0, 0, 0, 0, yangon), NextDay(start))
	require.Equal(t, 24*time.Hour, NextDay(start).Sub(start))
}

func TestStartOfDay_UsesTheDatesOwnYMDEvenWhenGivenAnInstantInAnotherZone(t *testing.T) {
	yangon := mustLoc(t, "Asia/Yangon")
	// 23:00 Oct 7 in Yangon is already Oct 8 in UTC; the date to honour is the
	// one written on the value passed in (its own location), here Oct 7.
	local := time.Date(2026, 10, 7, 23, 0, 0, 0, yangon)
	require.Equal(t, time.Date(2026, 10, 7, 0, 0, 0, 0, yangon), StartOfDay(local, yangon))
}

// A DST zone: the day of the spring-forward is 23h long, so +24h would be wrong.
func TestNextDay_IsCalendarArithmeticAcrossDST(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	start := StartOfDay(time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC), ny)
	require.Equal(t, 23*time.Hour, NextDay(start).Sub(start))
}
