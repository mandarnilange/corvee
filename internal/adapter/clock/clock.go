package clock

import "time"

// System is the production Clock implementation. Now returns the
// current wall-clock time in UTC.
type System struct{}

// New returns a System clock.
func New() System { return System{} }

// Now returns time.Now().UTC().
func (System) Now() time.Time { return time.Now().UTC() }
