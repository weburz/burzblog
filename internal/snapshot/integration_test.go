package snapshot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/s3"
)

// realRemote is a Remote on the S3-compatible server in
// BURZBLOG_TEST_S3_ENDPOINT (see internal/s3's integration test), or skips.
func realRemote(t *testing.T) Remote {
	t.Helper()
	endpoint := os.Getenv("BURZBLOG_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set BURZBLOG_TEST_S3_ENDPOINT to test against a real S3-compatible server")
	}
	c, err := s3.New(s3.Config{Endpoint: endpoint, Region: os.Getenv("BURZBLOG_TEST_S3_REGION"), Bucket: "burzblog-snapshot-test",
		AccessKeyID: os.Getenv("BURZBLOG_TEST_S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("BURZBLOG_TEST_S3_SECRET_ACCESS_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.MakeBucket(context.Background()); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("blog-%d-%d/", os.Getpid(), time.Now().UnixNano())
	r := Remote{Bucket: c, Prefix: prefix, Label: "s3://burzblog-snapshot-test/" + prefix}
	t.Cleanup(func() {
		list, _ := c.List(context.Background(), prefix)
		for _, o := range list {
			c.Delete(context.Background(), o.Key)
		}
	})
	return r
}

func TestRemoteAgainstARealServer(t *testing.T) {
	r := realRemote(t)
	b := newBlog(t)
	ctx := context.Background()
	c := &clock{t: time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)}
	first, err := CreateRemote(ctx, b.st, b.dir, r, 2, remoteOpts(c))
	if err != nil || first.Sent != 3 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	b.upload(t, []byte("%PDF-1.4 new upload"))
	for range 2 {
		res, err := CreateRemote(ctx, b.st, b.dir, r, 2, remoteOpts(c))
		if err != nil {
			t.Fatal(err)
		}
		if res.Sent > 1 {
			t.Errorf("sent %d uploads again", res.Sent)
		}
	}
	list, err := r.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("after keep 2: %+v, %v", list, err)
	}
	target, err := restoreRemote(t, r, "")
	if err != nil {
		t.Fatal(err)
	}
	checkRestored(t, b, target)
	objects, _ := r.Bucket.List(ctx, r.Prefix)
	for _, o := range objects {
		if !strings.HasSuffix(o.Key, ".enc") && !strings.HasSuffix(o.Key, ".manifest") {
			t.Errorf("%s is not encrypted", o.Key)
		}
	}
}
