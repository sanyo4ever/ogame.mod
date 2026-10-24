package gameforge

import "testing"

func TestCaptchaEndpointsRejectInvalidInputBeforeMakingARequest(t *testing.T) {
	if _, _, err := StartChallenge(nil, nil, ""); err == nil {
		t.Fatal("expected an empty challenge id to be rejected")
	}
	if err := SolveChallenge(nil, nil, "", 0); err == nil {
		t.Fatal("expected an empty challenge id to be rejected")
	}
	if err := SolveChallenge(nil, nil, "challenge", -1); err == nil {
		t.Fatal("expected a negative answer to be rejected")
	}
	if err := SolveChallenge(nil, nil, "challenge", 4); err == nil {
		t.Fatal("expected an answer above 3 to be rejected")
	}
}

func TestSolvePowKnown(t *testing.T) {
	n := SolvePow("c8ea7be2079a01ba2596b138096adc264e5f1d3eff31a8658e1bf557bf77e123", "00000")
	if n != 817404 {
		t.Fatalf("expected 817404, got %d", n)
	}
	n2 := SolvePowParallel("c8ea7be2079a01ba2596b138096adc264e5f1d3eff31a8658e1bf557bf77e123", "00000", 8)
	if n2 != 817404 {
		t.Fatalf("parallel expected 817404, got %d", n2)
	}
}
