package access

import (
	"path/filepath"
	"testing"
)

func TestEnableLoginAndDisable(t *testing.T) {
	manager, err := NewAt(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enable("lemonade", "secret", "different"); err == nil {
		t.Fatal("confirmation mismatch must fail")
	}
	token, err := manager.Enable("lemonade", "secret", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if status, err := manager.Status(token); err != nil || !status.Enabled || !status.Authenticated || status.Account != "lemonade" {
		t.Fatalf("status = %#v, %v", status, err)
	}
	if _, err := manager.Login("lemonade", "wrong"); err == nil {
		t.Fatal("wrong password must fail")
	}
	if err := manager.Disable(); err != nil {
		t.Fatal(err)
	}
	if status, err := manager.Status(""); err != nil || status.Enabled {
		t.Fatalf("disabled status = %#v, %v", status, err)
	}
}
