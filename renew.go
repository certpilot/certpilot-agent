package agent

import "time"

// renewAt is when this host asks for a replacement for held, and whether the
// core's instruction had to be overridden to get there.
//
// The core decides, in RenewAfter, and a sensible answer is taken exactly as
// given. The one exception is an instruction that cannot have been meant: a
// renew_after before the certificate was issued. A core without a bound on its
// lead time sent exactly that for any certificate shorter than the lead, and
// obeying it meant a new order to the CA on every cycle, 288 a day for one
// name (certpilot/certpilot#109).
//
// Replaced rather than ignored, because ignoring it would mean never renewing
// at all. The replacement is to renew when a third of the certificate's life
// remains, which is the rule the core itself now applies, so a host talking to
// a fixed core never reaches this and one talking to an old core behaves as if
// it were fixed.
//
// IssuedAt stands in for not_before, which this record does not keep. It is
// when this host received the certificate, which is within seconds of when the
// CA signed it and is what the host itself can vouch for.
func renewAt(held Held) (time.Time, bool) {
	if held.IssuedAt.IsZero() || !held.RenewAfter.Before(held.IssuedAt) {
		return held.RenewAfter, false
	}
	life := held.NotAfter.Sub(held.IssuedAt)
	if life <= 0 {
		return held.RenewAfter, false
	}
	return held.IssuedAt.Add(life * 2 / 3), true
}
