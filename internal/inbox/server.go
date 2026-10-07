// Package inbox is the read-only web view behind `ihme serve`: one
// page per shared address listing the mail it received, and one page
// per message, as plain text.
//
// The privacy model, in order of what it protects:
//
//   - Each address is its own door. A share key (internal/shares)
//     opens exactly one address. Message links are sealed tokens
//     bound to that address (token.go), so UIDs cannot be walked or
//     counted, and a message is shown only after its envelope proves
//     it was sent to that address and arrived inside the window.
//     Other recipients are never displayed.
//   - Nothing loads from anywhere. Mail HTML is converted to text on
//     the server; the page has no scripts, no images, and a CSP of
//     default-src 'none'. Links are text the reader may choose to
//     follow, with no referrer.
//   - The server holds a mail-reading credential only (an IMAP
//     app-specific password), never the iCloud web session: a leak
//     reads mail, it cannot create, delete, or redirect addresses.
//   - There is no admin page. Shares are minted and revoked with
//     `ihme share` on the machine that runs the server.
package inbox

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lroolle/ihme-cli/internal/shares"
	"github.com/lroolle/ihme-cli/pkg/output"
)

//go:embed web/*.html web/style.css
var webFS embed.FS

var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"join": strings.Join,
}).ParseFS(webFS, "web/*.html"))

const (
	cookieName = "ihme_key"
	cookieAge  = 7 * 24 * time.Hour
	// listTTL and messageTTL absorb reload storms: with one upstream
	// call per address at a time (turn), a key holder hammering a page
	// costs one IMAP round per window however many requests arrive.
	listTTL     = 20 * time.Second
	messageTTL  = 2 * time.Minute
	maxMessages = 32
)

// Server serves the viewer. Zero values are not useful; set every
// exported field.
type Server struct {
	// SharesPath is read on every request, so a revoke is immediate.
	SharesPath string
	Mail       Source
	Days       int
	Limit      int
	// Logf records who opened what, and what failed upstream. Keys
	// never reach it.
	Logf func(format string, args ...any)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Secure marks the key cookie Secure on every response, not only
	// when the request shows TLS. Set it when the public URL is https,
	// so a proxy that forgets X-Forwarded-Proto cannot leak the key
	// over a plain-HTTP request to the same host.
	Secure bool

	once     sync.Once
	links    *sealer
	mu       sync.Mutex
	gates    map[string]chan struct{}
	lists    map[string]cachedList
	messages map[string]cachedMessage
}

type cachedList struct {
	at   time.Time
	list []Summary
}

type cachedMessage struct {
	at  time.Time
	msg *Message
}

// Handler is the whole site.
func (s *Server) Handler() http.Handler {
	s.once.Do(func() { s.links = newSealer() })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("POST /open", s.open)
	mux.HandleFunc("GET /k/{key}", s.link)
	mux.HandleFunc("GET /inbox", s.inbox)
	mux.HandleFunc("GET /m/{token}", s.message)
	mux.HandleFunc("POST /close", s.close)
	mux.HandleFunc("GET /style.css", s.style)
	return secureHeaders(mux)
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// viewer is the share the request's cookie opens. revoked reports a
// cookie that used to open something, so the page can say so.
func (s *Server) viewer(r *http.Request) (share *shares.Share, revoked bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil, false
	}
	list, err := shares.Load(s.SharesPath)
	if err != nil {
		s.logf("reading shares: %v", err)
		return nil, false
	}
	share, ok := shares.Lookup(list, c.Value)
	if !ok {
		return nil, true
	}
	return share, false
}

func (s *Server) setKey(w http.ResponseWriter, r *http.Request, key string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    shares.Normalize(key),
		Path:     "/",
		MaxAge:   int(cookieAge / time.Second),
		HttpOnly: true,
		// Lax, not Strict: a share link opened from a chat app is a
		// cross-site navigation, and Strict would drop the cookie on
		// the redirect to /inbox, landing the visitor on a login form.
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secure(r),
	})
}

func (s *Server) clearKey(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secure(r)})
}

