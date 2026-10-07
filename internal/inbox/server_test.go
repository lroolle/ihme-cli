package inbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lroolle/ihme-cli/internal/shares"
)

// fakeMail is a Source with one hostile message for the shared
// address and one private message for another.
type fakeMail struct {
	lists, gets atomic.Int32
	slow        time.Duration
}

func (f *fakeMail) List(address string, since time.Time, limit int) ([]Summary, error) {
	f.lists.Add(1)
	time.Sleep(f.slow)
	if address != alias {
		return nil, nil
	}
	return []Summary{{
		Folder: "Junk", Junk: true, UID: 7, Date: time.Now().Add(-time.Hour),
		From: `Evil <x@evil.example>`, Subject: `<script>alert("s")</script> code`,
	}}, nil
}

func (f *fakeMail) Get(address, folder string, uid uint32, since time.Time) (*Message, error) {
	f.gets.Add(1)
	time.Sleep(f.slow)
	if address != alias || folder != "Junk" || uid != 7 {
		return nil, ErrNotFound
	}
	return &Message{
		Summary: Summary{Folder: "Junk", Junk: true, UID: 7, Date: time.Now(), From: "Evil <x@evil.example>", Subject: "hi"},
		Text:    "Code 123456 <img src=x onerror=alert(1)> https://ok.example/v?a=1&b=2 javascript:alert(2)",
	}, nil
}

type site struct {
	*httptest.Server
	srv      *Server
	path     string
	key      string
	otherKey string
	mail     *fakeMail
}

