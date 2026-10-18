package main

import "testing"

func TestScheduleIntervalMinutes(t *testing.T) {
	if got := (scheduleSettings{}).intervalMinutes(); got != defaultSyncIntervalMinutes {
		t.Fatalf("empty = %d, want %d", got, defaultSyncIntervalMinutes)
	}
	if got := (scheduleSettings{IntervalMinutes: 3}).intervalMinutes(); got != defaultSyncIntervalMinutes {
		t.Fatalf("below min = %d, want default", got)
	}
	if got := (scheduleSettings{IntervalMinutes: 15}).intervalMinutes(); got != 15 {
		t.Fatalf("valid = %d", got)
	}
}
