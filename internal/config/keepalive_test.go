package config

import (
	"testing"
	"time"
)

// MADR 0068 P1 — the kernel-keepalive numbers and the advertised read
// deadline: the blackhole reap itself needs hardware (Part F), so the
// in-CI half pins the config → syscall mapping and the defaulting rules.

func TestKeepaliveNetConfigDefaults(t *testing.T) {
	kc := KeepaliveConfig{}.NetConfig()
	if !kc.Enable {
		t.Fatal("zero-value keepalive must be enabled")
	}
	if kc.Idle != 25*time.Second || kc.Interval != 5*time.Second || kc.Count != 4 {
		t.Fatalf("defaults = %v/%v/%d, want 25s/5s/4", kc.Idle, kc.Interval, kc.Count)
	}
	// 25 + 4×5 = 45 s: the reap horizon must sit inside the default 120 s
	// app deadline, or the kernel layer buys nothing.
	reap := kc.Idle + time.Duration(kc.Count)*kc.Interval
	deadline := time.Duration(Defaults().Limits.WSReadDeadlineSeconds) * time.Second
	if reap >= deadline {
		t.Fatalf("keepalive reap %v not inside app deadline %v", reap, deadline)
	}
}

func TestKeepaliveDisable(t *testing.T) {
	kc := KeepaliveConfig{Disable: true}.NetConfig()
	if kc.Enable {
		t.Fatal("Disable: true must turn probes off")
	}
}

func TestKeepaliveExplicitValues(t *testing.T) {
	kc := KeepaliveConfig{IdleSeconds: 10, IntervalSeconds: 2, Count: 3}.NetConfig()
	if kc.Idle != 10*time.Second || kc.Interval != 2*time.Second || kc.Count != 3 {
		t.Fatalf("explicit values not honoured: %v/%v/%d", kc.Idle, kc.Interval, kc.Count)
	}
}

func TestReadDeadlineResolvedDefaultsAndFloor(t *testing.T) {
	if got := (LimitsConfig{}).Resolved().WSReadDeadlineSeconds; got != 120 {
		t.Fatalf("zero read deadline resolved to %d, want 120", got)
	}
	// The floor exists because the deadline is advertised contract
	// (MADR 0068 D2): below the ping cadence it would reap healthy clients.
	if got := (LimitsConfig{WSReadDeadlineSeconds: 5}).Resolved().WSReadDeadlineSeconds; got != 15 {
		t.Fatalf("sub-floor read deadline resolved to %d, want 15", got)
	}
	if got := (LimitsConfig{WSReadDeadlineSeconds: 120}).Resolved().WSReadDeadlineSeconds; got != 120 {
		t.Fatalf("explicit read deadline resolved to %d, want 120", got)
	}
}
