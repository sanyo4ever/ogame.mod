package v13_0_0

import (
	"testing"

	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractAuctionV13UsesTheAjaxTokenFromTheWrapper(t *testing.T) {
	auction, err := NewExtractor().ExtractAuction([]byte(v13AuctionHTML("null")))

	require.NoError(t, err)
	assert.Equal(t, int64(0), auction.AlreadyBid)
	assert.Empty(t, auction.Token)
	assert.Equal(t, "energy booster bronze", auction.CurrentItem)
	assert.Equal(t, int64(2000), auction.CurrentBid)
	assert.Equal(t, int64(3000), auction.MinimumBid)
	assert.Equal(t, ogame.Resources{Metal: 100, Crystal: 200, Deuterium: 300}, auctionResource(auction, "123"))
}

func TestExtractAuctionV13AcceptsAnExistingPlayerBid(t *testing.T) {
	auction, err := NewExtractor().ExtractAuction([]byte(v13AuctionHTML("1200")))

	require.NoError(t, err)
	assert.Equal(t, int64(1200), auction.AlreadyBid)
}

func TestExtractAllResourcesV13ReadsTheAuctionPlanetResourcesScript(t *testing.T) {
	resources, err := NewExtractor().ExtractAllResources([]byte(v13AuctionHTML("null")))

	require.NoError(t, err)
	assert.Equal(t, ogame.Resources{Metal: 100, Crystal: 200, Deuterium: 300}, resources[123])
}

func auctionResource(auction ogame.Auction, key string) ogame.Resources {
	resource, ok := auction.Resources[key].(map[string]any)
	if !ok {
		return ogame.Resources{}
	}
	input, ok := resource["input"].(map[string]any)
	if !ok {
		return ogame.Resources{}
	}
	return ogame.Resources{
		Metal:     int64(input["metal"].(float64)),
		Crystal:   int64(input["crystal"].(float64)),
		Deuterium: int64(input["deuterium"].(float64)),
	}
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