func newSite(t *testing.T) *site {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shares.json")
	key, _, err := shares.Grant(path, alias, "id1", "netflix", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	otherKey, _, err := shares.Grant(path, "other_1x@icloud.com", "id2", "bank", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	mail := &fakeMail{}
	srv := &Server{SharesPath: path, Mail: mail, Days: 30, Limit: 50}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &site{Server: ts, srv: srv, path: path, key: key, otherKey: otherKey, mail: mail}
}

var hrefPattern = regexp.MustCompile(`href="(/m/[^"]+)"`)

// firstMessage opens the inbox and returns the first message link on it.
func firstMessage(t *testing.T, s *site, c *http.Client) string {
	t.Helper()
	_, list := get(t, c, s.URL+"/k/"+s.key)
	m := hrefPattern.FindStringSubmatch(list)
	if m == nil {
		t.Fatalf("no message link on the inbox page:\n%s", list)
	}
	return m[1]
}

func (s *site) browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func get(t *testing.T, c *http.Client, u string) (*http.Response, string) {
	t.Helper()
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestOpenWithAddressAndKey(t *testing.T) {
	s := newSite(t)
	c := s.browser()
	resp, err := c.PostForm(s.URL+"/open", url.Values{"address": {strings.ToUpper(alias)}, "key": {strings.ToUpper(s.key)}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Request.URL.Path != "/inbox" || !strings.Contains(string(body), alias) {
		t.Fatalf("landed on %s:\n%s", resp.Request.URL.Path, body)
	}
}

func TestOpenRefusesMismatch(t *testing.T) {
	s := newSite(t)
	for _, form := range []url.Values{
		{"address": {"other_1x@icloud.com"}, "key": {s.key}}, // a real key, the wrong door
		{"address": {alias}, "key": {"nope"}},
	} {
		resp, err := s.browser().PostForm(s.URL+"/open", form)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), "do not match") {
			t.Errorf("form %v: status %d", form, resp.StatusCode)
		}
		if len(resp.Cookies()) != 0 {
			t.Errorf("a refused open set a cookie")
		}
	}
}

func TestLinkOpensThenRevokeCloses(t *testing.T) {
	s := newSite(t)
	c := s.browser()
	resp, body := get(t, c, s.URL+"/k/"+s.key)
	if resp.Request.URL.Path != "/inbox" || !strings.Contains(body, alias) {
		t.Fatalf("link landed on %s", resp.Request.URL.Path)
	}
	if strings.Contains(resp.Request.URL.String(), s.key) {
		t.Error("the key stayed in the address bar")
	}

	shares.Revoke(s.path, func(sh shares.Share) bool { return sh.Address == alias })
	resp, body = get(t, c, s.URL+"/inbox")
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "no longer works") || strings.Contains(body, "evil.example") {
		t.Errorf("after revoke: %d\n%s", resp.StatusCode, body)
	}
	if resp, _ := get(t, s.browser(), s.URL+"/k/"+s.key); resp.StatusCode != http.StatusNotFound {
		t.Errorf("revoked link status %d", resp.StatusCode)
	}
}

func TestPagesAreLockedDown(t *testing.T) {
	s := newSite(t)
	resp, err := http.Get(s.URL + "/k/" + s.key)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "frame-ancestors 'none'", "form-action 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if resp.Header.Get("Referrer-Policy") != "no-referrer" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("headers: %v", resp.Header)
	}

	// The cookie, as the redirect set it.
	req, _ := http.NewRequest("GET", s.URL+"/k/"+s.key, nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	raw, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	raw.Body.Close()
	ck := raw.Cookies()
	if len(ck) != 1 || !ck[0].HttpOnly || !ck[0].Secure || ck[0].SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie = %+v", ck)
	}
}

func TestMailCannotBecomeMarkup(t *testing.T) {
	s := newSite(t)
	c := s.browser()
	_, list := get(t, c, s.URL+"/k/"+s.key)
	if strings.Contains(list, "<script>") || !strings.Contains(list, "&lt;script&gt;") {
		t.Errorf("subject not escaped:\n%s", list)
	}
	link := firstMessage(t, s, c)
	if strings.Contains(link, "7") && strings.Contains(link, "Junk") {
		t.Errorf("link %q exposes the folder and UID", link)
	}

	_, msg := get(t, c, s.URL+link)
	if strings.Contains(msg, "<img") {
		t.Errorf("body markup reached the page:\n%s", msg)
	}
	if !strings.Contains(msg, `<a href="https://ok.example/v?a=1&amp;b=2" rel="noopener noreferrer nofollow">`) {
		t.Errorf("http link not rendered as a safe link:\n%s", msg)
	}
	if strings.Contains(msg, `href="javascript`) {
		t.Error("a javascript: URL became a link")
	}
	if !strings.Contains(msg, "To</dt><dd>"+alias) {
		t.Errorf("recipient should be the shared address only:\n%s", msg)
	}
}

func TestLinksCannotBeForgedOrCarried(t *testing.T) {
	s := newSite(t)
	c := s.browser()
	link := firstMessage(t, s, c)
	if resp, _ := get(t, c, s.URL+link); resp.StatusCode != http.StatusOK {
		t.Fatalf("own link: status %d", resp.StatusCode)
	}
	// Forged: a UID-shaped path, a hand-sealed link for a mailbox
	// place this visitor was never shown, a truncated token.
	forged := []string{"/m/7", "/m/" + newSealer().seal(alias, "Junk", 7), s.URL + link[:len(link)-4]}
	for _, path := range forged {
		path = strings.TrimPrefix(path, s.URL)
		if resp, _ := get(t, c, s.URL+path); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, resp.StatusCode)
		}
	}
	// Carried: the same valid link, opened under another address's key.
	other := s.browser()
	get(t, other, s.URL+"/k/"+s.otherKey)
	if resp, body := get(t, other, s.URL+link); resp.StatusCode != http.StatusNotFound || strings.Contains(body, "123456") {
		t.Errorf("a link worked under another address's key: %d", resp.StatusCode)
	}
	if n := s.mail.gets.Load(); n != 1 {
		t.Errorf("forged or carried links reached IMAP: %d gets, want 1", n)
	}
}

func TestSignedOutSeesNoMail(t *testing.T) {
	s := newSite(t)
	for _, path := range []string{"/inbox", "/m/7?folder=Junk"} {
		resp, body := get(t, s.browser(), s.URL+path)
		if resp.Request.URL.Path != "/" || strings.Contains(body, "evil.example") {
			t.Errorf("%s without a key reached %s", path, resp.Request.URL.Path)
		}
	}
	if s.mail.lists.Load() != 0 {
		t.Error("an anonymous visitor caused an IMAP search")
	}
}

