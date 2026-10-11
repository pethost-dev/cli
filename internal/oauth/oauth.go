// Package oauth is a person's sign-in in their browser: OAuth 2.1's authorization code grant
// with PKCE, for a public client on the person's own computer (pethost.dev/auth.md). It finds
// the panel's endpoints from the panel's address, registers a client, listens on a loopback
// port for the browser's answer, checks that the answer is this sign-in's and the panel's own,
// and trades its code for the token.
//
// It prints and asks nothing: the command that signs in shows the address and waits.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pethost-dev/cli/internal/fail"
)

// A Server is the panel as an authorization server: what its metadata says of it (RFC 8414).
type Server struct {
	// Who answers a sign-in: the panel's own address.
	Issuer string `json:"issuer"`
	// The page where the person approves.
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	// Where a code is traded for the token.
	TokenEndpoint string `json:"token_endpoint"`
	// Where a client registers itself (RFC 7591).
	RegistrationEndpoint string `json:"registration_endpoint"`
	// Where a token is revoked (RFC 7009). Empty = the panel offers none.
	RevocationEndpoint string `json:"revocation_endpoint"`
	// The panel names itself in its answer (RFC 9207): an answer without `iss` is not its own.
	NamesItself bool `json:"authorization_response_iss_parameter_supported"`
}

// A Refusal is the server's own no to a request (RFC 6749, 5.2).
type Refusal struct {
	// OAuth's word for it: "invalid_grant".
	Code string `json:"error"`
	// What is wrong, for people.
	Description string `json:"error_description"`
}

func (r *Refusal) Error() string {
	if r.Description == "" {
		return "the panel refused: " + r.Code
	}
	return "the panel refused: " + r.Description
}

// metadataPath is where an authorization server describes itself.
const metadataPath = "/.well-known/oauth-authorization-server"

// Discover reads the panel's description of itself as an authorization server. The description
// must be of the panel that was asked: one that names another issuer is refused, since every
// later check compares an answer's sender with this name.
func Discover(ctx context.Context, panelURL string) (*Server, error) {
	panelURL = strings.TrimRight(panelURL, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, panelURL+metadataPath, nil)
	if err != nil {
		return nil, err
	}

	var server Server
	if err := call(request, &server); err != nil {
		return nil, err
	}
	switch {
	case strings.TrimRight(server.Issuer, "/") != panelURL:
		return nil, fmt.Errorf("%s describes the sign-in of %q, not its own", panelURL, server.Issuer)
	case server.AuthorizationEndpoint == "" || server.TokenEndpoint == "" || server.RegistrationEndpoint == "":
		return nil, fmt.Errorf("%s does not say where a program signs in", panelURL)
	}
	return &server, nil
}

// registeredRedirect is the address a client registers for the browser's answer. The port it
// listens on is known only when it asks, and a loopback address's port may differ from the
// registered one (RFC 8252, 7.3).
const registeredRedirect = "http://127.0.0.1" + callbackPath

// Register registers a client under the name the panel's settings will show, and returns its
// id. It stores nothing there and gives no access: the person's approval does.
func (s *Server) Register(ctx context.Context, name string) (clientID string, err error) {
	asked, err := json.Marshal(map[string]any{"client_name": name, "redirect_uris": []string{registeredRedirect}})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.RegistrationEndpoint, strings.NewReader(string(asked)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")

	var registered struct {
		ClientID string `json:"client_id"`
	}
	if err := call(request, &registered); err != nil {
		return "", err
	}
	if registered.ClientID == "" {
		return "", errors.New("the panel registered the program without an id")
	}
	return registered.ClientID, nil
}

// exchange trades a code for the token, once: a code that was tried is spent.
func (s *Server) exchange(ctx context.Context, clientID, code, redirectURI, verifier string) (token string, err error) {
	var answer struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	err = s.post(ctx, s.TokenEndpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}, &answer)
	if err != nil {
		return "", err
	}
	if answer.AccessToken == "" || !strings.EqualFold(answer.TokenType, "Bearer") {
		return "", errors.New("the panel answered the sign-in without a token the CLI can use")
	}
	return answer.AccessToken, nil
}

// Revoke ends a token at the panel (RFC 7009), so that it works nowhere any more. It needs a
// panel that offers it: RevocationEndpoint is then not empty.
func (s *Server) Revoke(ctx context.Context, clientID, token string) error {
	return s.post(ctx, s.RevocationEndpoint, url.Values{
		"token":           {token},
		"token_type_hint": {"access_token"},
		"client_id":       {clientID},
	}, nil)
}

func (s *Server) post(ctx context.Context, endpoint string, form url.Values, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return call(request, into)
}

// client sends the few requests of a sign-in; none is a stream, so each is bounded whole.
var client = &http.Client{Timeout: 30 * time.Second}

// answerBytes bounds what is read of an answer: each is a small JSON document.
const answerBytes = 64 << 10

// call sends a request and reads its JSON answer into into (nil = the answer is not read). The
// server's own no is a *Refusal; a request that got no answer is said without its address.
func call(request *http.Request, into any) error {
	response, err := client.Do(request)
	if err != nil {
		return notReached(request, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, answerBytes))
	if err != nil {
		return notReached(request, err)
	}
	if response.StatusCode/100 != 2 {
		var refusal Refusal
		if json.Unmarshal(body, &refusal) == nil && refusal.Code != "" {
			return &refusal
		}
		return fmt.Errorf("%s answered %s to %s", request.URL.Host, response.Status, request.URL.Path)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%s answered %s with what is not JSON", request.URL.Host, request.URL.Path)
	}
	return nil
}

// NotReached is a request that got no answer: the panel is down, or the network is.
type NotReached struct {
	// The innermost cause, "connection refused", without the layers that wrap it.
	Cause error
}

func (e *NotReached) Error() string { return "the panel could not be reached: " + e.Cause.Error() }

func notReached(request *http.Request, err error) error {
	if ctxErr := request.Context().Err(); ctxErr != nil {
		return ctxErr
	}
	return &NotReached{Cause: fail.Innermost(err)}
}
