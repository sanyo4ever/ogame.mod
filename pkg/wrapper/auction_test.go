package wrapper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/alaingilbert/ogame/pkg/device"
	"github.com/alaingilbert/ogame/pkg/extractor/v13_0_0"
	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV13GetAuctionUsesTraderWorkflow(t *testing.T) {
	var requests []httpRequestSnapshot
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, snapshotRequest(r))
		switch len(requests) {
		case 1:
			assert.Equal(t, url.Values{
				"page":      {"ingame"},
				"component": {"trader"},
				"tab":       {"auctioneer"},
				"ajax":      {"1"},
			}, r.URL.Query())
			writeJSON(t, w, map[string]any{
				"content":      map[string]string{"trader": "<div>trader overview</div>"},
				"newAjaxToken": "fetch-token",
			})
		case 2:
			assert.Equal(t, url.Values{
				"page":      {"ingame"},
				"component": {"trader"},
				"action":    {"auctioneer"},
				"ajax":      {"1"},
			}, r.URL.Query())
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "auctioneer", r.Form.Get("action"))
			assert.Equal(t, "fetch-token", r.Form.Get("token"))
			assert.Equal(t, "1", r.Form.Get("ajax"))
			writeJSON(t, w, map[string]any{
				"content":      map[string]string{"trader": v13AuctionHTML("null")},
				"newAjaxToken": "auction-token",
			})
		default:
			t.Fatalf("unexpected request %d", len(requests))
		}
	}))
	defer server.Close()

	bot := newV13AuctionTestBot(t, server.URL)
	auction, err := bot.getAuction(0)

	require.NoError(t, err)
	assert.Equal(t, "auction-token", auction.Token)
	assert.Equal(t, "energy booster bronze", auction.CurrentItem)
	assert.Len(t, requests, 2)
}

func TestV13GetAllResourcesUsesTheSameTraderWorkflow(t *testing.T) {
	var requests []httpRequestSnapshot
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, snapshotRequest(r))
		if len(requests) == 1 {
			writeJSON(t, w, map[string]any{
				"content":      map[string]string{"trader": "<div>trader overview</div>"},
				"newAjaxToken": "fetch-token",
			})
			return
		}
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "fetch-token", r.Form.Get("token"))
		writeJSON(t, w, map[string]any{
			"content":      map[string]string{"trader": v13AuctionHTML("null")},
			"newAjaxToken": "auction-token",
		})
	}))
	defer server.Close()

	bot := newV13AuctionTestBot(t, server.URL)
	resources, err := bot.getAllResources()

	require.NoError(t, err)
	assert.Equal(t, ogame.Resources{Metal: 100, Crystal: 200, Deuterium: 300}, resources[123])
	assert.Len(t, requests, 2)
}

func TestV13DoAuctionUsesAuctioneerSubmitBid(t *testing.T) {
	var requests []httpRequestSnapshot
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, snapshotRequest(r))
		switch len(requests) {
		case 1:
			writeJSON(t, w, map[string]any{
				"content":      map[string]string{"trader": "<div>trader overview</div>"},
				"newAjaxToken": "fetch-token",
			})
		case 2:
			writeJSON(t, w, map[string]any{
				"content":      map[string]string{"trader": v13AuctionHTML("null")},
				"newAjaxToken": "auction-token",
			})
		case 3:
			assert.Equal(t, url.Values{
				"page":      {"ingame"},
				"component": {"trader"},
				"asJson":    {"1"},
			}, r.URL.Query())
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "auctioneerSubmitBid", r.Form.Get("action"))
			assert.Equal(t, "auction-token", r.Form.Get("token"))
			assert.Equal(t, "10", r.Form.Get("bid[planets][123][metal]"))
			writeJSON(t, w, map[string]any{"error": false, "message": "Your bid has been accepted."})
		default:
			t.Fatalf("unexpected request %d", len(requests))
		}
	}))
	defer server.Close()

	bot := newV13AuctionTestBot(t, server.URL)
	err := bot.doAuction(0, map[ogame.CelestialID]ogame.Resources{
		123: {Metal: 10},
	})

	require.NoError(t, err)
	assert.Len(t, requests, 3)
}

type httpRequestSnapshot struct {
	Method string
	Query  url.Values
}

func snapshotRequest(r *http.Request) httpRequestSnapshot {
	return httpRequestSnapshot{Method: r.Method, Query: r.URL.Query()}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func newV13AuctionTestBot(t *testing.T, serverURL string) *OGame {
	t.Helper()
	testDevice, err := device.NewBuilder("auction-http-test").
		SetTimezone("America/Los_Angeles").
		SetOsName(device.Windows).
		SetBrowserName(device.Chrome).
		SetMemory(8).
		SetHardwareConcurrency(4).
		ScreenColorDepth(24).
		SetScreenWidth(1920).
		SetScreenHeight(1080).
		Build()
	require.NoError(t, err)

	bot, err := NewNoLogin(testDevice, "", "", "", "en")
	require.NoError(t, err)
	bot.cache.serverURL = serverURL
	bot.cache.serverData.Version = "13.0.0"
	bot.extractor = v13_0_0.NewExtractor()
	bot.isLoggedInAtom.Store(true)
	bot.isConnectedAtom.Store(true)
	return bot
}

func v13AuctionHTML(playerBid string) string {
	return `<div class="image_140px"><img alt="Energy Booster Bronze"><a class="detail_button" ref="auction-ref"></a></div>
<p class="auction_info"><span>approx. 5m</span></p>
<div class="currentSum">2,000</div>
<div class="numberOfBids">2</div>
<div class="currentPlayer">Player</div>
<span class="level amount">1</span>
<table class="table_ressources_sum">
  <tr><td class="auctionInfo js_price">3,000</td><td class="auctionInfo js_deficit">0</td></tr>
</table>
<script>
var playerBid = ` + playerBid + `;
var auctionFinished = false;
var multiplier = {"metal":1,"crystal":1.5,"deuterium":3,"honor":100};
var planetResources = {"123":{"input":{"metal":100,"crystal":200,"deuterium":300},"output":{"metal":0,"crystal":0,"deuterium":0},"isMoon":false,"name":"Home","otherPlanetId":null}};
</script>`
}
