package msg

import "testing"

func TestValidHasReadSeq(t *testing.T) {
	tests := []struct {
		name       string
		hasReadSeq int64
		maxSeq     int64
		want       int64
	}{
		{name: "valid", hasReadSeq: 367, maxSeq: 500, want: 367},
		{name: "at maximum", hasReadSeq: 500, maxSeq: 500, want: 500},
		{name: "beyond maximum", hasReadSeq: 500, maxSeq: 367, want: 367},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validHasReadSeq(tt.hasReadSeq, tt.maxSeq); got != tt.want {
				t.Fatalf("validHasReadSeq(%d, %d) = %d, want %d", tt.hasReadSeq, tt.maxSeq, got, tt.want)
			}
		})
	}
}
