package gameforge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

type captchaTestDevice struct {
	responses []captchaTestResponse
	requests  []*http.Request
}

type captchaTestResponse struct {
	status int
	header http.Header
	body   []byte
}

func (d *captchaTestDevice) Do(req *http.Request) (*http.Response, error) {
	d.requests = append(d.requests, req.Clone(req.Context()))
	if len(d.responses) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	response := d.responses[0]
	d.responses = d.responses[1:]
	return &http.Response{
		StatusCode: response.status,
		Status:     http.StatusText(response.status),
		Header:     response.header,
		Body:       io.NopCloser(bytes.NewReader(response.body)),
		Request:    req,
	}, nil
}

func (d *captchaTestDevice) GetBlackbox() (string, error) {
	return "blackbox", nil
}

func TestLoginCarriesSolvedCaptchaChallengeToTheSingleRetry(t *testing.T) {
	challengeID := "challenge-1"
	device := &captchaTestDevice{responses: []captchaTestResponse{
		{status: http.StatusOK, body: []byte(`{"gameEnvironmentId":"env","platformGameId":"game"}`)},
		{status: http.StatusConflict, header: func() http.Header {
			header := make(http.Header)
			header.Set(ChallengeIDCookieName, challengeID+";https://challenge.gameforge.com")
			return header
		}()},
		{status: http.StatusOK, body: []byte("challenge")},
		{status: http.StatusOK, body: []byte("locale")},
		{status: http.StatusOK, body: []byte("question")},
		{status: http.StatusOK, body: []byte("icons")},
		{status: http.StatusOK},
		{status: http.StatusOK, body: []byte(`{"gameEnvironmentId":"env","platformGameId":"game"}`)},
		{status: http.StatusCreated, body: []byte(`{"token":"token-1"}`)},
	}}

	var solverCalls int
	solver := func(_ context.Context, question, icons []byte) (int64, error) {
		solverCalls++
		if string(question) != "question" {
			t.Fatalf("unexpected question payload %q", question)
		}
		if string(icons) != "icons" {
			t.Fatalf("unexpected icons payload %q", icons)
		}
		return 2, nil
	}

	gf, err := New(&Config{
		Ctx:      context.Background(),
		Device:   device,
		Platform: OGAME,
		Solver:   solver,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := gf.Login(&LoginParams{Username: "user@example.com", Password: "password"}); err != nil {
		t.Fatal(err)
	}
	if solverCalls != 1 {
		t.Fatalf("expected one captcha solve, got %d", solverCalls)
	}
	if len(device.requests) != 9 {
		t.Fatalf("expected 9 HTTP requests, got %d", len(device.requests))
	}
	expectedURLs := []string{
		"https://lobby.ogame.gameforge.com/config/configuration.js",
		"https://spark-web.gameforge.com/api/v2/authProviders/mauth/sessions",
		"https://challenge.gameforge.com/challenge/" + challengeID,
		"https://image-drop-challenge.gameforge.com/challenge/" + challengeID + "/en-GB",
		"https://image-drop-challenge.gameforge.com/challenge/" + challengeID + "/en-GB/text",
		"https://image-drop-challenge.gameforge.com/challenge/" + challengeID + "/en-GB/drag-icons",
		"https://image-drop-challenge.gameforge.com/challenge/" + challengeID + "/en-GB",
		"https://lobby.ogame.gameforge.com/config/configuration.js",
		"https://spark-web.gameforge.com/api/v2/authProviders/mauth/sessions",
	}
	for i, expected := range expectedURLs {
		if got := device.requests[i].URL.String(); got != expected {
			t.Fatalf("request %d: expected URL %q, got %q", i, expected, got)
		}
	}

	if got := device.requests[1].Header.Get(ChallengeIDCookieName); got != "" {
		t.Fatalf("initial login unexpectedly carried challenge id %q", got)
	}
	retry := device.requests[8]
	if got := retry.Method; got != http.MethodPost {
		t.Fatalf("expected retry to be POST, got %s", got)
	}
	if got := retry.Header.Get(ChallengeIDCookieName); got != challengeID {
		t.Fatalf("expected retry to carry challenge id %q, got %q", challengeID, got)
	}
	if got := retry.Header.Get(contentTypeHeaderKey); got != applicationJson {
		t.Fatalf("expected JSON content type, got %q", got)
	}
	var solvePayload struct {
		Answer int64 `json:"answer"`
	}
	if err := json.NewDecoder(device.requests[6].Body).Decode(&solvePayload); err != nil {
		t.Fatalf("could not decode solve payload: %v", err)
	}
	if solvePayload.Answer != 2 {
		t.Fatalf("expected answer 2, got %d", solvePayload.Answer)
	}
}

func TestStartChallengeRejectsNonSuccessResponses(t *testing.T) {
	device := &captchaTestDevice{responses: []captchaTestResponse{{status: http.StatusUnauthorized}}}
	_, _, err := StartChallenge(context.Background(), device, "challenge-1")
	if err == nil {
		t.Fatal("expected a non-success CAPTCHA response to fail")
	}
}
