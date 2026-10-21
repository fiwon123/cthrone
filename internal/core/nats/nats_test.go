package natscore

import (
	"testing"
)

func TestSubjectFrom(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no argument uses the default",
			args: nil,
			want: DefaultSubject,
		},
		{
			name: "an empty argument uses the default",
			args: []string{""},
			want: DefaultSubject,
		},
		{
			name: "the argument is the subject",
			args: []string{"room-42"},
			want: "room-42",
		},
		{
			name: "only the first argument is the subject",
			args: []string{"room-42", "extra"},
			want: "room-42",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := subjectFrom(tc.args); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBrokerURL(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		want       string
	}{
		{
			name:       "an empty value uses the default broker",
			configured: "",
			want:       DefaultBrokerURL,
		},
		{
			name:       "a configured broker is used",
			configured: "nats://broker.internal:4222",
			want:       "nats://broker.internal:4222",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := brokerURL(tc.configured); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
