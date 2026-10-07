package inbox

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

const alias = "brisk.heron_0q@icloud.com"

type fixture struct {
	m    *IMAP
	user *imapmemserver.User
	addr string
	uids map[string]imap.UID // message name -> UID in its folder
}

func rfc822(headers map[string]string, body string) []byte {
	var b bytes.Buffer
	for _, k := range []string{"From", "To", "Cc", "Subject", "Date", "MIME-Version", "Content-Type", "Content-Transfer-Encoding"} {
		if v, ok := headers[k]; ok {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return b.Bytes()
}

func newFixture(t *testing.T, now time.Time) *fixture {
	t.Helper()
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("me@icloud.com", "app-pass")
	mem.AddUser(user)
	for _, name := range []string{"INBOX", "Junk", "Sent Messages"} {
		if err := user.Create(name, nil); err != nil {
			t.Fatal(err)
		}
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}},
		InsecureAuth: true,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	f := &fixture{user: user, addr: ln.Addr().String(), uids: map[string]imap.UID{}}
	f.m = &IMAP{Dial: func() (*imapclient.Client, error) {
		c, err := imapclient.DialInsecure(f.addr, ClientOptions())
		if err != nil {
			return nil, err
		}
		if err := c.Login("me@icloud.com", "app-pass").Wait(); err != nil {
			c.Close()
			return nil, err
		}
		return c, nil
	}}

	add := func(name, folder string, at time.Time, headers map[string]string, body string) {
		headers["From"] = "Shop <noreply@shop.example>"
		headers["Date"] = at.Format(time.RFC1123Z)
		data, err := user.Append(folder, bytes.NewReader(rfc822(headers, body)), &imap.AppendOptions{Time: at})
		if err != nil {
			t.Fatal(err)
		}
		f.uids[name] = data.UID
	}
	add("plain", "INBOX", now.Add(-2*time.Hour), map[string]string{"To": alias, "Subject": "Your code"}, "Your code is 111111.\n")
	add("other", "INBOX", now.Add(-90*time.Minute), map[string]string{"To": "other_1x@icloud.com", "Subject": "Private"}, "not yours\n")
	add("substring", "INBOX", now.Add(-80*time.Minute), map[string]string{"To": "x" + alias, "Subject": "Near miss"}, "not yours either\n")
	add("cc", "INBOX", now.Add(-time.Hour), map[string]string{
		"To": "someone@example.com", "Cc": "Me <" + strings.ToUpper(alias) + ">", "Subject": "Both parts",
		"MIME-Version": "1.0", "Content-Type": `multipart/alternative; boundary="b"`,
	}, "--b\nContent-Type: text/plain; charset=utf-8\n\nplain wins\n--b\nContent-Type: text/html\n\n<p>html loses</p>\n--b--\n")
	add("junk", "Junk", now.Add(-30*time.Minute), map[string]string{
		"To": alias, "Subject": "=?GBK?B?0enWpMLr?=",
		"MIME-Version": "1.0", "Content-Type": "text/html; charset=GBK",
	}, "<p>\xd1\xe9\xd6\xa4\xc2\xeb 123456</p>")
	add("old", "INBOX", now.AddDate(0, 0, -40), map[string]string{"To": alias, "Subject": "Ancient"}, "old\n")
	add("sent", "Sent Messages", now.Add(-10*time.Minute), map[string]string{"To": alias, "Subject": "Owner wrote this"}, "sent\n")
	return f
}

func TestIMAPListShowsOnlyThisAddress(t *testing.T) {
	now := time.Now()
	f := newFixture(t, now)
	list, err := f.m.List(alias, now.AddDate(0, 0, -30), 50)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, s := range list {
		subjects = append(subjects, s.Subject)
	}
	want := []string{"验证码", "Both parts", "Your code"}
	if !slices.Equal(subjects, want) {
		t.Fatalf("subjects = %q, want %q (newest first, exact address, junk included)", subjects, want)
	}
	if !list[0].Junk || list[0].Folder != "Junk" || list[1].Junk {
		t.Errorf("junk flags wrong: %+v", list[:2])
	}
	if list[2].From != "Shop <noreply@shop.example>" {
		t.Errorf("from = %q", list[2].From)
	}

	capped, err := f.m.List(alias, now.AddDate(0, 0, -30), 1)
	if err != nil || len(capped) != 1 || capped[0].Subject != "验证码" {
		t.Errorf("limit 1 = %+v, %v", capped, err)
	}
}

func TestIMAPGetRefusesOtherMail(t *testing.T) {
	now := time.Now()
	f := newFixture(t, now)
	for _, tc := range []struct{ folder, name string }{
		{"INBOX", "other"},
		{"INBOX", "substring"},
		{"Sent Messages", "sent"},
		{"INBOX", "nonexistent"},
	} {
		uid := uint32(f.uids[tc.name])
		if tc.name == "nonexistent" {
			uid = 9999
		}
		if _, err := f.m.Get(alias, tc.folder, uid, time.Time{}); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s/%s: err = %v, want ErrNotFound", tc.folder, tc.name, err)
		}
	}
}

