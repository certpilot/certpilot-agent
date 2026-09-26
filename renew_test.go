package agent

import (
	"testing"
	"time"
)

// certpilot/certpilot#109. A core without a bound on its lead time handed this
// agent a renew_after 24 days before a six-day certificate was issued, and the
// agent, doing exactly what the core said, ordered a replacement on every
// five-minute cycle. The core no longer sends that. This host is the side that
// would do the damage, and it can see that the instruction is impossible.

func TestARenewAfterBeforeTheCertificateExistedIsNotObeyed(t *testing.T) {
	issued := time.Date(2026, 9, 22, 21, 30, 7, 0, time.UTC)
	held := Held{
		Names:      []string{"shop.pebble.test"},
		IssuedAt:   issued,
		NotAfter:   issued.Add(6 * 24 * time.Hour),
		RenewAfter: issued.Add(-24 * 24 * time.Hour), // what #109 measured
	}

	at, overridden := renewAt(held)
	if !overridden {
		t.Fatal("a renew_after 24 days before issue was obeyed; the host renews on every cycle")
	}
	// When a third of its life remains, which is the rule the core now applies
	// itself: day four of six.
	if want := issued.Add(4 * 24 * time.Hour); !at.Equal(want) {
		t.Errorf("renews at %v, want %v (day four of six)", at, want)
	}
}

// The core decides, and a sensible answer is taken exactly as given. A host
// that second-guessed the core whenever it disagreed would be a host that could
// decide to renew hourly.
func TestASensibleRenewAfterIsObeyedExactly(t *testing.T) {
	issued := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	for _, renewAfter := range []time.Time{
		issued,                           // the earliest a real instruction can be
		issued.Add(time.Hour),            // early, but after issue: the core's call
		issued.Add(60 * 24 * time.Hour),  // the ordinary 90-day, 30-day-lead case
		issued.Add(100 * 24 * time.Hour), // after expiry: odd, and still the core's call
	} {
		held := Held{IssuedAt: issued, NotAfter: issued.Add(90 * 24 * time.Hour), RenewAfter: renewAfter}
		at, overridden := renewAt(held)
		if overridden || !at.Equal(renewAfter) {
			t.Errorf("renew_after %v: got %v (overridden %v), want it obeyed exactly", renewAfter, at, overridden)
		}
	}
}

// Held records written before IssuedAt existed, or with an expiry that makes no
// sense, give nothing to reason from. Those keep the core's answer rather than
// one invented here.
func TestWithNothingToReasonFromTheCoreIsObeyed(t *testing.T) {
	past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := map[string]Held{
		"no issue time":       {NotAfter: past.Add(90 * 24 * time.Hour), RenewAfter: past},
		"expiry before issue": {IssuedAt: past, NotAfter: past.Add(-time.Hour), RenewAfter: past.Add(-2 * time.Hour)},
	}
	for name, held := range cases {
		if at, overridden := renewAt(held); overridden || !at.Equal(held.RenewAfter) {
			t.Errorf("%s: got %v (overridden %v), want renew_after %v obeyed", name, at, overridden, held.RenewAfter)
		}
	}
}