// secure trusts X-Forwarded-Proto for one decision only, the cookie's
// Secure flag; a forged header can only make the cookie stricter.
func (s *Server) secure(r *http.Request) bool {
	return s.Secure || r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

type homePage struct {
	Title   string
	Error   string
	Address string
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	share, revoked := s.viewer(r)
	if share != nil {
		http.Redirect(w, r, "/inbox", http.StatusSeeOther)
		return
	}
	if revoked {
		s.signedOut(w, r, true)
		return
	}
	render(w, http.StatusOK, "home.html", homePage{Title: "Inbox"})
}

// signedOut answers a request without a working key. A key that
// stopped working is said out loud, right where the visitor is: a
// silent bounce to the sign-in form reads as "I typed it wrong".
func (s *Server) signedOut(w http.ResponseWriter, r *http.Request, revoked bool) {
	if !revoked {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.clearKey(w, r)
	render(w, http.StatusUnauthorized, "home.html", homePage{
		Title: "Inbox",
		Error: "Your access key no longer works. Ask the owner for a new one.",
	})
}

func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	address := strings.TrimSpace(r.PostFormValue("address"))
	key := r.PostFormValue("key")
	list, err := shares.Load(s.SharesPath)
	if err != nil {
		s.logf("reading shares: %v", err)
		render(w, http.StatusInternalServerError, "notice.html", notice("Something is wrong on this server", "The owner needs to check its log."))
		return
	}
	share, ok := shares.Lookup(list, key)
	if !ok || !strings.EqualFold(share.Address, address) {
		// One message for both: which half was wrong is not ours to say.
		s.logf("open refused from %s", clientIP(r))
		render(w, http.StatusUnauthorized, "home.html", homePage{Title: "Inbox", Address: address, Error: "That address and key do not match."})
		return
	}
	s.logf("open %s from %s", share.Address, clientIP(r))
	s.setKey(w, r, key)
	http.Redirect(w, r, "/inbox", http.StatusSeeOther)
}

// link opens a share from the URL `ihme share` prints, then moves the
// key out of the address bar.
func (s *Server) link(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	list, err := shares.Load(s.SharesPath)
	if err != nil {
		s.logf("reading shares: %v", err)
		render(w, http.StatusInternalServerError, "notice.html", notice("Something is wrong on this server", "The owner needs to check its log."))
		return
	}
	share, ok := shares.Lookup(list, key)
	if !ok {
		s.logf("link refused from %s", clientIP(r))
		render(w, http.StatusNotFound, "notice.html", notice("This link does not open anything", "It was mistyped, or the owner revoked it. Ask them for a new one."))
		return
	}
	s.logf("open %s by link from %s", share.Address, clientIP(r))
	s.setKey(w, r, key)
	http.Redirect(w, r, "/inbox", http.StatusSeeOther)
}

func (s *Server) close(w http.ResponseWriter, r *http.Request) {
	s.clearKey(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type row struct {
	Href, From, Subject, Ago, Full string
	Junk                           bool
}

type inboxPage struct {
	Title, Address, Checked, Error string
	Days, Limit                    int
	Capped                         bool
	Rows                           []row
}

func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	share, revoked := s.viewer(r)
	if share == nil {
		s.signedOut(w, r, revoked)
		return
	}
	now := s.now()
	page := inboxPage{Title: share.Address, Address: share.Address, Days: s.Days, Limit: s.Limit}
	list, at, err := s.list(r.Context(), share.Address, s.since(now))
	if r.Context().Err() != nil {
		return // the visitor left; nobody to answer
	}
	if err != nil {
		s.logf("listing %s: %v", share.Address, err)
		page.Error = "The mailbox did not answer. Try again in a moment."
		render(w, http.StatusBadGateway, "inbox.html", page)
		return
	}
	// Times are UTC: the server's own zone is the owner's, not the
	// visitor's business.
	page.Checked = at.UTC().Format("15:04:05 UTC")
	page.Capped = len(list) >= s.Limit
	for _, m := range list {
		page.Rows = append(page.Rows, row{
			Href:    "/m/" + s.links.seal(share.Address, m.Folder, m.UID),
			From:    orDash(m.From),
			Subject: orNoSubject(m.Subject),
			Ago:     output.Ago(m.Date, now.UTC()),
			Full:    m.Date.UTC().Format("Mon Jan 2 2006 15:04 UTC"),
			Junk:    m.Junk,
		})
	}
	render(w, http.StatusOK, "inbox.html", page)
}

// since is the start of the window, on a UTC day boundary: IMAP SEARCH
// SINCE compares dates, not times, so List and Get must agree on the
// day or a listed message could refuse to open.
func (s *Server) since(now time.Time) time.Time {
	d := now.UTC().AddDate(0, 0, -s.Days)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

// turn waits until no other upstream call for this address is in
// flight. A key holder then has at most one place in the queue for
// the mailbox connection every address shares, so reading a hundred
// messages in a loop delays nobody else by more than one fetch; and a
// visitor who gives up leaves the queue instead of leaving it work.
func (s *Server) turn(ctx context.Context, address string) (done func(), err error) {
	key := strings.ToLower(address)
	s.mu.Lock()
	if s.gates == nil {
		s.gates = make(map[string]chan struct{})
	}
	g := s.gates[key]
	if g == nil {
		g = make(chan struct{}, 1)
		s.gates[key] = g
	}
	s.mu.Unlock()
	select {
	case g <- struct{}{}:
		return func() { <-g }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Server) list(ctx context.Context, address string, since time.Time) ([]Summary, time.Time, error) {
	key := strings.ToLower(address)
	fresh := func() (cachedList, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, ok := s.lists[key]
		return c, ok && s.now().Sub(c.at) < listTTL
	}
	if c, ok := fresh(); ok {
		return c.list, c.at, nil
	}
	done, err := s.turn(ctx, address)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer done()
	if c, ok := fresh(); ok { // filled while we waited our turn
		return c.list, c.at, nil
	}
	list, err := s.Mail.List(address, since, s.Limit)
	if err != nil {
		return nil, time.Time{}, err
	}
	c := cachedList{at: s.now(), list: list}
	s.mu.Lock()
	if s.lists == nil {
		s.lists = make(map[string]cachedList)
	}
	s.lists[key] = c
	s.mu.Unlock()
	return c.list, c.at, nil
}

func (s *Server) get(ctx context.Context, address, folder string, uid uint32, since time.Time) (*Message, error) {
	key := strings.ToLower(address) + "\x00" + folder + "\x00" + strconv.FormatUint(uint64(uid), 10)
	fresh := func() (*Message, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, ok := s.messages[key]
		return c.msg, ok && s.now().Sub(c.at) < messageTTL
	}
	if m, ok := fresh(); ok {
		return m, nil
	}
	done, err := s.turn(ctx, address)
	if err != nil {
		return nil, err
	}
	defer done()
	if m, ok := fresh(); ok { // filled while we waited our turn
		return m, nil
	}
	m, err := s.Mail.Get(address, folder, uid, since)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.messages == nil {
		s.messages = make(map[string]cachedMessage)
	}
	if len(s.messages) >= maxMessages {
		oldest := ""
		for k, c := range s.messages {
			if oldest == "" || c.at.Before(s.messages[oldest].at) {
				oldest = k
			}
		}
		delete(s.messages, oldest)
	}
	s.messages[key] = cachedMessage{at: s.now(), msg: m}
	s.mu.Unlock()
	return m, nil
}

type messagePage struct {
	Title, Address, From, Subject, Full string
	Junk, TooLarge, TooComplex          bool
	Body                                []Segment
	Attachments                         []string
}

func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	share, revoked := s.viewer(r)
	if share == nil {
		s.signedOut(w, r, revoked)
		return
	}
	folder, uid, ok := s.links.open(share.Address, r.PathValue("token"))
	if !ok {
		render(w, http.StatusNotFound, "notice.html", notice("This link has expired", "Message links last until the server restarts. Go back to the inbox for fresh ones."))
		return
	}
	m, err := s.get(r.Context(), share.Address, folder, uid, s.since(s.now()))
	switch {
	case r.Context().Err() != nil:
		return // the visitor left; nobody to answer
	case errors.Is(err, ErrNotFound):
		render(w, http.StatusNotFound, "notice.html", notice("No such message",
			fmt.Sprintf("It was deleted or moved, or it is older than %d days.", s.Days)))
		return
	case err != nil:
		s.logf("reading %s %s/%d: %v", share.Address, folder, uid, err)
		render(w, http.StatusBadGateway, "notice.html", notice("The mailbox did not answer", "Try again in a moment."))
		return
	}
	render(w, http.StatusOK, "message.html", messagePage{
		Title:       orNoSubject(m.Subject),
		Address:     share.Address,
		From:        orDash(m.From),
		Subject:     orNoSubject(m.Subject),
		Full:        m.Date.UTC().Format("Mon Jan 2 2006 15:04 UTC"),
		Junk:        m.Junk,
		TooLarge:    m.TooLarge,
		TooComplex:  m.TooComplex,
		Body:        linkify(m.Text),
		Attachments: m.Attachments,
	})
}

func (s *Server) style(w http.ResponseWriter, r *http.Request) {
	css, _ := webFS.ReadFile("web/style.css")
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(css)
}

type noticePage struct{ Title, Text string }

func notice(title, text string) noticePage { return noticePage{Title: title, Text: text} }

func render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = pages.ExecuteTemplate(w, name, data)
}

// clientIP is the peer address for the log. Behind a reverse proxy it
// is the proxy; the proxy's own log has the visitor.
func clientIP(r *http.Request) string {
	if i := strings.LastIndexByte(r.RemoteAddr, ':'); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orNoSubject(s string) string {
	if s == "" {
		return "(no subject)"
	}
	return s
}
