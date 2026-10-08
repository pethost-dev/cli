package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"time"
)

// An Attempt is one sign-in under way: a port that waits for the browser's answer, and the two
// secrets that tie the answer to it. `state` says the answer is to this request; the PKCE
// verifier, of which the panel saw only the hash, says the code is traded by who asked.
type Attempt struct {
	// Where the person approves: the address to open in their browser.
	URL string

	server      *Server
	clientID    string
	listener    net.Listener
	redirectURI string
	state       string
	verifier    string
}

// ErrDenied is the person's own no: they pressed Cancel on the panel's page.
var ErrDenied = errors.New("the sign-in was cancelled in the browser")

// callbackPath is where the browser brings the answer.
const callbackPath = "/callback"

// Begin starts a sign-in of the client: it listens on a free loopback port, which only programs
// on this computer reach, and makes the address the person opens. Close ends it.
func (s *Server) Begin(clientID string) (*Attempt, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("this computer gives no port to wait for the browser on: %w", err)
	}

	a := &Attempt{
		server:      s,
		clientID:    clientID,
		listener:    listener,
		redirectURI: "http://" + listener.Addr().String() + callbackPath,
		state:       secret(),
		verifier:    secret(),
	}
	a.URL, err = a.approvalURL()
	if err != nil {
		listener.Close()
		return nil, err
	}
	return a, nil
}

// secret is 32 random bytes in base64url: 43 characters, what a PKCE verifier is at least.
func secret() string {
	random := make([]byte, 32)
	rand.Read(random) // It never fails: the runtime ends the process when the system has no randomness.
	return base64.RawURLEncoding.EncodeToString(random)
}

func (a *Attempt) approvalURL() (string, error) {
	address, err := url.Parse(a.server.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("the panel's sign-in page has an address that cannot be read: %w", err)
	}

	challenge := sha256.Sum256([]byte(a.verifier))
	query := address.Query()
	query.Set("response_type", "code")
	query.Set("client_id", a.clientID)
	query.Set("redirect_uri", a.redirectURI)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	query.Set("state", a.state)
	address.RawQuery = query.Encode()
	return address.String(), nil
}

// Close stops waiting for the answer.
func (a *Attempt) Close() {
	a.listener.Close()
}

// Wait waits for the browser's answer and trades its code for the token. The first answer that
// is this sign-in's ends it, well or not: ErrDenied when the person said no. A request that is
// not this sign-in's answer, as one with another `state`, is refused and the wait goes on, so
// that nothing else on the computer can end a sign-in by guessing its port.
func (a *Attempt) Wait(ctx context.Context) (token string, err error) {
	type end struct {
		token string
		err   error
	}
	ended := make(chan end, 1)

	answer := func(w http.ResponseWriter, r *http.Request) {
		code, err := a.read(r)
		if errors.Is(err, errNotThis) {
			http.Error(w, "This is not the answer to a sign-in that waits here.", http.StatusBadRequest)
			return
		}

		var token string
		if err == nil {
			token, err = a.server.exchange(ctx, a.clientID, code, a.redirectURI, a.verifier)
		}
		writePage(w, err)
		select {
		case ended <- end{token, err}:
		default: // The page was loaded again: the sign-in has its end.
		}
	}

	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(answer)}
	go server.Serve(a.listener)
	defer finish(server)

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case e := <-ended:
		return e.token, e.err
	}
}

// finish closes the server once the page it is writing has gone out.
func finish(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if server.Shutdown(ctx) != nil {
		server.Close()
	}
}

// errNotThis is a request that is not this sign-in's answer.
var errNotThis = errors.New("not this sign-in's answer")

// read checks a request to the port and returns the code it brings. The order matters: only a
// request with this sign-in's `state` is its answer at all, and only then is its sender looked
// at, since an answer from another server with the right state is an attack on this sign-in
// (RFC 9207), not noise.
func (a *Attempt) read(r *http.Request) (code string, err error) {
	query := r.URL.Query()
	if r.Method != http.MethodGet || r.URL.Path != callbackPath ||
		subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(a.state)) != 1 {
		return "", errNotThis
	}

	sender := query.Get("iss")
	if sender != a.server.Issuer && (sender != "" || a.server.NamesItself) {
		return "", fmt.Errorf("the answer came from %q, not from %s: nothing was signed in", sender, a.server.Issuer)
	}

	switch refused := query.Get("error"); {
	case refused == "access_denied":
		return "", ErrDenied
	case refused != "":
		return "", &Refusal{Code: refused, Description: query.Get("error_description")}
	case query.Get("code") == "":
		return "", errors.New("the panel's answer has no code")
	}
	return query.Get("code"), nil
}

// writePage tells the person in the browser how the sign-in ended, and where to look next. The
// page's address holds the code, which is spent by now; the page is not kept and sends its
// address nowhere.
func writePage(w http.ResponseWriter, err error) {
	title, text := "Signed in", "pethost on this computer is signed in. Close this tab and go back to your terminal."
	switch {
	case errors.Is(err, ErrDenied):
		title, text = "Not signed in", "You cancelled the sign-in. Nothing was changed: go back to your terminal."
	case err != nil:
		title, text = "Not signed in", "The sign-in did not finish: "+err.Error()+". Go back to your terminal."
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	fmt.Fprintf(w, page, html.EscapeString(title), html.EscapeString(text))
}

const page = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>Pethost</title>
<body style="font: 17px/1.5 system-ui, sans-serif; max-width: 30em; margin: 22vh auto; padding: 0 1.5em">
<h1 style="font-size: 1.4em">%s</h1>
<p>%s</p>
`
