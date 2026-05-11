package auth

import "testing"

func TestHashVerify(t *testing.T) {
	h, err := HashPassword("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword("hunter2", h)
	if err != nil || !ok {
		t.Fatalf("verify good: ok=%v err=%v", ok, err)
	}
	ok, _ = VerifyPassword("wrong", h)
	if ok {
		t.Error("verify wrong should fail")
	}
}

func TestHashUniqueSalts(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Error("same input produced identical hashes: salts not randomised")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"not-a-hash",
		"$argon2id$",
		"$argon2id$v=19$m=64$saltbad$hashbad",
	}
	for _, c := range cases {
		if _, err := VerifyPassword("pw", c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}
