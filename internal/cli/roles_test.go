package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/store"
)

func TestUserAddDefaultsToAuthor(t *testing.T) {
	e := newEnv(t)
	out := e.must("correct horse battery\n", "user", "add", "grace")
	if !strings.Contains(out, "as author") {
		t.Errorf("user add = %q", out)
	}
	e.must("correct horse battery\n", "user", "add", "ada", "--role", "admin")
	for name, want := range map[string]string{"grace": store.RoleAuthor, "ada": store.RoleAdmin} {
		u, err := e.store.UserByName(context.Background(), name)
		if err != nil || u.Role != want {
			t.Errorf("%s = %+v, %v; want role %s", name, u, err, want)
		}
	}
	list := e.must("", "user", "list")
	if !strings.Contains(list, "grace  author") || !strings.Contains(list, "ada  admin") {
		t.Errorf("user list = %q", list)
	}
	if _, errOut, code := e.run("correct horse battery\n", "user", "add", "bob", "--role", "owner"); code != ExitUsage || !strings.Contains(errOut, "author or admin") {
		t.Errorf("bad role: %d %q", code, errOut)
	}
	if _, err := e.store.UserByName(context.Background(), "bob"); err == nil {
		t.Error("an account with a bad role was created")
	}
}

func TestUserRole(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ada", "--role", "admin")
	e.must("correct horse battery\n", "user", "add", "grace")

	// The last admin cannot be demoted.
	_, errOut, code := e.run("", "user", "role", "ada", "author")
	if code == 0 || !strings.Contains(errOut, "last admin") {
		t.Errorf("demote the last admin: %d %q", code, errOut)
	}
	if u, _ := e.store.UserByName(context.Background(), "ada"); u.Role != store.RoleAdmin {
		t.Errorf("ada is %s after a refused demotion", u.Role)
	}

	if out := e.must("", "user", "role", "grace", "admin"); !strings.Contains(out, "grace is now admin (was author)") {
		t.Errorf("promote = %q", out)
	}
	if out := e.must("", "user", "role", "grace", "admin"); !strings.Contains(out, "already admin") {
		t.Errorf("repeat = %q", out)
	}
	e.must("", "user", "role", "ada", "author") // grace is an admin now

	if _, errOut, code := e.run("", "user", "role", "nobody", "admin"); code == 0 || !strings.Contains(errOut, "no account called nobody") {
		t.Errorf("unknown account: %d %q", code, errOut)
	}
	if _, errOut, code := e.run("", "user", "role", "ada", "owner"); code != ExitUsage || !strings.Contains(errOut, "admin or author") {
		t.Errorf("unknown role: %d %q", code, errOut)
	}
}

func TestAuditCommand(t *testing.T) {
	e := newEnv(t)
	if out := e.must("", "audit"); !strings.Contains(out, "audit log is empty") {
		t.Errorf("empty audit = %q", out)
	}
	e.must("correct horse battery\n", "user", "add", "ada", "--role", "admin")
	e.must("correct horse battery\n", "user", "add", "grace")
	e.must("", "user", "role", "grace", "admin")
	e.must("another long passphrase\n", "user", "passwd", "grace")

	out := e.must("", "audit")
	for _, want := range []string{"host", "account.create", "ada as admin", "grace as author", "role.change", "grace: author to admin", "password.change"} {
		if !strings.Contains(out, want) {
			t.Errorf("audit output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "correct horse") || strings.Contains(out, "another long") {
		t.Errorf("audit output holds a password:\n%s", out)
	}
	if strings.Index(out, "password.change") > strings.Index(out, "account.create") {
		t.Errorf("audit is not newest first:\n%s", out)
	}

	var page struct {
		Items []struct {
			Action string `json:"action"`
			Actor  string `json:"actor"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(e.must("", "audit", "--json", "--limit", "2")), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || len(page.Items) != 2 || page.Items[0].Action != "password.change" || page.Items[0].Actor != "host" {
		t.Errorf("audit --json = %+v", page)
	}
	if out := e.must("", "audit", "--limit", "1"); !strings.Contains(out, "Showing 1 of 4") {
		t.Errorf("paged audit = %q", out)
	}
	if _, _, code := e.run("", "audit", "--limit", "0"); code != ExitUsage {
		t.Errorf("limit 0 exit = %d", code)
	}
}
