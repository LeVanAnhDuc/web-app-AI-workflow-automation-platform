package credentials

import (
	"sort"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// Registry holds the credential types a build knows about, in a stable order so
// the editor's list does not shuffle between requests.
type Registry struct {
	byType map[string]domain.CredentialType
	order  []string
}

// NewRegistry builds a registry from the given types.
func NewRegistry(types ...domain.CredentialType) *Registry {
	r := &Registry{byType: make(map[string]domain.CredentialType, len(types))}
	for _, t := range types {
		r.Register(t)
	}
	return r
}

// Register adds or replaces a type.
func (r *Registry) Register(t domain.CredentialType) {
	if _, seen := r.byType[t.Type]; !seen {
		r.order = append(r.order, t.Type)
	}
	r.byType[t.Type] = t
}

// Get looks a type up.
func (r *Registry) Get(name string) (domain.CredentialType, bool) {
	if r == nil {
		return domain.CredentialType{}, false
	}
	t, ok := r.byType[name]
	return t, ok
}

// All returns every type in registration order.
func (r *Registry) All() []domain.CredentialType {
	if r == nil {
		return nil
	}
	out := make([]domain.CredentialType, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.byType[name])
	}
	return out
}

// Names lists the registered type ids, sorted, for error messages.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := append([]string(nil), r.order...)
	sort.Strings(out)
	return out
}

/* ---------------------------------------------------------------------------
   The built-in types.

   Each is a declaration. Adding an authentication scheme means adding one of
   these — the editor's form, the API's validation and the request signing all
   read from it.
   --------------------------------------------------------------------------- */

// DefaultRegistry returns the credential types this build ships with.
func DefaultRegistry() *Registry {
	return NewRegistry(
		HTTPBasic(),
		HTTPHeader(),
		BearerToken(),
		AnthropicAPI(),
		SlackOAuth2(),
		GoogleOAuth2(GmailScopes, "gmailOAuth2", "Gmail",
			"Sends and reads mail as the connected Google account."),
		GoogleOAuth2(SheetsScopes, "googleSheetsOAuth2", "Google Sheets",
			"Reads and writes spreadsheets as the connected Google account."),
	)
}

// HTTPBasic is username and password, sent as an Authorization header.
func HTTPBasic() domain.CredentialType {
	return domain.CredentialType{
		Type:        "httpBasicAuth",
		Name:        "HTTP Basic Auth",
		Icon:        "lock",
		Description: "A username and password, sent on every request.",
		Auth:        domain.AuthBasic,
		Fields: []domain.CredentialField{
			{Name: "username", Label: "Username", Type: "string", Required: true},
			{Name: "password", Label: "Password", Type: "password", Required: true, Secret: true},
		},
	}
}

// HTTPHeader is an API key in a header or query parameter — the shape most
// smaller APIs use, and the one people most often have to configure by hand.
func HTTPHeader() domain.CredentialType {
	return domain.CredentialType{
		Type:        "httpHeaderAuth",
		Name:        "API Key",
		Icon:        "lock",
		Description: "A key sent in a header or a query parameter.",
		Auth:        domain.AuthAPIKey,
		Fields: []domain.CredentialField{
			{Name: "apiKey", Label: "API Key", Type: "password", Required: true, Secret: true},
			{
				Name: "in", Label: "Send it in", Type: "select", Default: "header",
				Options: []domain.FieldOption{
					{Label: "A header", Value: "header"},
					{Label: "A query parameter", Value: "query"},
				},
			},
			{
				Name: "headerName", Label: "Name", Type: "string", Default: "Authorization",
				Placeholder: "X-API-Key",
				Description: "The header or parameter name the API expects.",
			},
			{
				Name: "prefix", Label: "Prefix", Type: "string",
				Placeholder: "Bearer",
				Description: "Put in front of the key, with a space. Leave empty to send the key alone.",
			},
		},
	}
}

