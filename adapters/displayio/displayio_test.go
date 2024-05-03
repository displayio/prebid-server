package displayio

import (
	"testing"

	"github.com/prebid/prebid-server/v2/config"
	"github.com/prebid/prebid-server/v2/openrtb_ext"

	"github.com/prebid/prebid-server/v2/adapters/adapterstest"
)

func TestJsonSamples(t *testing.T) {
	bidder, buildErr := Builder(openrtb_ext.BidderDisplayio,
		config.Adapter{Endpoint: "http://test.com/pserver"},
		config.Server{ExternalUrl: "http://test.com/pserver", GvlID: 1, DataCenter: "2"},
	)

	if buildErr != nil {
		t.Fatalf("Builder returned unexpected error %v", buildErr)
	}

	adapterstest.RunJSONBidderTest(t, "displayiotest", bidder)
}
