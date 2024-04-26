package displayio

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v2/adapters"
	"github.com/prebid/prebid-server/v2/config"
	"github.com/prebid/prebid-server/v2/errortypes"
	"github.com/prebid/prebid-server/v2/macros"
	"github.com/prebid/prebid-server/v2/openrtb_ext"
	"net/http"
	"text/template"
)

type DisplayioAdapter struct {
	endpoint *template.Template
}

type ReqExt struct {
	DioExt ReqDioExt `json:"dio"`
}

type ReqDioExt struct {
	UserSession string `json:"userSession,omitempty"`
	PlacementId string `json:"placementId"`
	InventoryId string `json:"inventoryId"`
}

func (adapter *DisplayioAdapter) MakeRequests(request *openrtb2.BidRequest, requestInfo *adapters.ExtraRequestInfo) ([]*adapters.RequestData, []error) {
	headers := http.Header{}
	headers.Add("Content-Type", "application/json;charset=utf-8")
	headers.Add("Accept", "application/json")
	headers.Add("x-openrtb-version", "2.5")

	var reqExt map[string]interface{}
	var dioExt ReqDioExt

	if len(request.Imp) == 0 {
		return nil, []error{&errortypes.BadInput{Message: "No impression in the bid request"}}
	}

	impressions := request.Imp
	result := make([]*adapters.RequestData, 0, len(impressions))
	errs := make([]error, 0, len(impressions))

	for i, impression := range impressions {
		if impression.Banner == nil && impression.Video == nil {
			errs = append(errs, &errortypes.BadInput{
				Message: "Display.io only supports banner or video ads",
			})
			continue
		}

		if impression.BidFloor == 0 {
			errs = append(errs, &errortypes.BadInput{
				Message: "BidFloor cannot be zero",
			})
			continue
		}

		if impression.BidFloorCur == "" {
			errs = append(errs, &errortypes.BadInput{
				Message: "BidFloorCur should be specified",
			})
			continue
		}

		if impression.BidFloorCur != "USD" {
			convertedValue, err := requestInfo.ConvertCurrency(impression.BidFloor, impression.BidFloorCur, "USD")

			if err != nil {
				errs = append(errs, err)
				continue
			}

			impression.BidFloorCur = "USD"
			impression.BidFloor = convertedValue
		}

		if len(impression.Ext) == 0 {
			errs = append(errs, errors.New("impression extensions required"))
			continue
		}

		var bidderExt adapters.ExtImpBidder
		err := json.Unmarshal(impression.Ext, &bidderExt)

		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(bidderExt.Bidder) == 0 {
			errs = append(errs, errors.New("bidder required"))
			continue
		}
		var impressionExt openrtb_ext.ExtImpDisplayio
		err = json.Unmarshal(bidderExt.Bidder, &impressionExt)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if impressionExt.PublisherId == "" {
			errs = append(errs, errors.New("publisherId required"))
			continue
		}
		if impressionExt.InventoryId == "" {
			errs = append(errs, errors.New("inventoryId required"))
			continue
		}
		if impressionExt.PlacementId == "" {
			errs = append(errs, errors.New("placementId required"))
			continue
		}

		if i == 0 {
			dioExt = ReqDioExt{PlacementId: impressionExt.PlacementId, InventoryId: impressionExt.InventoryId}

			err = json.Unmarshal(request.Ext, &reqExt)
			if err != nil {
				reqExt = make(map[string]interface{})
			}

			reqExt["displayio"] = dioExt

			request.Ext, err = json.Marshal(reqExt)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		}

		request.Imp = impressions[i : i+1]
		body, err := json.Marshal(request)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		url, err := adapter.buildEndpointURL(&impressionExt)
		if err != nil {
			return nil, []error{err}
		}

		result = append(result, &adapters.RequestData{
			Method:  "POST",
			Uri:     url,
			Body:    body,
			Headers: headers,
			ImpIDs:  openrtb_ext.GetImpIDs(request.Imp),
		})
	}

	request.Imp = impressions

	if len(result) == 0 {
		return nil, errs
	}
	return result, errs
}

// MakeBids translates Displayio bid response to prebid-server specific format
func (adapter *DisplayioAdapter) MakeBids(internalRequest *openrtb2.BidRequest, _ *adapters.RequestData, response *adapters.ResponseData) (*adapters.BidderResponse, []error) {

	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if response.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("Unexpected http status code: %d", response.StatusCode)
		return nil, []error{&errortypes.BadServerResponse{Message: msg}}
	}

	var bidResp openrtb2.BidResponse

	if err := json.Unmarshal(response.Body, &bidResp); err != nil {
		msg := fmt.Sprintf("Bad server response: %d", err)
		return nil, []error{&errortypes.BadServerResponse{Message: msg}}
	}

	if len(bidResp.SeatBid) != 1 {
		msg := fmt.Sprintf("Invalid SeatBids count: %d", len(bidResp.SeatBid))
		return nil, []error{&errortypes.BadServerResponse{Message: msg}}
	}

	bid := bidResp.SeatBid[0].Bid[0]
	bidResponse := adapters.NewBidderResponseWithBidsCapacity(1)

	bidResponse.Bids = append(bidResponse.Bids, &adapters.TypedBid{
		Bid:     &bid,
		BidType: GetMediaTypeForImp(bid.ImpID, internalRequest.Imp),
	})

	return bidResponse, nil
}

func Builder(_ openrtb_ext.BidderName, config config.Adapter, _ config.Server) (adapters.Bidder, error) {
	endpoint, err := template.New("endpointTemplate").Parse(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("unable to parse endpoint url template: %v", err)
	}

	bidder := &DisplayioAdapter{
		endpoint: endpoint,
	}
	return bidder, nil
}

const UndefinedMediaType = openrtb_ext.BidType("")

func GetMediaTypeForImp(impID string, imps []openrtb2.Imp) openrtb_ext.BidType {
	var bidType = UndefinedMediaType
	for _, impression := range imps {
		if impression.ID != impID {
			continue
		}
		switch {
		case impression.Banner != nil:
			bidType = openrtb_ext.BidTypeBanner
		case impression.Video != nil:
			bidType = openrtb_ext.BidTypeVideo
		case impression.Native != nil:
			bidType = openrtb_ext.BidTypeNative
		case impression.Audio != nil:
			bidType = openrtb_ext.BidTypeAudio
		}
		break
	}
	return bidType
}

func (adapter *DisplayioAdapter) buildEndpointURL(params *openrtb_ext.ExtImpDisplayio) (string, error) {
	endpointParams := macros.EndpointTemplateParams{PublisherID: params.PublisherId}
	return macros.ResolveMacros(adapter.endpoint, endpointParams)
}
