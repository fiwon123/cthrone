package ui

import (
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/e2e"
)

// The first-contact texts are the only thing a person has to decide with, so
// each one has to carry the code, say whose code it is, and survive a device
// that announced no name and a key with no fingerprint.
func TestFirstContactText(t *testing.T) {
	tests := []struct {
		name    string
		contact e2e.FirstContact
		want    []string
		notWant []string
	}{
		{
			name:    "a named device with a fingerprint",
			contact: e2e.FirstContact{Name: "phone", Fingerprint: "SHA256:abc", Code: "123 456"},
			want:    []string{"phone wants to connect", "123 456", "SHA256:abc", "must agree"},
		},
		{
			name:    "an unnamed device says so instead of naming nobody",
			contact: e2e.FirstContact{Code: "123 456"},
			want:    []string{"An unnamed device wants to connect", "123 456"},
			notWant: []string{"  wants to connect", "fingerprint is ."},
		},
		{
			name:    "a contact without a fingerprint leaves the line out",
			contact: e2e.FirstContact{Name: "phone", Code: "999 000"},
			want:    []string{"phone wants to connect", "999 000"},
			notWant: []string{"fingerprint"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstContactText(tt.contact)

			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("firstContactText(%+v) = %q, want it to contain %q", tt.contact, got, want)
				}
			}

			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("firstContactText(%+v) = %q, want it not to contain %q", tt.contact, got, notWant)
				}
			}
		})
	}
}

// The waiting line is what the side that is not asked anything shows, so it has
// to name who is deciding and repeat the code that decides it.
func TestWaitingText(t *testing.T) {
	tests := []struct {
		name    string
		contact e2e.FirstContact
		want    []string
		notWant []string
	}{
		{
			name:    "a named device",
			contact: e2e.FirstContact{Name: "laptop", Code: "123 456"},
			want:    []string{"Waiting for laptop to accept", "123 456", "must match"},
		},
		{
			name:    "an unnamed device is still somebody to wait for",
			contact: e2e.FirstContact{Code: "123 456"},
			want:    []string{"Waiting for the other device to accept", "123 456"},
			notWant: []string{"Waiting for  to accept"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := waitingText(tt.contact)

			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("waitingText(%+v) = %q, want it to contain %q", tt.contact, got, want)
				}
			}

			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("waitingText(%+v) = %q, want it not to contain %q", tt.contact, got, notWant)
				}
			}
		})
	}
}

// contactName is what a status line calls the device that asked, and a status
// line with a hole in it reads as a bug.
func TestContactName(t *testing.T) {
	if got := contactName(e2e.FirstContact{Name: "phone"}); got != "phone" {
		t.Errorf("contactName(named) = %q, want phone", got)
	}

	if got := contactName(e2e.FirstContact{}); got != "an unnamed device" {
		t.Errorf("contactName(unnamed) = %q, want an unnamed device", got)
	}
}
