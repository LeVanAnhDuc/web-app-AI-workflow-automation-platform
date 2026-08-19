// Package credentials owns everything about a stored secret: the registry of
// credential types, sealing and unsealing, attaching a credential to an outgoing
// request, and keeping an OAuth token fresh.
//
// It is the only package that holds the encryption key. The store handles a
// sealed blob it cannot read; the API returns metadata it never decrypts; nodes
// receive an already-authenticated request. Narrowing the blast radius to one
// package is the point.
package credentials

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/crypto"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// Store is the persistence this package needs. *store.Store satisfies it.
type Store interface {
	CreateCredential(ctx context.Context, workspaceID, credType, name string, sealed []byte) (domain.Credential, error)
	Credential(ctx context.Context, workspaceID, id string) (domain.Credential, error)
	CredentialSealed(ctx context.Context, workspaceID, id string) (domain.Credential, []byte, error)
	ListCredentials(ctx context.Context, workspaceID, credType string) ([]domain.Credential, error)
	UpdateCredential(ctx context.Context, workspaceID, id string, name *string, sealed []byte) (domain.Credential, error)
	DeleteCredential(ctx context.Context, workspaceID, id string) error
	CredentialUsage(ctx context.Context, workspaceID string) (map[string]int, error)
	TouchCredential(ctx context.Context, workspaceID, id string, sealed []byte, at time.Time) error
}

// ErrUnknownType is returned for a credential type no build knows about.
type ErrUnknownType struct{ Type string }

func (e *ErrUnknownType) Error() string {
	return fmt.Sprintf("unknown credential type %q", e.Type)
}

// ErrNotConnected means an OAuth credential exists but was never authorised.
type ErrNotConnected struct{ Name string }

func (e *ErrNotConnected) Error() string {
	return fmt.Sprintf("the credential %q has not been connected yet", e.Name)
}

// Service is the credential façade.
type Service struct {
	store  Store
	sealer *crypto.Sealer
	types  *Registry
	client *http.Client
	now    func() time.Time

	// In-flight OAuth authorisations, built on first use so a Service that
	// never runs the dance carries no state.
	stateOnce sync.Once
	state     *stateStore
}

// Options configures a Service. Everything has a sane default except the key.
type Options struct {
	Types      *Registry
	HTTPClient *http.Client
	Now        func() time.Time
}

// New builds a Service. The key must be exactly the sealer's key length.
func New(st Store, key []byte, opts Options) (*Service, error) {
	sealer, err := crypto.NewSealer(key)
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}
	s := &Service{
		store:  st,
		sealer: sealer,
		types:  opts.Types,
		client: opts.HTTPClient,
		now:    opts.Now,
	}
	if s.types == nil {
		s.types = DefaultRegistry()
	}
	if s.client == nil {
		s.client = &http.Client{Timeout: 30 * time.Second}
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s, nil
}

// Types exposes the registry, so the API can serve the descriptors the editor
// renders its credential forms from.
func (s *Service) Types() *Registry { return s.types }

/* --- reading and writing -------------------------------------------------- */

// Create seals a credential's data and stores it.
func (s *Service) Create(ctx context.Context, in domain.NewCredential) (domain.CredentialSummary, error) {
	ct, ok := s.types.Get(in.Type)
	if !ok {
		return domain.CredentialSummary{}, &ErrUnknownType{Type: in.Type}
	}
	if err := validate(ct, in.Data.Fields, true); err != nil {
		return domain.CredentialSummary{}, err
	}

	sealed, err := s.sealer.SealJSON(in.Data)
	if err != nil {
		return domain.CredentialSummary{}, fmt.Errorf("credentials: seal: %w", err)
	}
	rec, err := s.store.CreateCredential(ctx, in.WorkspaceID, in.Type, strings.TrimSpace(in.Name), sealed)
	if err != nil {
		return domain.CredentialSummary{}, err
	}
	return s.summarise(rec, in.Data, 0), nil
}

