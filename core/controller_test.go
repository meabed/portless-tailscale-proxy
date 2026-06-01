package core

import (
	"testing"
	"time"
)

func TestControllerStartStop(t *testing.T) {
	// Fake runner: empty lsof (no services), tailscale calls succeed (no-op).
	c := newControllerWithRunner(&fakeRunner{})
	o := Options{Ports: "3000-5000", Bind: "127.0.0.1", Port: 18991, Interval: 1, ProxyOnly: true}

	if err := c.Start(o); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !c.Running() {
		t.Fatal("expected running after Start")
	}
	if st := c.Status(); !st.Running || st.Port != 18991 {
		t.Fatalf("bad status: %+v", st)
	}
	// Second start must fail (already running).
	if err := c.Start(o); err == nil {
		t.Fatal("expected error starting an already-running controller")
	}
	if err := c.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if c.Running() {
		t.Fatal("expected not running after Stop")
	}
	// Stop is idempotent.
	if err := c.Stop(); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestControllerInvalidPorts(t *testing.T) {
	c := newControllerWithRunner(&fakeRunner{})
	if err := c.Start(Options{Ports: "not-a-range", Port: 18992, ProxyOnly: true}); err == nil {
		t.Fatal("expected error for invalid port range")
	}
	if c.Running() {
		t.Fatal("must not be running after a failed Start")
	}
}

func TestControllerOnChangeFires(t *testing.T) {
	c := newControllerWithRunner(&fakeRunner{})
	ch := make(chan struct{}, 8)
	c.OnChange(func() { ch <- struct{}{} })
	if err := c.Start(Options{Ports: "3000-5000", Port: 18993, Interval: 1, ProxyOnly: true}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer c.Stop()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("OnChange did not fire after Start")
	}
}
