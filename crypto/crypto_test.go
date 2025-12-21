package crypto

import (
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := "my-secret-key-for-testing"
	plaintext := "Hello, World!"

	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if encrypted == plaintext {
		t.Error("Encrypted text should not equal plaintext")
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_WrongKey(t *testing.T) {
	plaintext := "Secret message"

	encrypted, err := Encrypt(plaintext, "correct-key")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(encrypted, "wrong-key")
	if err == nil {
		t.Error("Decrypt with wrong key should fail")
	}
}

func TestPasswordHash(t *testing.T) {
	password := "secure-password-123"

	hash, err := GeneratePasswordHash(password)
	if err != nil {
		t.Fatalf("GeneratePasswordHash failed: %v", err)
	}

	if len(hash) == 0 {
		t.Error("Hash should not be empty")
	}

	if !VerifyPassword(string(hash), password) {
		t.Error("VerifyPassword should return true for correct password")
	}

	if VerifyPassword(string(hash), "wrong-password") {
		t.Error("VerifyPassword should return false for wrong password")
	}
}
