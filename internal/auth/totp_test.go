package auth

import (
	"encoding/json"
	"strings"
	"testing"
)

// Recovery codes used to be stored in cleartext beside the AES-GCM-encrypted
// secret, which handed anyone with a copy of the database eight working second
// factors per user. New code sets are stored hashed; sheets printed before the
// change still have to work.
func TestRecoveryEntriesAreHashedNotCleartext(t *testing.T) {
	codes := generateRecoveryCodes(4)
	entries := toRecoveryEntries(codes)

	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	stored := string(b)
	for _, c := range codes {
		if strings.Contains(stored, c) {
			t.Errorf("recovery code %q appears verbatim in the stored JSON", c)
		}
	}
	for i, e := range entries {
		if e.Code != "" {
			t.Errorf("entry %d still carries a cleartext Code", i)
		}
		if e.Hash != hashRecoveryCode(codes[i]) {
			t.Errorf("entry %d hash does not match its code", i)
		}
	}
}

func TestGenerateRecoveryCodesAreDistinct(t *testing.T) {
	codes := generateRecoveryCodes(16)
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 10 {
			t.Errorf("code %q has length %d, want 10", c, len(c))
		}
		if seen[c] {
			t.Errorf("duplicate recovery code %q", c)
		}
		seen[c] = true
	}
}

// Legacy rows carry Code and no Hash; the verifier has to keep accepting them.
func TestRecoveryEntryLegacyShapeStillParses(t *testing.T) {
	var entries []recoveryEntry
	if err := json.Unmarshal([]byte(`[{"code":"ABCDE12345","used":false}]`), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Code != "ABCDE12345" || entries[0].Hash != "" {
		t.Fatalf("legacy entry did not round-trip: %+v", entries)
	}
}