// One key holder firing many requests at once costs one IMAP round
// per page, not one per request: the mailbox is shared by every viewer.
func TestConcurrentStormHitsIMAPOnce(t *testing.T) {
	s := newSite(t)
	s.mail.slow = 50 * time.Millisecond
	c := s.browser()
	link := firstMessage(t, s, c) // 1 list
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() { defer wg.Done(); get(t, c, s.URL+"/inbox") }()
		go func() { defer wg.Done(); get(t, c, s.URL+link) }()
	}
	wg.Wait()
	if n := s.mail.lists.Load(); n != 1 {
		t.Errorf("%d IMAP searches for 21 inbox loads inside the cache window", n)
	}
	if n := s.mail.gets.Load(); n != 1 {
		t.Errorf("%d IMAP fetches for 20 concurrent opens of one message", n)
	}
}

// gatedMail holds every upstream call until release closes, and
// records the most calls in flight at once per address.
type gatedMail struct {
	mu      sync.Mutex
	in, max map[string]int
	release chan struct{}
	entered chan string
}

func newGatedMail() *gatedMail {
	return &gatedMail{in: map[string]int{}, max: map[string]int{}, release: make(chan struct{}), entered: make(chan string, 8)}
}

func (g *gatedMail) enter(address string) {
	g.mu.Lock()
	g.in[address]++
	g.max[address] = max(g.max[address], g.in[address])
	g.mu.Unlock()
	g.entered <- address
	<-g.release
	g.mu.Lock()
	g.in[address]--
	g.mu.Unlock()
}

func (g *gatedMail) List(address string, since time.Time, limit int) ([]Summary, error) {
	g.enter(address)
	return nil, nil
}

func (g *gatedMail) Get(address, folder string, uid uint32, since time.Time) (*Message, error) {
	g.enter(address)
	return &Message{Summary: Summary{Folder: folder, UID: uid}}, nil
}

// One key holder reading message after message holds at most one
// place in the queue for the shared mailbox connection: other
// addresses are not stuck behind them, and a visitor who leaves
// never reaches IMAP.
func TestOneUpstreamCallPerAddress(t *testing.T) {
	const other = "other_1x@icloud.com"
	g := newGatedMail()
	s := &Server{Mail: g, Days: 30, Limit: 50}
	errs := make(chan error, 3)
	for _, c := range []struct {
		address string
		uid     uint32
	}{{alias, 1}, {alias, 2}, {other, 3}} {
		go func() {
			_, err := s.get(context.Background(), c.address, "INBOX", c.uid, time.Time{})
			errs <- err
		}()
	}
	seen := map[string]int{}
	for range 2 {
		select {
		case a := <-g.entered:
			seen[a]++
		case <-time.After(2 * time.Second):
			t.Fatalf("upstream calls in flight: %v, want one for each address", seen)
		}
	}
	if seen[alias] != 1 || seen[other] != 1 {
		t.Fatalf("upstream calls in flight: %v, want one for each address", seen)
	}
	select {
	case a := <-g.entered:
		t.Fatalf("a second call for %s ran while its first was in flight", a)
	case <-time.After(100 * time.Millisecond):
	}

	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.get(gone, alias, "INBOX", 4, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Errorf("a visitor who left: err = %v, want context.Canceled", err)
	}

	close(g.release)
	for range 3 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.max[alias] != 1 || len(g.entered) != 1 {
		t.Errorf("max in flight for one address = %d, later calls = %d; want 1 and 1 (uid 4 never ran)", g.max[alias], len(g.entered))
	}
}

// With a public https URL the key cookie is Secure even on a request
// that arrived as plain HTTP, as behind a proxy that drops
// X-Forwarded-Proto.
func TestSecureCookieForHTTPSDeployments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shares.json")
	key, _, err := shares.Grant(path, alias, "id1", "netflix", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, secure := range []bool{false, true} {
		srv := &Server{SharesPath: path, Mail: &fakeMail{}, Days: 30, Limit: 50, Secure: secure}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/k/"+key, nil))
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Secure != secure {
			t.Errorf("Secure=%v: cookies %+v", secure, cookies)
		}
	}
}

// Pages show times in UTC: the server's zone is the owner's.
func TestTimesAreUTC(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("XST", 8*3600)
	t.Cleanup(func() { time.Local = local })
	s := newSite(t)
	c := s.browser()
	_, inbox := get(t, c, s.URL+"/k/"+s.key)
	_, msg := get(t, c, s.URL+firstMessage(t, s, c))
	for name, page := range map[string]string{"inbox": inbox, "message": msg} {
		if strings.Contains(page, "XST") || !strings.Contains(page, " UTC") {
			t.Errorf("%s page shows the server's zone, or no UTC:\n%s", name, page)
		}
	}
}
