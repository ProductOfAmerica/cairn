package ids_test

import (
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/ids"
)

func TestValidateOpID(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"01HNBXBT9J6MGK3Z5R7WVXTM0P", false}, // 26-char ULID
		{"01HNBXBT9J6MGK3Z5R7WVXTM0", true},   // 25 chars
		{"01HNBXBT9J6MGK3Z5R7WVXTM0PZ", true}, // 27 chars
		{"01hnbxbt9j6mgk3z5r7wvxtm0p", true},  // lowercase not allowed (ULID is uppercase Crockford)
		{"", true},
		{"deadbeef", true},
	}
	for _, tc := range cases {
		err := ids.ValidateOpID(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("%q: got err=%v want err=%v", tc.in, err, tc.wantErr)
		}
	}
}
