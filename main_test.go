package main

import (
	"testing"
)

func TestReplaceProfanity(t *testing.T) {

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "no bad words",
			body: "hello world",
			want: "hello world",
		},
		{
			name: "replaces lowercase bad word",
			body: "that was a kerfuffle",
			want: "that was a ****",
		},
		{
			name: "replaces uppercase variant",
			body: "FORNAX is here",
			want: "**** is here",
		},
		{
			name: "does not replace punctuation version",
			body: "sharbert!",
			want: "sharbert!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("replacing %s", tt.body)
			got := ReplaceProfanity(tt.body)
			t.Logf("replaced: %s", got)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