// BearerToken is a static token, for APIs that issue long-lived ones.
func BearerToken() domain.CredentialType {
	return domain.CredentialType{
		Type:        "bearerAuth",
		Name:        "Bearer Token",
		Icon:        "lock",
		Description: "A long-lived token sent as Authorization: Bearer.",
		Auth:        domain.AuthBearer,
		Fields: []domain.CredentialField{
			{Name: "token", Label: "Token", Type: "password", Required: true, Secret: true},
		},
	}
}

// AnthropicAPI backs the AI nodes. Phase 2 read this key from the environment;
// storing it here is what lets a workspace bring its own.
func AnthropicAPI() domain.CredentialType {
	return domain.CredentialType{
		Type:        "anthropicApi",
		Name:        "Anthropic API",
		Icon:        "sparkle",
		Description: "An Anthropic API key for the LLM and AI Agent nodes.",
		Auth:        domain.AuthAPIKey,
		Fields: []domain.CredentialField{
			{
				Name: "apiKey", Label: "API Key", Type: "password", Required: true, Secret: true,
				Placeholder: "sk-ant-…",
			},
			{Name: "in", Label: "", Type: "hidden", Default: "header"},
			{Name: "headerName", Label: "", Type: "hidden", Default: "x-api-key"},
		},
	}
}

// SlackOAuth2 connects a Slack workspace.
func SlackOAuth2() domain.CredentialType {
	return domain.CredentialType{
		Type:        "slackOAuth2",
		Name:        "Slack",
		Icon:        "slack",
		Description: "Posts and reads messages as the connected Slack app.",
		Auth:        domain.AuthOAuth2,
		TestURL:     "https://slack.com/api/auth.test",
		Fields:      oauthClientFields("https://api.slack.com/apps"),
		OAuth2: &domain.OAuth2Config{
			AuthorizeURL: "https://slack.com/oauth/v2/authorize",
			TokenURL:     "https://slack.com/api/oauth.v2.access",
			Scopes:       []string{"chat:write", "channels:read", "channels:history"},
		},
	}
}

// The Google scope sets, kept as names so a connector and its credential cannot
// drift apart.
var (
	GmailScopes = []string{
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/gmail.send",
	}
	SheetsScopes = []string{
		"https://www.googleapis.com/auth/spreadsheets",
	}
)

// GoogleOAuth2 builds a Google credential type for one product's scopes.
//
// access_type=offline and prompt=consent are not optional decoration: without
// them Google returns no refresh token on a repeat authorisation, and the
// credential silently stops working an hour later.
func GoogleOAuth2(scopes []string, id, name, description string) domain.CredentialType {
	return domain.CredentialType{
		Type:        id,
		Name:        name,
		Icon:        "google",
		Description: description,
		Auth:        domain.AuthOAuth2,
		TestURL:     "https://www.googleapis.com/oauth2/v3/userinfo",
		Fields:      oauthClientFields("https://console.cloud.google.com/apis/credentials"),
		OAuth2: &domain.OAuth2Config{
			AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			Scopes:       scopes,
			AuthParams: map[string]string{
				"access_type": "offline",
				"prompt":      "consent",
			},
		},
	}
}

// oauthClientFields are the two fields every OAuth type needs from the user,
// plus the scope override and a pointer to where the app is registered.
func oauthClientFields(consoleURL string) []domain.CredentialField {
	return []domain.CredentialField{
		{
			Name: "notice", Label: "", Type: "notice",
			Description: "Register an app with the provider, add this instance's redirect URL " +
				"to it, then paste the client ID and secret below. Create the app at " + consoleURL + ".",
		},
		{Name: "clientId", Label: "Client ID", Type: "string", Required: true},
		{Name: "clientSecret", Label: "Client Secret", Type: "password", Required: true, Secret: true},
		{
			Name: "scopes", Label: "Scopes", Type: "string",
			Description: "Space-separated. Leave empty to request the defaults for this type.",
		},
	}
}
