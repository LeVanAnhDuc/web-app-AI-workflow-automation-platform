package connectors

import (
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// Gmail declares the Gmail API for the authorised account.
//
// On sending: the API takes one field, "raw", holding a base64url-encoded
// RFC 2822 message. This connector exposes that field as it is instead of
// offering To / Subject / Body and assembling MIME behind the scenes. The
// alternative would mean per-operation Go code inside a package whose whole
// point is that a connector is a declaration — and a To/Subject/Body form that
// quietly needed a hand-built MIME document would be a worse lie than an honest
// field with instructions. The description says exactly what to put in it.
func Gmail() Connector {
	return Connector{
		ID:          "gmail",
		Name:        "Gmail",
		Icon:        "mail",
		Description: "Reads and sends mail as the connected Google account.",
		Credential:  "gmailOAuth2",
		BaseURL:     "https://gmail.googleapis.com/gmail/v1",
		Operations: []Operation{
			{
				ID:   "listMessages",
				Name: "List messages",
				Description: "Searches the mailbox and returns one item per match. " +
					"Each item carries only id and threadId — follow it with Get Message for the content.",
				Method: "GET",
				Path:   "/users/me/messages",
				Params: []nodes.ParamSpec{
					{
						Name:               "q",
						Label:              "Search",
						Type:               nodes.ParamString,
						Placeholder:        "is:unread from:alerts@example.com",
						Description:        "Gmail search query, the same syntax as the search box. Empty matches everything.",
						SupportsExpression: true,
					},
					{
						Name:               "maxResults",
						Label:              "Max Results",
						Type:               nodes.ParamNumber,
						Default:            25,
						Description:        "How many messages to return in one page.",
						SupportsExpression: true,
					},
				},
				Query: map[string]string{
					"q":          "{q}",
					"maxResults": "{maxResults}",
				},
				ItemsPath: "messages",
			},
			{
				ID:          "getMessage",
				Name:        "Get a message",
				Description: "Fetches one message by id and returns it as a single item.",
				Method:      "GET",
				Path:        "/users/me/messages/{messageId}",
				Params: []nodes.ParamSpec{
					{
						Name:               "messageId",
						Label:              "Message ID",
						Type:               nodes.ParamString,
						Required:           true,
						Placeholder:        "{{ $json.id }}",
						Description:        "The id List Messages returned.",
						SupportsExpression: true,
					},
					{
						Name:    "format",
						Label:   "Format",
						Type:    nodes.ParamSelect,
						Default: "full",
						Options: []nodes.ParamOption{
							{Label: "Full — headers and parsed body", Value: "full"},
							{Label: "Metadata — headers only", Value: "metadata"},
							{Label: "Minimal — ids and labels only", Value: "minimal"},
							{Label: "Raw — the whole RFC 2822 message, base64url", Value: "raw"},
						},
						Description: "How much of the message to return.",
					},
				},
				Query: map[string]string{"format": "{format}"},
			},
			{
				ID:          "sendMessage",
				Name:        "Send a message",
				Description: "Sends an already-assembled RFC 2822 message.",
				Method:      "POST",
				Path:        "/users/me/messages/send",
				Params: []nodes.ParamSpec{
					{
						Name:        "raw",
						Label:       "Raw Message",
						Type:        nodes.ParamString,
						Required:    true,
						Placeholder: "{{ $json.raw }}",
						Description: "The complete RFC 2822 message, base64url-encoded with no padding — " +
							"the only shape this endpoint accepts. Build it in a Code node, for example: " +
							"btoa('To: a@b.com\\r\\nSubject: Hi\\r\\n\\r\\nBody')" +
							".replace(/\\+/g,'-').replace(/\\//g,'_').replace(/=+$/,'')",
						SupportsExpression: true,
					},
				},
				Body: `{"raw":{raw}}`,
			},
		},
	}
}
