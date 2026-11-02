package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/weburz/burzblog/internal/s3"
	"github.com/weburz/burzblog/internal/snapshot"
)

// isRemote reports whether a snapshot target is in object storage.
func isRemote(target string) bool { return strings.HasPrefix(target, "s3://") }

// remote opens the bucket a target such as s3://bucket/blog/ names, with the
// BURZBLOG_S3_* settings, and returns the snapshot name when the target ends
// in snapshots/<name>.
func (a *App) remote(target string) (snapshot.Remote, string, error) {
	rest := strings.TrimPrefix(target, "s3://")
	bucket, prefix, _ := strings.Cut(rest, "/")
	name := ""
	if dir, last, ok := cutLast(prefix, "/"); ok && snapshot.IsName(last) && strings.HasSuffix(dir+"/", "snapshots/") {
		prefix, name = strings.TrimSuffix(dir+"/", "snapshots/"), last
	}
	prefix = strings.Trim(prefix, "/")
	if strings.Contains(prefix, "//") {
		return snapshot.Remote{}, "", usagef("the prefix in %s has an empty part (//)", target)
	}
	if prefix != "" {
		prefix += "/"
	}
	endpoint := a.Getenv("BURZBLOG_S3_ENDPOINT")
	if endpoint == "" {
		return snapshot.Remote{}, "", usagef("set BURZBLOG_S3_ENDPOINT to the storage address, for example https://<account>.r2.cloudflarestorage.com, and BURZBLOG_S3_ACCESS_KEY_ID and BURZBLOG_S3_SECRET_ACCESS_KEY")
	}
	virtual := false
	switch v := a.Getenv("BURZBLOG_S3_VIRTUAL_HOSTED"); v {
	case "", "false", "0":
	case "true", "1":
		virtual = true
	default:
		return snapshot.Remote{}, "", fmt.Errorf("BURZBLOG_S3_VIRTUAL_HOSTED must be true or false, got %q", v)
	}
	secret, err := a.secretSetting("BURZBLOG_S3_SECRET_ACCESS_KEY")
	if err != nil {
		return snapshot.Remote{}, "", err
	}
	client, err := s3.New(s3.Config{
		Endpoint: endpoint, Region: a.Getenv("BURZBLOG_S3_REGION"), Bucket: bucket,
		AccessKeyID: a.Getenv("BURZBLOG_S3_ACCESS_KEY_ID"), SecretAccessKey: secret,
		VirtualHosted: virtual,
	})
	if err != nil {
		return snapshot.Remote{}, "", err
	}
	return snapshot.Remote{Bucket: client, Prefix: prefix, Label: "s3://" + bucket + "/" + prefix}, name, nil
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return "", s, s != ""
	}
	return s[:i], s[i+len(sep):], true
}

// secretSetting reads the setting name, or the file name_FILE names (for
// Docker and systemd secrets); setting both is refused.
func (a *App) secretSetting(name string) (string, error) {
	v, file := a.Getenv(name), a.Getenv(name+"_FILE")
	switch {
	case v != "" && file != "":
		return "", fmt.Errorf("set %s or %s_FILE, not both", name, name)
	case file == "":
		return v, nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read %s_FILE: %w", name, err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// snapshotPassphrase reads BURZBLOG_SNAPSHOT_PASSPHRASE, or the file
// BURZBLOG_SNAPSHOT_PASSPHRASE_FILE names (for Docker and systemd secrets),
// or file when it is set. It returns "" when none is.
func (a *App) snapshotPassphrase(file string) (string, error) {
	if file == "" {
		file = a.Getenv("BURZBLOG_SNAPSHOT_PASSPHRASE_FILE")
		if v := a.Getenv("BURZBLOG_SNAPSHOT_PASSPHRASE"); v != "" {
			if file != "" {
				return "", errors.New("set BURZBLOG_SNAPSHOT_PASSPHRASE or BURZBLOG_SNAPSHOT_PASSPHRASE_FILE, not both")
			}
			return v, nil
		}
	}
	if file == "" {
		return "", nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read the passphrase file: %w", err)
	}
	p := strings.TrimRight(string(b), "\r\n")
	if p == "" {
		return "", fmt.Errorf("the passphrase file %s is empty", file)
	}
	return p, nil
}
