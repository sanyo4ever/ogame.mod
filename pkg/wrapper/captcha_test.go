package wrapper

import "testing"

func TestCaptchaChallengeStateCanBeReconciledWithoutLeakingAStaleChallenge(t *testing.T) {
	bot := &OGame{}
	bot.rememberCaptchaChallenge("first")
	if got := bot.pendingCaptchaChallenge(); got != "first" {
		t.Fatalf("expected first challenge, got %q", got)
	}

	bot.forgetCaptchaChallenge("other")
	if got := bot.pendingCaptchaChallenge(); got != "first" {
		t.Fatalf("wrong challenge cleared the pending challenge: %q", got)
	}

	bot.forgetCaptchaChallenge("first")
	if got := bot.pendingCaptchaChallenge(); got != "" {
		t.Fatalf("expected challenge to be cleared, got %q", got)
	}
}

func TestSolvedCaptchaIsConsumedOnceForTheNextLogin(t *testing.T) {
	bot := &OGame{}
	bot.rememberSolvedCaptcha("solved")
	if !bot.hasSolvedCaptcha() {
		t.Fatal("expected a solved CAPTCHA to be pending")
	}

	if got := bot.takeSolvedCaptcha(); got != "solved" {
		t.Fatalf("expected solved challenge to be forwarded, got %q", got)
	}
	if bot.hasSolvedCaptcha() {
		t.Fatal("expected the solved CAPTCHA to be cleared after forwarding")
	}
	if got := bot.takeSolvedCaptcha(); got != "" {
		t.Fatalf("expected solved challenge to be consumed, got %q", got)
	}
}

func TestNewCaptchaInvalidatesPreviouslySolvedChallenge(t *testing.T) {
	bot := &OGame{}
	bot.rememberSolvedCaptcha("old")
	bot.rememberCaptchaChallenge("new")

	if got := bot.takeSolvedCaptcha(); got != "" {
		t.Fatalf("expected stale solved challenge to be discarded, got %q", got)
	}
}
