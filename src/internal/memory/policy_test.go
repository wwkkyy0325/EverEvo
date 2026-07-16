package memory

import "testing"

func TestDefaultMemoryPolicy(t *testing.T) {
	p := DefaultMemoryPolicy()
	if p.Tier != "standard" || p.HalfLifeDays != 14 || p.TTLDays != 90 || p.Alpha != 0.7 {
		t.Errorf("default policy unexpected: %+v", p)
	}
}

func TestDecayScore(t *testing.T) {
	p := DefaultMemoryPolicy() // halfLife 14, alpha 0.7
	day := int64(86400000)
	now := 1000 * day

	// Fresh (age 0): score = α·1 + (1-α)·1 = 1.0
	fresh := decayScore(1.0, now, now, "normal", p, now)
	if fresh < 0.99 {
		t.Errorf("fresh score: want ~1.0, got %f", fresh)
	}

	// One half-life old (14d): recency 0.5 → 0.7 + 0.3·0.5 = 0.85
	half := decayScore(1.0, now-14*day, now-14*day, "normal", p, now)
	if half < 0.84 || half > 0.86 {
		t.Errorf("1 half-life: want ~0.85, got %f", half)
	}

	// "low" importance ages 2× → 14d acts like 28d → recency 0.25 → 0.775
	eph := decayScore(1.0, now-14*day, now-14*day, "low", p, now)
	if eph >= half {
		t.Errorf("ephemeral should decay faster than normal: got %f (normal %f)", eph, half)
	}

	// Negative similarity clamped to 0 (score = 0 + (1-α)·recency)
	neg := decayScore(-0.5, now, now, "normal", p, now)
	if neg < 0.29 || neg > 0.31 {
		t.Errorf("clamped neg sim fresh: want ~0.30, got %f", neg)
	}

	// Falls back to createdAt when last_access is 0
	fallback := decayScore(1.0, 0, now, "normal", p, now)
	if fallback < 0.99 {
		t.Errorf("createdAt fallback (fresh): want ~1.0, got %f", fallback)
	}
}