func TestIMAPGetReadsWithoutMarkingSeen(t *testing.T) {
	now := time.Now()
	f := newFixture(t, now)
	m, err := f.m.Get(alias, "INBOX", uint32(f.uids["cc"]), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Text != "plain wins" {
		t.Errorf("text = %q, want the plain part", m.Text)
	}

	junk, err := f.m.Get(alias, "Junk", uint32(f.uids["junk"]), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if junk.Text != "验证码 123456" || junk.Subject != "验证码" || !junk.Junk {
		t.Errorf("GBK message = %+v", junk)
	}

	c, err := f.m.Dial()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	msgs, err := c.Fetch(imap.UIDSetNum(f.uids["cc"]), &imap.FetchOptions{Flags: true}).Collect()
	if err != nil || len(msgs) != 1 {
		t.Fatal(err)
	}
	if slices.Contains(msgs[0].Flags, imap.FlagSeen) {
		t.Error("viewing a message marked it read in the owner's mailbox")
	}
}

// --days bounds what a key can read, not only what the list shows:
// sharing a years-old address must not open its whole history.
func TestIMAPGetEnforcesWindow(t *testing.T) {
	now := time.Now()
	f := newFixture(t, now)
	if _, err := f.m.Get(alias, "INBOX", uint32(f.uids["old"]), now.AddDate(0, 0, -30)); !errors.Is(err, ErrNotFound) {
		t.Errorf("a message outside the window opened: %v", err)
	}
	if _, err := f.m.Get(alias, "INBOX", uint32(f.uids["old"]), time.Time{}); err != nil {
		t.Errorf("the same message with no window: %v", err)
	}
}

func TestIMAPRedialsDeadConnection(t *testing.T) {
	now := time.Now()
	f := newFixture(t, now)
	if err := f.m.Check(); err != nil {
		t.Fatal(err)
	}
	f.m.c.Close() // the server hung up while idle
	if _, err := f.m.List(alias, now.AddDate(0, 0, -30), 50); err != nil {
		t.Errorf("no redial after a dropped connection: %v", err)
	}
}

func TestIMAPWrongPasswordFailsCheck(t *testing.T) {
	f := newFixture(t, time.Now())
	m := &IMAP{Dial: func() (*imapclient.Client, error) {
		c, err := imapclient.DialInsecure(f.addr, nil)
		if err != nil {
			return nil, err
		}
		if err := c.Login("me@icloud.com", "wrong").Wait(); err != nil {
			c.Close()
			return nil, err
		}
		return c, nil
	}}
	err := m.Check()
	if err == nil {
		t.Fatal("a wrong password passed the startup check")
	}
	// serve gives the credential hint only for a server's "no".
	var refused *imap.Error
	if !errors.As(err, &refused) {
		t.Errorf("a refused login is not an *imap.Error: %v", err)
	}
}
