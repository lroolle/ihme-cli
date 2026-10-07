package inbox

import (
	"errors"
	"fmt"
	"mime"
	"net"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
)

// ErrNotFound is a message that does not exist, was not sent to the
// address asking for it, or is older than the viewer's window.
var ErrNotFound = errors.New("message not found")

// maxRaw caps the message size the viewer downloads. Bigger mail is
// almost always attachments, which the viewer never shows anyway.
const maxRaw = 8 << 20

// opTimeout bounds one IMAP operation. The client waits on a reply
// with no read deadline, and every viewer shares one connection, so a
// link that dies mid-command would otherwise hold them all until the
// kernel gives up on the socket.
const opTimeout = 45 * time.Second

// Summary is one row of an address's inbox.
type Summary struct {
	Folder  string
	Junk    bool
	UID     uint32
	Date    time.Time
	From    string
	Subject string
}

// Message is one message, readable.
type Message struct {
	Summary
	Text        string
	Attachments []string
	TooLarge    bool
	TooComplex  bool // MIME nested past maxMultipart; not parsed
}

// Source is where the viewer reads mail from.
type Source interface {
	// List returns up to limit messages sent to address since since,
	// newest first.
	List(address string, since time.Time, limit int) ([]Summary, error)
	// Get returns one message, or ErrNotFound unless it was sent to
	// address, lives in a folder List reads, and arrived since since.
	Get(address, folder string, uid uint32, since time.Time) (*Message, error)
}

// IMAP reads one mailbox over IMAP: the INBOX and the junk folder,
// where verification codes from unfamiliar senders tend to land. It
// keeps one connection, serialized, and redials once when an idle
// connection turns out to be dead.
type IMAP struct {
	Dial func() (*imapclient.Client, error)

	mu      sync.Mutex
	c       *imapclient.Client
	folders []folder
}

type folder struct {
	name string
	junk bool
}

// DialTLS returns a Dial for host:port over implicit TLS, logging in
// as user. iCloud is imap.mail.me.com:993 with an app-specific
// password; the IMAP user is the @icloud.com address, not the Apple
// ID when they differ.
func DialTLS(addr, user, password string) func() (*imapclient.Client, error) {
	return func() (*imapclient.Client, error) {
		opts := ClientOptions()
		opts.Dialer = &net.Dialer{Timeout: 15 * time.Second}
		c, err := imapclient.DialTLS(addr, opts)
		if err != nil {
			return nil, fmt.Errorf("connecting to %s: %w", addr, err)
		}
		watchdog := time.AfterFunc(opTimeout, func() { c.Close() })
		err = c.Login(user, password).Wait()
		watchdog.Stop()
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("IMAP login as %s: %w", user, err)
		}
		return c, nil
	}
}

// ClientOptions decodes RFC 2047 subjects in any charset mail uses,
// not only UTF-8 and Latin-1.
func ClientOptions() *imapclient.Options {
	return &imapclient.Options{WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader}}
}

// Check connects once, so a wrong password fails at startup instead
// of on the first visitor.
func (m *IMAP) Check() error {
	return m.do(func(c *imapclient.Client) error {
		_, err := m.discover(c)
		return err
	})
}

func (m *IMAP) do(fn func(c *imapclient.Client) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if m.c != nil {
			select {
			case <-m.c.Closed():
				m.c = nil
			default:
			}
		}
		if m.c == nil {
			c, err := m.Dial()
			if err != nil {
				return err
			}
			m.c, m.folders = c, nil
		}
		c := m.c
		watchdog := time.AfterFunc(opTimeout, func() { c.Close() })
		err := fn(c)
		if !watchdog.Stop() {
			// The watchdog hung up on a silent server; a redial would
			// most likely sit just as long.
			m.c = nil
			return fmt.Errorf("the mailbox did not answer within %s", opTimeout)
		}
		var imapErr *imap.Error
		if err == nil || errors.Is(err, ErrNotFound) || errors.As(err, &imapErr) {
			// The server answered: the connection is fine.
			return err
		}
		c.Close()
		m.c = nil
		if attempt > 0 {
			return err
		}
	}
}

// discover finds the folders worth reading: INBOX, and the junk
// folder by its SPECIAL-USE flag or, failing that, by name.
func (m *IMAP) discover(c *imapclient.Client) ([]folder, error) {
	if m.folders != nil {
		return m.folders, nil
	}
	list, err := c.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("listing folders: %w", err)
	}
	found := []folder{{name: "INBOX"}}
	junk := ""
	for _, mb := range list {
		if slices.Contains(mb.Attrs, imap.MailboxAttrJunk) {
			junk = mb.Mailbox
			break
		}
	}
	if junk == "" {
		for _, mb := range list {
			switch strings.ToLower(mb.Mailbox) {
			case "junk", "spam", "junk e-mail", "[gmail]/spam":
				junk = mb.Mailbox
			}
		}
	}
	if junk != "" && !strings.EqualFold(junk, "INBOX") {
		found = append(found, folder{name: junk, junk: true})
	}
	m.folders = found
	return found, nil
}

