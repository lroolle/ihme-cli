package api

const (
	AuthOrigin      = "https://idmsa.apple.com"
	AuthEndpoint    = AuthOrigin + "/appleauth/auth"
	SetupEndpoint   = "https://setup.icloud.com/setup/ws/1"
	SetupEndpointCN = "https://setup.icloud.com.cn/setup/ws/1"

	WidgetKey = "d39ba9916b7251055b22c7f910e2ea796ee65e98b2ddecea8f5dde8d9d1a815d"

	ClientBuildNumber     = "2602Build17"
	ClientMasteringNumber = "2602Build17"
)

var authHeaders = map[string]string{
	"Accept":                           "application/json",
	"Content-Type":                     "application/json",
	"X-Apple-OAuth-Client-Id":          WidgetKey,
	"X-Apple-OAuth-Client-Type":        "firstPartyAuth",
	"X-Apple-OAuth-Redirect-URI":       "https://www.icloud.com",
	"X-Apple-OAuth-Require-Grant-Code": "true",
	"X-Apple-OAuth-Response-Mode":      "web_message",
	"X-Apple-OAuth-Response-Type":      "code",
	"X-Apple-Widget-Key":               WidgetKey,
	"X-Requested-With":                 "XMLHttpRequest",
	"X-Apple-I-Require-UE":             "true",
	"X-Apple-Mandate-Security-Upgrade": "0",
	"X-Apple-Offer-Security-Upgrade":   "1",
	"Origin":                           "https://idmsa.apple.com",
	"Referer":                          "https://idmsa.apple.com/",
}

var serviceHeaders = map[string]string{
	"Accept":       "application/json",
	"Content-Type": "application/json",
	"Origin":       "https://www.icloud.com",
	"Referer":      "https://www.icloud.com/",
}
