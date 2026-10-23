# Single discovery dispatch

This patch adapts the single-discovery safeguards from the MIT-licensed
[Ronnie32/ogame.mod](https://github.com/ronnie32/ogame.mod) release source at
`e9f365f803e15f937916ef152462f231b8676512`, specifically:

- `b7f9e90cc5be47bfc43b92d7907746fff0134857`: redirect classification and a fleet watermark.
- `33d070cb08247fc618e3def964355133ef9bc730`: use the freshly loaded galaxy page token.
- `a0c8113077f54f7194495bde15da2862d9e3f8c8`: one discovery POST and explicit v13 response errors.

The original MIT notice, Copyright (c) 2018 Alain Gilbert, remains in the root
LICENSE. The new code is a narrow adaptation to this fork's existing transport
and parsers. It does not import donor files, bulk-system discovery, login or PoW
changes, dependencies, or replacements for existing protocol validators.

The dispatch requires a target position with SearchForLifeforms mission 18 and
literal boolean canSend=true, a nonempty page-scoped token, and a readable
pre-dispatch fleet snapshot with positive fleet IDs. It preserves the sending
origin option and never automatically replays its state-changing POST.

Discovery reads use a separate strict movement snapshot path; the general
fleet extractor and other callers keep their existing compatibility behavior.
Each snapshot needs an original HTTP 2xx response with no followed redirect,
a movement page identified by the existing extractor (including the source
shape body#ingamepage with currentPage=movement), exactly one nonnegative
numeric occupied-slot count, the same number of fleet-detail rows, and unique
explicit positive fleet IDs
that agree with the existing extractor. Mobile UI may have multiple detail
links per fleet row; every link must supply the same positive ID. Missing,
invalid or conflicting link IDs and duplicate IDs across rows are rejected.
Missing or inconsistent rows prevent the POST when detected before dispatch,
and leave its outcome unconfirmed when
detected after dispatch. The occupied-slot/detail count agrees in all nine
checked-in movement samples (v7 through v11.13.0, including mobile); no modern
live-server completeness guarantee is inferred from those historical samples.

An HTTP 200 response needs an explicit boolean success=true at the top level or
inside response. Explicit errors, non-success statuses, false/null/invalid
success values and contradictory outer/nested responses are rejected before
success is consumed. Busy code 100 remains an error. Legacy galaxy responses
also reject explicit failure envelopes before their mission data is consumed.

Only HTTP 302 redirects to the same scheme/host, /game/index.php, and the known
overview/fleetdispatch forms of page=ingame or page=componentOnly (or no page
and no component) may be reconciled. Redirect bodies do not prove dispatch.
External, login, unknown-page and other HTTP statuses return errors without
following the redirect or replaying the POST.

Reconciliation reads movement at most three times, with cancellable 300 ms gaps.
It requires exactly one positive fleet ID absent from the entire earlier ID
snapshot, explicit data-return-flight=false and mission 18 attributes, exact
origin/destination coordinate anchors, and explicit matching planet/moon figure
classes. An old ID, returning fleet, changed route, unrelated mission,
missing new fleet or multiple matching new fleets cannot prove this dispatch.
SendDiscoveryFleet2 uses the same watermark rather than returning an old fleet.

Historical movement samples contain empty data-return-flight attributes. Their
IDs may participate in a complete watermark, but empty, missing, numeric or
unknown direction values cannot prove a new outgoing fleet. Missing or unknown
celestial figure types also cannot prove a route. If a current server uses only
those ambiguous forms, redirect reconciliation is deliberately unavailable and
returns an unconfirmed outcome; the POST is never replayed. An explicit valid
JSON success remains usable by SendDiscoveryFleet after a complete watermark.
Historical no-fleet movement redirects to fleet1 are not accepted as complete
watermarks: discovery stops before its POST when that view cannot be verified.

These are synthetic localhost transport tests using the existing real
extractors. No new account captures or live game/session requests were used. The
protocol shapes are source-based assumptions, not live-server certification.
A lost response stays unconfirmed. Visibility delayed beyond the bounded reads
also stays unconfirmed. A concurrent manual discovery with exactly the same
route cannot be distinguished by an API without a server request identifier;
this patch does not claim general fleet dispatch idempotency or account-wide
serialization. The C# bridge must independently avoid POST retries.
