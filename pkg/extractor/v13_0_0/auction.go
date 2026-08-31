package v13_0_0

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/alaingilbert/ogame/pkg/utils"
)

// extractAuctionFromDoc parses the auction markup returned by the v13 trader
// endpoint. In v13 the request token is returned in the JSON envelope by the
// wrapper, not in this HTML, so Auction.Token intentionally remains empty.
func extractAuctionFromDoc(doc *goquery.Document) (ogame.Auction, error) {
	auction := ogame.Auction{}
	auction.HasFinished = false

	if nextAuction := doc.Find("#nextAuction"); nextAuction.Size() > 0 {
		auction.Endtime = utils.DoParseI64(nextAuction.Text())
		auction.HasFinished = true
	} else {
		endAtApprox := doc.Find("p.auction_info span").Text()
		match := regexp.MustCompile(`[^\d]*(\d+)`).FindStringSubmatch(endAtApprox)
		if len(match) != 2 {
			return ogame.Auction{}, errors.New("failed to find end time approx")
		}
		endTimeMinutes, err := utils.ParseI64(match[1])
		if err != nil {
			return ogame.Auction{}, errors.New("invalid end time approx: " + err.Error())
		}
		auction.Endtime = endTimeMinutes * 60
	}

	auction.HighestBidder = strings.TrimSpace(doc.Find("a.currentPlayer").Text())
	auction.HighestBidderUserID = utils.DoParseI64(doc.Find("a.currentPlayer").AttrOr("data-player-id", ""))
	auction.NumBids = utils.DoParseI64(doc.Find("div.numberOfBids").Text())
	auction.CurrentBid = utils.ParseInt(doc.Find("div.currentSum").Text())
	auction.Inventory = utils.DoParseI64(doc.Find("span.level.amount").Text())
	auction.Ref = doc.Find("a.detail_button").First().AttrOr("ref", "")
	auction.CurrentItem = strings.ToLower(strings.TrimSpace(doc.Find("img").First().AttrOr("alt", "")))
	auction.CurrentItemLong = strings.ToLower(doc.Find("div.image_140px").First().Find("a").First().AttrOr("title", ""))

	script := doc.Text()
	value, err := auctionScriptValue(script, "multiplier")
	if err != nil {
		return ogame.Auction{}, err
	}
	if err := json.Unmarshal([]byte(value), &auction.ResourceMultiplier); err != nil {
		return ogame.Auction{}, errors.New("failed to json parse auction multiplier: " + err.Error())
	}

	value, err = auctionScriptValue(script, "planetResources")
	if err != nil {
		return ogame.Auction{}, err
	}
	if err := json.Unmarshal([]byte(value), &auction.Resources); err != nil {
		return ogame.Auction{}, errors.New("failed to json unmarshal planetResources: " + err.Error())
	}

	value, err = auctionScriptValue(script, "playerBid")
	if err != nil {
		return ogame.Auction{}, errors.New("failed to get playerBid")
	}
	if value != "null" && value != "false" {
		auction.AlreadyBid = utils.DoParseI64(value)
	}

	auction.MinimumBid = utils.ParseInt(doc.Find("table.table_ressources_sum tr td.auctionInfo.js_price").Text())
	auction.DeficitBid = utils.ParseInt(doc.Find("table.table_ressources_sum tr td.auctionInfo.js_deficit").Text())

	return auction, nil
}

func auctionScriptValue(script, name string) (string, error) {
	match := regexp.MustCompile(`var\s+` + regexp.QuoteMeta(name) + `\s*=\s*([^;]+);`).FindStringSubmatch(script)
	if len(match) != 2 {
		return "", errors.New("failed to find " + name)
	}
	return strings.TrimSpace(match[1]), nil
}

// ExtractAuction overrides the legacy parser embedded through v12. The v13
// page has no auctioneerToken variable and may expose playerBid as null.
func (e *Extractor) ExtractAuction(pageHTML []byte) (ogame.Auction, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(pageHTML))
	if err != nil {
		return ogame.Auction{}, err
	}
	return extractAuctionFromDoc(doc)
}
