package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/weburz/burzblog/internal/client"
)

// Exit codes. Scripts may rely on them; they are documented in
// docs/terminal.md, including the failures that used to exit 1.
const (
	ExitOK         = 0 // success
	ExitError      = 1 // any other failure
	ExitUsage      = 2 // wrong flags or arguments
	ExitConflict   = 3 // the post changed since its base version, or the slug is taken
	ExitNotFound   = 4 // the post does not exist
	ExitAuth       = 5 // not signed in, or the token or password is not accepted
	ExitValidation = 6 // the server rejected the content
	ExitUpdate     = 7 // upgrade --check found a newer release
)

// exitError is a failure with a stable machine-readable code and exit code.
// It carries only text, so an API error it was made from cannot show through.
type exitError struct {
	code string
	exit int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

var errNotSignedIn = &exitError{"authentication", ExitAuth,
	"not signed in. Run `burzblog login https://your-blog.example.com` first, or set BURZBLOG_SERVER and BURZBLOG_TOKEN"}

func usagef(format string, args ...any) error {
	return &exitError{"usage", ExitUsage, fmt.Sprintf(format, args...)}
}

func conflictf(format string, args ...any) error {
	return &exitError{"conflict", ExitConflict, fmt.Sprintf(format, args...)}
}

// reword keeps the code and exit code of err but shows msg instead.
func reword(err error, msg string) error {
	code, exit := classify(err)
	return &exitError{code, exit, msg}
}

// classify returns the machine-readable code and the exit code for err.
func classify(err error) (string, int) {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code, ee.exit
	}
	var ae *client.Error
	if errors.As(err, &ae) {
		switch ae.Code {
		case "conflict", "slug_taken":
			return ae.Code, ExitConflict
		case "not_found":
			return ae.Code, ExitNotFound
		case "authentication", "credentials":
			return ae.Code, ExitAuth
		case "validation":
			return ae.Code, ExitValidation
		}
		return ae.Code, ExitError
	}
	return "error", ExitError
}

// jsonFailure is the error document printed to stderr with --json.
type jsonFailure struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	} `json:"error"`
}

func (a *App) printJSONFailure(err error) {
	var f jsonFailure
	f.Error.Code, _ = classify(err)
	var ae *client.Error
	if errors.As(err, &ae) {
		f.Error.Message, f.Error.Detail = ae.Message, ae.Detail
		if ae.Status == 401 && ae.Code == "authentication" && ae.Detail == "" {
			f.Error.Detail = "Run `burzblog login <server>` to sign in again, or set a valid BURZBLOG_TOKEN."
		}
	} else {
		// Hints follow the first line of a message; they become the detail.
		msg, rest, _ := strings.Cut(err.Error(), "\n")
		lines := strings.Split(rest, "\n")
		for i := range lines {
			lines[i] = strings.TrimSpace(lines[i])
		}
		f.Error.Message, f.Error.Detail = msg, strings.TrimSpace(strings.Join(lines, "\n"))
	}
	_ = encodeJSON(a.Err, f)
}

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// emit prints v as one line of JSON on standard output.
func (a *App) emit(v any) error {
	return encodeJSON(a.Out, v)
}
