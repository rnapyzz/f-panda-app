package password

import "testing"

func TestHashAndVerify(t *testing.T) {
	h, err := Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}

	ok, err := Verify("correct horse battery", h)
	if err != nil || !ok {
		t.Fatalf("Verify(correct) = %v, %v; want true, nil", ok, err)
	}
	ok, err = Verify("wrong password!!", h)
	if err != nil || ok {
		t.Fatalf("Verify(wrong) = %v, %v; want false, nil", ok, err)
	}
}

func TestHashUsesRandomSalt(t *testing.T) {
	a, _ := Hash("same password 123")
	b, _ := Hash("same password 123")
	if a == b {
		t.Error("同じパスワードから同じハッシュが生成された")
	}
}

func TestVerifyInvalidHash(t *testing.T) {
	for _, h := range []string{"", "plain", "bcrypt$1$a$b", "pbkdf2-sha256$x$a$b", "pbkdf2-sha256$1$!!$b"} {
		if _, err := Verify("whatever", h); err != ErrInvalidHash {
			t.Errorf("Verify(%q) err = %v, want ErrInvalidHash", h, err)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("short"); err == nil {
		t.Error("短いパスワードが通った")
	}
	if err := Validate("パスワードは十二文字以上ですよ"); err != nil {
		t.Errorf("マルチバイト12文字以上が拒否された: %v", err)
	}
}