// List searches each folder on the server for mail to address, then
// checks every envelope for the exact address: IMAP header search is
// a substring match, and "a@x.com" must never surface "ba@x.com".
func (m *IMAP) List(address string, since time.Time, limit int) ([]Summary, error) {
	var out []Summary
	err := m.do(func(c *imapclient.Client) error {
		out = out[:0]
		folders, err := m.discover(c)
		if err != nil {
			return err
		}
		for _, f := range folders {
			if _, err := c.Select(f.name, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
				var refused *imap.Error
				if f.junk && errors.As(err, &refused) {
					continue // the server says the junk folder is gone
				}
				return fmt.Errorf("opening %s: %w", f.name, err)
			}
			criteria := &imap.SearchCriteria{
				Since: since,
				Or: [][2]imap.SearchCriteria{{
					{Header: []imap.SearchCriteriaHeaderField{{Key: "To", Value: address}}},
					{Header: []imap.SearchCriteriaHeaderField{{Key: "Cc", Value: address}}},
				}},
			}
			data, err := c.UIDSearch(criteria, nil).Wait()
			if err != nil {
				return fmt.Errorf("searching %s: %w", f.name, err)
			}
			uids := data.AllUIDs()
			if len(uids) == 0 {
				continue
			}
			sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
			if len(uids) > limit {
				uids = uids[len(uids)-limit:]
			}
			msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
				UID: true, Envelope: true, InternalDate: true,
			}).Collect()
			if err != nil {
				return fmt.Errorf("reading %s: %w", f.name, err)
			}
			for _, msg := range msgs {
				if msg.Envelope == nil || !sentTo(msg.Envelope, address) {
					continue
				}
				out = append(out, summarize(f, msg))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Get reads one message after proving it was sent to address and is
// inside the window the list shows: --days limits what a key reads,
// not only what the list displays.
func (m *IMAP) Get(address, folderName string, uid uint32, since time.Time) (*Message, error) {
	var msg *Message
	var raw []byte
	err := m.do(func(c *imapclient.Client) error {
		folders, err := m.discover(c)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(folders, func(f folder) bool { return f.name == folderName })
		if i < 0 || uid == 0 {
			return ErrNotFound
		}
		f := folders[i]
		if _, err := c.Select(f.name, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return fmt.Errorf("opening %s: %w", f.name, err)
		}
		set := imap.UIDSetNum(imap.UID(uid))
		heads, err := c.Fetch(set, &imap.FetchOptions{
			UID: true, Envelope: true, InternalDate: true, RFC822Size: true,
		}).Collect()
		if err != nil {
			return err
		}
		if len(heads) != 1 || heads[0].Envelope == nil || !sentTo(heads[0].Envelope, address) {
			return ErrNotFound
		}
		msg = &Message{Summary: summarize(f, heads[0])}
		if msg.Date.Before(since) {
			msg = nil
			return ErrNotFound
		}
		if heads[0].RFC822Size > maxRaw {
			msg.TooLarge = true
			return nil
		}
		// BODY.PEEK[]: reading here must not mark the owner's mail read.
		full, err := c.Fetch(set, &imap.FetchOptions{
			BodySection: []*imap.FetchItemBodySection{{Peek: true}},
		}).Collect()
		if err != nil {
			return err
		}
		if len(full) == 1 && len(full[0].BodySection) == 1 {
			raw = full[0].BodySection[0].Bytes
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Parse after the lock: MIME is CPU work, and the connection every
	// visitor shares must not wait on one sender's message.
	if raw != nil {
		b := parseBody(raw)
		msg.Text, msg.Attachments, msg.TooComplex = b.Text, b.Attachments, b.TooComplex
	}
	return msg, nil
}

// sentTo reports whether address is literally among To or Cc. Bcc is
// not in a delivered message's headers, so mail that only Bcc'd the
// address cannot be attributed to it and is not shown.
func sentTo(env *imap.Envelope, address string) bool {
	for _, list := range [][]imap.Address{env.To, env.Cc} {
		for _, a := range list {
			if strings.EqualFold(a.Addr(), address) {
				return true
			}
		}
	}
	return false
}

func summarize(f folder, msg *imapclient.FetchMessageBuffer) Summary {
	s := Summary{Folder: f.name, Junk: f.junk, UID: uint32(msg.UID), Date: msg.InternalDate}
	if env := msg.Envelope; env != nil {
		s.Subject = clean(env.Subject)
		if !env.Date.IsZero() && s.Date.IsZero() {
			s.Date = env.Date
		}
		if len(env.From) > 0 {
			s.From = displayAddress(env.From[0])
		}
	}
	return s
}

func displayAddress(a imap.Address) string {
	addr := clean(a.Addr())
	name := clean(a.Name)
	switch {
	case name == "" || strings.EqualFold(name, addr):
		return addr
	case addr == "":
		return name
	default:
		return name + " <" + addr + ">"
	}
}