// Update merges a patch into the stored data. Fields the patch omits keep their
// stored values, which is what lets an edit form show "unchanged" for a secret
// instead of forcing the user to retype it.
func (s *Service) Update(ctx context.Context, workspaceID, id string, patch domain.CredentialPatch) (domain.CredentialSummary, error) {
	rec, data, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return domain.CredentialSummary{}, err
	}
	ct, ok := s.types.Get(rec.Type)
	if !ok {
		return domain.CredentialSummary{}, &ErrUnknownType{Type: rec.Type}
	}

	var sealed []byte
	if patch.Fields != nil || patch.OAuth != nil {
		if data.Fields == nil {
			data.Fields = map[string]string{}
		}
		for k, v := range patch.Fields {
			// An empty string for a secret means "leave it alone": the editor
			// cannot show the stored value, so it cannot round-trip it either.
			if f, known := ct.Field(k); known && f.Secret && v == "" {
				continue
			}
			data.Fields[k] = v
		}
		if patch.OAuth != nil {
			data.OAuth = patch.OAuth
		}
		if err := validate(ct, data.Fields, false); err != nil {
			return domain.CredentialSummary{}, err
		}
		if sealed, err = s.sealer.SealJSON(data); err != nil {
			return domain.CredentialSummary{}, fmt.Errorf("credentials: seal: %w", err)
		}
	}

	var name *string
	if patch.Name != nil {
		trimmed := strings.TrimSpace(*patch.Name)
		name = &trimmed
	}

	updated, err := s.store.UpdateCredential(ctx, workspaceID, id, name, sealed)
	if err != nil {
		return domain.CredentialSummary{}, err
	}
	return s.summarise(updated, data, 0), nil
}

// List returns a workspace's credentials as summaries, never as secrets.
func (s *Service) List(ctx context.Context, workspaceID, credType string) ([]domain.CredentialSummary, error) {
	records, err := s.store.ListCredentials(ctx, workspaceID, credType)
	if err != nil {
		return nil, err
	}
	usage, err := s.store.CredentialUsage(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	out := make([]domain.CredentialSummary, 0, len(records))
	for _, rec := range records {
		// A blob that will not open is a real operational event — a rotated or
		// wrong key — and the list must still render, with the credential
		// visibly unusable rather than missing.
		_, data, err := s.loadSealed(ctx, workspaceID, rec.ID)
		if err != nil {
			out = append(out, domain.CredentialSummary{
				ID: rec.ID, WorkspaceID: rec.WorkspaceID, Type: rec.Type,
				Name: rec.Name, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt,
				UsedByCount: usage[rec.ID],
			})
			continue
		}
		out = append(out, s.summarise(rec, data, usage[rec.ID]))
	}
	return out, nil
}

// Get returns one summary.
func (s *Service) Get(ctx context.Context, workspaceID, id string) (domain.CredentialSummary, error) {
	rec, data, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return domain.CredentialSummary{}, err
	}
	usage, err := s.store.CredentialUsage(ctx, workspaceID)
	if err != nil {
		return domain.CredentialSummary{}, err
	}
	return s.summarise(rec, data, usage[id]), nil
}

// Delete removes a credential.
func (s *Service) Delete(ctx context.Context, workspaceID, id string) error {
	return s.store.DeleteCredential(ctx, workspaceID, id)
}

// load reads and unseals one credential.
func (s *Service) load(ctx context.Context, workspaceID, id string) (domain.Credential, domain.CredentialData, error) {
	return s.loadSealed(ctx, workspaceID, id)
}

func (s *Service) loadSealed(ctx context.Context, workspaceID, id string) (domain.Credential, domain.CredentialData, error) {
	rec, sealed, err := s.store.CredentialSealed(ctx, workspaceID, id)
	if err != nil {
		return domain.Credential{}, domain.CredentialData{}, err
	}
	var data domain.CredentialData
	if len(sealed) > 0 {
		if err := s.sealer.OpenJSON(sealed, &data); err != nil {
			return rec, domain.CredentialData{}, fmt.Errorf(
				"credentials: %q could not be decrypted, which usually means CREDENTIAL_KEY changed: %w",
				rec.Name, err)
		}
	}
	if data.Fields == nil {
		data.Fields = map[string]string{}
	}
	return rec, data, nil
}

func (s *Service) summarise(rec domain.Credential, data domain.CredentialData, usedBy int) domain.CredentialSummary {
	out := domain.CredentialSummary{
		ID:          rec.ID,
		WorkspaceID: rec.WorkspaceID,
		Type:        rec.Type,
		Name:        rec.Name,
		CreatedAt:   rec.CreatedAt,
		UpdatedAt:   rec.UpdatedAt,
		UsedByCount: usedBy,
	}
	for name, value := range data.Fields {
		if value != "" {
			out.SetFields = append(out.SetFields, name)
		}
	}
	// Sorted so the editor's "already set" list does not reorder between loads.
	sort.Strings(out.SetFields)

	if data.OAuth != nil {
		out.Connected = data.OAuth.Connected()
		if !data.OAuth.ExpiresAt.IsZero() {
			expires := data.OAuth.ExpiresAt
			out.ExpiresAt = &expires
		}
	}
	return out
}

