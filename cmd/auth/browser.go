package auth

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// maxCookieInput bounds what is read from a pipe; a real iCloud
// Cookie header is a few KB.
const maxCookieInput = 1 << 20

const browserSteps = `Sign in at https://www.icloud.com in your browser, then copy its cookies:
  1. Open the developer tools, Network tab, and reload the page.
  2. Click a request to setup.icloud.com.
  3. Under Request Headers, copy the whole value of Cookie.
Paste it here and press Enter (it is not shown):
`

// loginBrowser signs in with a session the browser already has: the
// browser handled the password, the second factor and any device
// approval, ihme only needs the cookies.
func loginBrowser(cmd *cobra.Command, sessPath string) error {
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	if interactive {
		fmt.Print(browserSteps)
	}
	text, err := readCookieInput(os.Stdin, interactive)
	if err != nil {
		return err
	}
	if interactive {
		fmt.Printf("Read %d characters.\n", len(strings.TrimSpace(text)))
	}
	cookies, err := api.ParseBrowserCookies(text)
	if err != nil {
		return err
	}

	client, err := api.NewClientWithSession(&api.SessionData{Cookies: cookies})
	if err != nil {
		return err
	}
	client.Verbose, _ = cmd.Flags().GetBool("verbose")

	fmt.Println("Checking the session with iCloud...")
	if err := client.ImportBrowserSession(); err != nil {
		// %v: see the password path in newCmdLogin.
		return fmt.Errorf("sign-in failed: %v", err)
	}
	if err := confirmHme(client, sessPath, "Logged in as "+client.Session().AppleID+" from your browser session"); err != nil {
		return err
	}
	fmt.Println("Keep that browser signed in: signing out at icloud.com ends this session too.")
	return nil
}

// readCookieInput reads the pasted Cookie header: all of stdin from a
// pipe, one hidden line from a terminal.
func readCookieInput(in *os.File, interactive bool) (string, error) {
	if !interactive {
		data, err := io.ReadAll(io.LimitReader(in, maxCookieInput))
		if err != nil {
			return "", fmt.Errorf("reading cookies: %w", err)
		}
		return string(data), nil
	}
	// Raw mode, not term.ReadPassword: a terminal in line mode keeps
	// at most 1024 bytes of a line on macOS (4096 on Linux), and
	// iCloud's Cookie header is longer, so a paste would be cut short.
	fd := int(in.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return "", fmt.Errorf("reading cookies: %w", err)
	}
	line, err := readHiddenLine(in)
	term.Restore(fd, old)
	fmt.Println()
	return line, err
}

// readHiddenLine reads one line from a terminal in raw mode, where
// the terminal handles no keys: Enter ends the line, Backspace
// deletes, Ctrl-C cancels, Ctrl-D on an empty line is EOF. Bracketed
// paste markers and other control bytes are dropped.
//
// A pasted JSON export spans many lines, so input that starts with
// "[" ends only at a line ending in "]". Stopping at its first line
// would leave the rest in the terminal for the shell to run as
// commands, cookies and all, into the shell history.
func readHiddenLine(r io.Reader) (string, error) {
	br := bufio.NewReader(r)
	var line []byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				break
			}
			return "", fmt.Errorf("reading cookies: %w", err)
		}
		switch b {
		case '\r', '\n':
			if s := strings.TrimSpace(cleanPaste(line)); strings.HasPrefix(s, "[") && !strings.HasSuffix(s, "]") {
				line = append(line, ' ')
				continue
			}
			return cleanPaste(line), nil
		case 0x03:
			return "", cmdutil.ErrCancelled
		case 0x04:
			if len(line) == 0 {
				return "", fmt.Errorf("reading cookies: %w", io.EOF)
			}
		case 0x7f, 0x08:
			if len(line) > 0 {
				line = line[:len(line)-1]
			}
		default:
			line = append(line, b)
		}
	}
	return cleanPaste(line), nil
}

func cleanPaste(line []byte) string {
	s := strings.NewReplacer("\x1b[200~", "", "\x1b[201~", "").Replace(string(line))
	return strings.Map(func(r rune) rune {
		if r < 0x20 {
			return -1
		}
		return r
	}, s)
}
