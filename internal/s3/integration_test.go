package s3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// TestingClient connects to the S3-compatible server in
// BURZBLOG_TEST_S3_ENDPOINT (with BURZBLOG_TEST_S3_ACCESS_KEY_ID and
// BURZBLOG_TEST_S3_SECRET_ACCESS_KEY) and makes the bucket, or skips the test
// when it is not set. A local MinIO does:
//
//	docker run -d -p 127.0.0.1:9100:9000 -e MINIO_ROOT_USER=burzblogtest \
//	  -e MINIO_ROOT_PASSWORD=burzblogtest-secret minio/minio server /data
//	BURZBLOG_TEST_S3_ENDPOINT=http://127.0.0.1:9100 BURZBLOG_TEST_S3_ACCESS_KEY_ID=burzblogtest \
//	  BURZBLOG_TEST_S3_SECRET_ACCESS_KEY=burzblogtest-secret go test ./internal/s3/ ./internal/snapshot/
func TestingClient(t testing.TB, bucket string) *Client {
	t.Helper()
	endpoint := os.Getenv("BURZBLOG_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set BURZBLOG_TEST_S3_ENDPOINT to test against a real S3-compatible server")
	}
	c, err := New(Config{Endpoint: endpoint, Region: os.Getenv("BURZBLOG_TEST_S3_REGION"), Bucket: bucket,
		AccessKeyID: os.Getenv("BURZBLOG_TEST_S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("BURZBLOG_TEST_S3_SECRET_ACCESS_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.MakeBucket(context.Background()); err != nil {
		t.Fatalf("make bucket %s: %v", bucket, err)
	}
	return c
}

func put(t *testing.T, c *Client, key, body string) {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	if err := c.Put(context.Background(), key, strings.NewReader(body), int64(len(body)), hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
}

func TestAgainstARealServer(t *testing.T) {
	c := TestingClient(t, "burzblog-s3-test")
	ctx := context.Background()
	prefix := fmt.Sprintf("run-%d/", os.Getpid())

	keys := []string{prefix + "plain.txt", prefix + "with space+plus/ünïcode~.txt", prefix + "snapshots/burzblog-snapshot-20261028T031700.000Z.tar.gz.enc"}
	for _, k := range keys {
		put(t, c, k, "body of "+k)
	}
	for _, k := range keys {
		rc, err := c.Get(ctx, k)
		if err != nil {
			t.Fatalf("get %s: %v", k, err)
		}
		got, _ := io.ReadAll(rc)
		rc.Close()
		if string(got) != "body of "+k {
			t.Errorf("get %s = %q", k, got)
		}
		if ok, err := c.Exists(ctx, k); !ok || err != nil {
			t.Errorf("exists %s = %v, %v", k, ok, err)
		}
	}
	if _, err := c.Get(ctx, prefix+"missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing object: %v", err)
	}

	// More than one page of listing.
	for i := range 1003 {
		put(t, c, fmt.Sprintf("%smany/%04d", prefix, i), "x")
	}
	list, err := c.List(ctx, prefix+"many/")
	if err != nil || len(list) != 1003 || list[1002].Key != prefix+"many/1002" || list[0].Size != 1 {
		t.Fatalf("list = %d objects, %v", len(list), err)
	}

	for _, o := range append(list, Object{Key: keys[0]}, Object{Key: keys[1]}, Object{Key: keys[2]}) {
		if err := c.Delete(ctx, o.Key); err != nil {
			t.Fatalf("delete %s: %v", o.Key, err)
		}
	}
	if left, err := c.List(ctx, prefix); err != nil || len(left) != 0 {
		t.Errorf("after deleting: %v, %v", left, err)
	}

	// A wrong secret is refused with a message that says what to check.
	bad := *c
	bad.cfg.SecretAccessKey = "not-the-secret"
	bad.retryWait = 0
	err = bad.Put(ctx, prefix+"x", strings.NewReader("x"), 1, EmptySHA256)
	var se *Error
	if !errors.As(err, &se) || !strings.Contains(err.Error(), "check the access key") || strings.Contains(err.Error(), "not-the-secret") {
		t.Errorf("wrong secret: %v", err)
	}
	// A body that does not match its declared checksum is refused.
	err = c.Put(ctx, prefix+"y", strings.NewReader("y"), 1, EmptySHA256)
	if err == nil {
		t.Error("the server accepted a body whose SHA-256 did not match")
		c.Delete(ctx, prefix+"y")
	}
}
