package domain

import "time"

/* ---------------------------------------------------------------------------
   Credentials.

   A credential is a named, encrypted bag of fields belonging to a workspace.
   Its *shape* comes from a CredentialType, which is a descriptor in the same
   spirit as a node's: the editor renders the form from it, so adding an
   authentication scheme is a declaration rather than a screen.

   The decrypted values never leave the server. What the API returns is which
   fields are set, never what they are set to.
   --------------------------------------------------------------------------- */

// AuthKind is how a credential attaches itself to an outgoing request.
type AuthKind string

const (
	// AuthNone is a credential that carries data but attaches nothing itself,
	// such as an API key a node passes in a body rather than a header.
	AuthNone AuthKind = "none"

	AuthAPIKey AuthKind = "apiKey" // a key in a named header or query parameter
	AuthBasic  AuthKind = "basic"  // username and password
	AuthBearer AuthKind = "bearer" // a static token in Authorization
	AuthOAuth2 AuthKind = "oauth2" // an authorisation-code grant with refresh
)

// OAuth2Config is everything needed to run the authorisation-code dance for one
// provider. It is part of the type, not of the credential: the URLs and scopes
// are the same for every account, while the client id and tokens are not.
type OAuth2Config struct {
	AuthorizeURL string   `json:"authorizeUrl"`
	TokenURL     string   `json:"tokenUrl"`
	Scopes       []string `json:"scopes,omitempty"`

	// AuthParams are appended to the authorisation URL. Google needs
	// access_type=offline and prompt=consent here, or it returns no refresh
	// token on a repeat authorisation and the credential silently expires.
	AuthParams map[string]string `json:"authParams,omitempty"`

	// UsePKCE adds a code challenge. Harmless where unsupported, and required
	// by providers that reject a plain code exchange.
	UsePKCE bool `json:"usePkce,omitempty"`
}

// CredentialField is one input on a credential's form. It deliberately mirrors
// a node's parameter spec rather than inventing a second vocabulary.
type CredentialField struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"` // string | password | select | notice
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Description string `json:"description,omitempty"`

	// Secret marks a value the API must never return once stored. The editor
	// shows it as a password field and, on edit, as "unchanged".
	Secret bool `json:"secret,omitempty"`

	Options []FieldOption `json:"options,omitempty"`
}

// FieldOption is one choice of a select field.
type FieldOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// CredentialType is the descriptor for one kind of credential.
type CredentialType struct {
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Icon        string            `json:"icon"`
	Description string            `json:"description,omitempty"`
	Auth        AuthKind          `json:"auth"`
	Fields      []CredentialField `json:"fields"`
	OAuth2      *OAuth2Config     `json:"oauth2,omitempty"`

	// TestURL is a cheap authenticated endpoint used by "Test connection". An
	// empty value means the type cannot be tested, and the UI says so rather
	// than offering a button that does nothing.
	TestURL string `json:"testUrl,omitempty"`
}

// NeedsOAuth reports whether this type requires the authorisation dance.
func (t CredentialType) NeedsOAuth() bool {
	return t.Auth == AuthOAuth2 && t.OAuth2 != nil
}

// SecretFields lists the field names whose values must never be returned.
func (t CredentialType) SecretFields() []string {
	var out []string
	for _, f := range t.Fields {
		if f.Secret {
			out = append(out, f.Name)
		}
	}
	return out
}

// Field finds a field by name.
func (t CredentialType) Field(name string) (CredentialField, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return CredentialField{}, false
}

/* --- the stored credential ------------------------------------------------ */

// OAuth2Tokens is the sealed half of an OAuth credential. It lives inside the
// encrypted blob alongside the user's own fields, never in a column.
type OAuth2Tokens struct {
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	TokenType    string    `json:"tokenType,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitzero"`
	Scope        string    `json:"scope,omitempty"`
}

// Expired reports whether the access token is past its life, with a margin so a
// token that dies mid-request is refreshed before it is used rather than after
// it fails.
func (t OAuth2Tokens) Expired(now time.Time) bool {
	if t.AccessToken == "" {
		return true
	}
	if t.ExpiresAt.IsZero() {
		// A provider that returns no expiry is treated as long-lived; a failed
		// call will surface the problem plainly enough.
		return false
	}
	return !now.Add(60 * time.Second).Before(t.ExpiresAt)
}

// Connected reports whether the authorisation dance has completed.
func (t OAuth2Tokens) Connected() bool { return t.AccessToken != "" }

// CredentialData is the decrypted contents of one credential: the fields the
// user filled in, plus the tokens for an OAuth type.
type CredentialData struct {
	Fields map[string]string `json:"fields"`
	OAuth  *OAuth2Tokens     `json:"oauth,omitempty"`
}

// Field reads one value, tolerating a nil map.
func (d CredentialData) Field(name string) string {
	if d.Fields == nil {
		return ""
	}
	return d.Fields[name]
}

// CredentialSummary is what the API returns: everything about a credential
// except what it is worth protecting.
type CredentialSummary struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Type        string    `json:"type"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	// SetFields names the fields that hold a value, so the editor can show
	// "unchanged" instead of an empty box that looks like data loss.
	SetFields []string `json:"setFields"`

	// Connected is meaningful only for an OAuth type: it reports whether the
	// authorisation completed.
	Connected bool `json:"connected"`

	// ExpiresAt is the access token's expiry, so the UI can warn before a
	// scheduled workflow starts failing at 3am.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`

	// UsedByCount is how many nodes across the workspace reference it, which is
	// what makes a delete confirmation honest.
	UsedByCount int `json:"usedByCount"`
}

// NewCredential is the input of a create.
type NewCredential struct {
	WorkspaceID string
	Type        string
	Name        string
	Data        CredentialData
}

// CredentialPatch updates a credential. A nil field map leaves the stored data
// alone; a present one is merged, so omitting a secret keeps it rather than
// blanking it.
type CredentialPatch struct {
	Name   *string
	Fields map[string]string
	OAuth  *OAuth2Tokens
}
