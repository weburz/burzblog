package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// deployDelay batches a burst of saves into one rebuild.
var deployDelay = 5 * time.Second

// deployer POSTs the reader site's deploy hook (for example a Vercel deploy
// hook) after published content changes. The URL is a secret and is never
// returned by the API.
type deployer struct {
	url    string
	log    *slog.Logger
	client *http.Client

	mu         sync.Mutex
	timer      *time.Timer
	pending    bool
	lastAt     *time.Time
	lastStatus string
	lastError  string
	wg         sync.WaitGroup
}

func newDeployer(url string, log *slog.Logger) *deployer {
	return &deployer{url: url, log: log, client: &http.Client{
		Timeout: 15 * time.Second,
		// A redirect would send the request somewhere the operator did not
		// configure; report it instead.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (d *deployer) configured() bool {
	return d.url != ""
}

// schedule asks for a rebuild soon, restarting the delay on every call.
func (d *deployer) schedule() {
	if !d.configured() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending = true
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(deployDelay, d.fire)
}

// now asks for a rebuild immediately.
func (d *deployer) now() {
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
	}
	d.pending = true
	d.mu.Unlock()
	go d.fire()
}

func (d *deployer) fire() {
	d.wg.Add(1)
	defer d.wg.Done()
	d.mu.Lock()
	if !d.pending {
		d.mu.Unlock()
		return
	}
	d.pending = false
	d.mu.Unlock()

	status, errText := "ok", ""
	resp, err := d.client.Post(d.url, "application/json", nil)
	if err != nil {
		// The error text can contain the hook URL; keep it out of the API.
		status, errText = "failed", "The deploy hook could not be reached."
		// *url.Error prints the URL, which is a secret; log only the cause.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		d.log.Warn("deploy hook failed", "err", err)
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			status, errText = "failed", fmt.Sprintf("The deploy hook answered HTTP %d.", resp.StatusCode)
			d.log.Warn("deploy hook refused", "status", resp.StatusCode)
		}
	}
	now := time.Now().UTC()
	d.mu.Lock()
	d.lastAt, d.lastStatus, d.lastError = &now, status, errText
	d.mu.Unlock()
}

type deployStatus struct {
	Configured bool       `json:"configured"`
	Pending    bool       `json:"pending"`
	LastAt     *time.Time `json:"lastAt"`
	LastStatus string     `json:"lastStatus"`
	LastError  string     `json:"lastError"`
}

func (d *deployer) status() deployStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return deployStatus{Configured: d.configured(), Pending: d.pending, LastAt: d.lastAt, LastStatus: d.lastStatus, LastError: d.lastError}
}

// close sends a pending rebuild before shutdown and waits for it.
func (d *deployer) close(ctx context.Context) {
	d.mu.Lock()
	pending := d.pending
	if d.timer != nil {
		d.timer.Stop()
	}
	d.mu.Unlock()
	if pending {
		d.fire()
	}
	done := make(chan struct{})
	go func() { d.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
