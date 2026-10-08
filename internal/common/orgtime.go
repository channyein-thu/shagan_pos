package common

import "time"

// DefaultTimezone is the IANA zone an organization uses unless it was given
// another one - Shagan's market is Myanmar (UTC+6:30, no DST).
const DefaultTimezone = "Asia/Yangon"

// LoadTimezone resolves an IANA zone name ("" means DefaultTimezone). Used to
// validate a name before it is stored; use OrgLocation to read a stored one.
func LoadTimezone(name string) (*time.Location, error) {
	if name == "" {
		name = DefaultTimezone
	}
	return time.LoadLocation(name)
}

// OrgLocation resolves a stored organization timezone for use in date math.
// A stored name is validated on write, so a failure here means the runtime
// lacks tz data or the row was edited by hand; rather than failing every
// report it falls back to DefaultTimezone, then UTC.
func OrgLocation(name string) *time.Location {
	if loc, err := LoadTimezone(name); err == nil {
		return loc
	}
	if loc, err := time.LoadLocation(DefaultTimezone); err == nil {
		return loc
	}
	return time.UTC
}

// StartOfDay is midnight, in loc, of t's calendar date - t's own Y/M/D as
// written (so a "2026-10-08" parsed as UTC midnight means Oct 8 in loc, not
// the instant it denotes).
func StartOfDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// NextDay is the midnight after a StartOfDay value, in the same zone. Done
// by calendar arithmetic, not +24h, so a DST change can't skew it.
func NextDay(startOfDay time.Time) time.Time {
	return startOfDay.AddDate(0, 0, 1)
}