// validate checks required fields. On create everything required must be
// present; on update a stored secret may legitimately be absent from the patch.
func validate(ct domain.CredentialType, fields map[string]string, creating bool) error {
	var missing []string
	for _, f := range ct.Fields {
		if !f.Required || f.Type == "notice" {
			continue
		}
		// An OAuth credential's tokens arrive from the dance, not the form, so
		// its client fields are the only ones required up front.
		if strings.TrimSpace(fields[f.Name]) == "" {
			missing = append(missing, f.Label)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	verb := "needs"
	if !creating {
		verb = "still needs"
	}
	return domain.Errorf(domain.ErrCodeValidation,
		"this credential %s: %s", verb, strings.Join(missing, ", "))
}

/* --- using a credential --------------------------------------------------- */

// Resolved is a credential ready for use by a node.
type Resolved struct {
	Type domain.CredentialType
	Data domain.CredentialData
	Name string
}

// Resolve loads a credential, refreshing an expired OAuth token first, and
// returns it ready to attach. The refreshed token is written back so the next
// run starts from a live one.
func (s *Service) Resolve(ctx context.Context, workspaceID, id string) (Resolved, error) {
	rec, data, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return Resolved{}, err
	}
	ct, ok := s.types.Get(rec.Type)
	if !ok {
		return Resolved{}, &ErrUnknownType{Type: rec.Type}
	}

	if ct.NeedsOAuth() {
		if data.OAuth == nil || !data.OAuth.Connected() {
			return Resolved{}, &ErrNotConnected{Name: rec.Name}
		}
		if data.OAuth.Expired(s.now()) {
			refreshed, err := s.refresh(ctx, ct, data)
			if err != nil {
				return Resolved{}, err
			}
			data.OAuth = refreshed
			if err := s.persistTokens(ctx, workspaceID, rec.ID, data); err != nil {
				// The call can still proceed on the token in hand; failing to
				// cache it costs a refresh next time, not this run.
				return Resolved{Type: ct, Data: data, Name: rec.Name}, nil
			}
		}
	}

	return Resolved{Type: ct, Data: data, Name: rec.Name}, nil
}

func (s *Service) persistTokens(ctx context.Context, workspaceID, id string, data domain.CredentialData) error {
	sealed, err := s.sealer.SealJSON(data)
	if err != nil {
		return err
	}
	return s.store.TouchCredential(ctx, workspaceID, id, sealed, s.now())
}

// Apply attaches a resolved credential to an outgoing request.
func Apply(req *http.Request, r Resolved) error {
	switch r.Type.Auth {
	case domain.AuthNone, "":
		return nil

	case domain.AuthBasic:
		req.SetBasicAuth(r.Data.Field("username"), r.Data.Field("password"))
		return nil

	case domain.AuthBearer:
		token := r.Data.Field("token")
		if token == "" {
			return errors.New("this credential has no token")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil

	case domain.AuthAPIKey:
		key := r.Data.Field("apiKey")
		if key == "" {
			return errors.New("this credential has no API key")
		}
		name := r.Data.Field("headerName")
		if name == "" {
			name = "Authorization"
		}
		prefix := r.Data.Field("prefix")
		if r.Data.Field("in") == "query" {
			q := req.URL.Query()
			q.Set(name, key)
			req.URL.RawQuery = q.Encode()
			return nil
		}
		req.Header.Set(name, strings.TrimSpace(prefix+" "+key))
		return nil

	case domain.AuthOAuth2:
		if r.Data.OAuth == nil || !r.Data.OAuth.Connected() {
			return &ErrNotConnected{Name: r.Name}
		}
		tokenType := r.Data.OAuth.TokenType
		if tokenType == "" {
			tokenType = "Bearer"
		}
		req.Header.Set("Authorization", tokenType+" "+r.Data.OAuth.AccessToken)
		return nil

	default:
		return fmt.Errorf("unsupported credential auth %q", r.Type.Auth)
	}
}

// basicAuthHeader builds the client-authentication header for a token request,
// which several providers require instead of client_secret in the body.
func basicAuthHeader(id, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(id+":"+secret))
}
