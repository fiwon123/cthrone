package wire

import "testing"

// The name in a header is chosen by the peer that sends it, and it becomes a
// file on this disk. Every spelling that could walk out of the folder it is
// written into has to be refused at the door.
func TestCheckNameRefusesPaths(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"photo.jpg", true},
		{"a b.txt", true},
		{"..hidden", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../escape.txt", false},
		{"..\\escape.txt", false},
		{"dir/file.txt", false},
		{"dir\\file.txt", false},
		{"/etc/passwd", false},
		{"C:\\windows\\evil.exe", false},
		{"a\x00b", false},
	}

	for _, tt := range tests {
		err := checkName(tt.name)
		if (err == nil) != tt.ok {
			t.Errorf("checkName(%q) error = %v, want ok=%v", tt.name, err, tt.ok)
		}
	}
}
